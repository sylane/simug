package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"simug/internal/agent"
	"simug/internal/git"
	"simug/internal/state"
)

type agentRunOptions struct {
	Prompt                  string
	ExpectedBranch          string
	BeforeHead              string
	RequireCommitForDone    bool
	AllowIdleOnMain         bool
	ScopeLock               *executionScopeLock
	SessionID               string
	FailClosedOnHeadAdvance bool
	ValidateResult          func(agent.Result, string, string) error
}

type bootstrapIntentProposal struct {
	TaskRef    string   `json:"task_ref"`
	Summary    string   `json:"summary"`
	BranchSlug string   `json:"branch_slug"`
	PRTitle    string   `json:"pr_title"`
	PRBody     string   `json:"pr_body"`
	Checks     []string `json:"checks"`
}

type planningTaskStatus string

const (
	planningTaskTODO       planningTaskStatus = "todo"
	planningTaskInProgress planningTaskStatus = "in_progress"
	planningTaskDone       planningTaskStatus = "done"
)

type planningStatusSnapshot struct {
	Path            string
	Exists          bool
	SupportedFormat bool
	TasksByID       map[string]planningTaskStatus
	InProgressID    []string
}

type executionScopeLock struct {
	TaskRef          string
	TaskID           string
	BranchName       string
	PlanningBaseline planningStatusSnapshot
	PlanningEnforced bool
}

type executionReport struct {
	TaskRef string   `json:"task_ref"`
	Summary string   `json:"summary"`
	Branch  string   `json:"branch"`
	Head    string   `json:"head"`
	Checks  []string `json:"checks,omitempty"`
}

func (o *orchestrator) runAgentWithValidation(ctx context.Context, prompt, expectedBranch, beforeHead string, requireCommitForDone, allowIdleOnMain bool, scopeLock *executionScopeLock, sessionID string, failClosedOnHeadAdvance bool, validateResult func(agent.Result, string, string) error) (agent.Result, string, error) {
	return o.runAgentWithValidationOptions(ctx, agentRunOptions{
		Prompt:                  prompt,
		ExpectedBranch:          expectedBranch,
		BeforeHead:              beforeHead,
		RequireCommitForDone:    requireCommitForDone,
		AllowIdleOnMain:         allowIdleOnMain,
		ScopeLock:               scopeLock,
		SessionID:               sessionID,
		FailClosedOnHeadAdvance: failClosedOnHeadAdvance,
		ValidateResult:          validateResult,
	})
}

func (o *orchestrator) runAgentWithValidationOptions(ctx context.Context, options agentRunOptions) (agent.Result, string, error) {
	currentPrompt := options.Prompt
	for attempt := 0; attempt <= o.cfg.MaxRepairAttempts; attempt++ {
		turn := newCoordinatorTurn(o.runID, o.tickSeq, attempt+1, options.SessionID)
		promptForRun := appendCoordinatorTurnPrompt(currentPrompt, turn)
		if err := o.recordInFlightAttemptStart(attempt+1, o.cfg.MaxRepairAttempts+1, options.ExpectedBranch, options.BeforeHead, promptForRun); err != nil {
			return agent.Result{}, "", err
		}

		fmt.Printf("agent: running Codex (attempt %d/%d)\n", attempt+1, o.cfg.MaxRepairAttempts+1)
		runStart := time.Now()
		o.logEvent("agent_attempt", "running codex attempt", map[string]any{
			"attempt":             attempt + 1,
			"max_attempts":        o.cfg.MaxRepairAttempts + 1,
			"expected_branch":     options.ExpectedBranch,
			"require_commit_done": options.RequireCommitForDone,
			"allow_idle_on_main":  options.AllowIdleOnMain,
			"protocol_turn_id":    turn.TurnID,
			"protocol_session_id": turn.SessionID,
			"session_id":          strings.TrimSpace(options.SessionID),
		})

		runner := o.runner
		if strings.TrimSpace(options.SessionID) != "" {
			resumeCommand, err := buildSessionResumeCommand(runner.Command, options.SessionID)
			if err != nil {
				return agent.Result{}, "", err
			}
			runner.Command = resumeCommand
		}
		runner.Turn = turn
		if o.verboseConsole {
			o.emitVerbosePrompt(attempt+1, o.cfg.MaxRepairAttempts+1, promptForRun, options.SessionID)
			runner.OnLine = o.emitVerboseAgentLine
		}

		result, err := runner.Run(ctx, promptForRun)
		if err != nil {
			rawOutput := agent.RawOutputFromError(err)
			afterHead := ""
			if options.FailClosedOnHeadAdvance {
				observedHead, headErr := git.HeadSHA(ctx, o.repoRoot)
				if headErr != nil {
					return agent.Result{}, "", fmt.Errorf("read HEAD after failed bootstrap attempt: %w", headErr)
				}
				afterHead = observedHead
			}
			diagnostics := buildAttemptArchiveDiagnostics(rawOutput, nil, err, nil)
			if journalErr := o.recordInFlightAttemptResult(attempt+1, afterHead, "", err.Error(), ""); journalErr != nil {
				return agent.Result{}, "", journalErr
			}
			paths, archiveErr := o.archiveAgentAttempt(
				attempt+1,
				o.cfg.MaxRepairAttempts+1,
				options.ExpectedBranch,
				options.BeforeHead,
				afterHead,
				turn,
				promptForRun,
				rawOutput,
				"",
				false,
				err.Error(),
				"",
				diagnostics,
			)
			if archiveErr != nil {
				o.logEvent("agent_archive_error", "failed to archive codex attempt", map[string]any{
					"attempt": attempt + 1,
					"error":   archiveErr.Error(),
				})
			} else {
				o.logEvent("agent_archive", "archived codex attempt artifacts", map[string]any{
					"attempt":       attempt + 1,
					"protocol_turn": turn.TurnID,
					"metadata_path": paths.MetadataPath,
					"prompt_path":   paths.PromptPath,
					"output_path":   paths.OutputPath,
				})
			}

			o.logEvent("agent_attempt", "codex attempt failed", map[string]any{
				"attempt":      attempt + 1,
				"duration_ms":  time.Since(runStart).Milliseconds(),
				"error":        err.Error(),
				"prompt_tail":  tailString(promptForRun, 600),
				"output_tail":  tailString(rawOutput, 600),
				"terminal":     "",
				"terminal_set": false,
			})
			o.logEvent("invariant_decision", "agent execution/protocol failed", map[string]any{
				"pass":    false,
				"attempt": attempt + 1,
				"error":   err.Error(),
			})

			if options.FailClosedOnHeadAdvance && strings.TrimSpace(afterHead) != strings.TrimSpace(options.BeforeHead) {
				return agent.Result{}, "", fmt.Errorf("bootstrap attempt advanced HEAD from %q to %q before successful validation; refusing automatic repair after execution/protocol failure: %w", options.BeforeHead, afterHead, err)
			}

			if attempt >= o.cfg.MaxRepairAttempts {
				return agent.Result{}, "", fmt.Errorf("agent failed after %d attempts with execution/protocol errors: %w", attempt+1, err)
			}

			currentPrompt = o.buildRepairPrompt(options.ExpectedBranch, err, options.ScopeLock)
			continue
		}
		o.logEvent("agent_attempt", "codex attempt completed", map[string]any{
			"attempt":      attempt + 1,
			"duration_ms":  time.Since(runStart).Milliseconds(),
			"output_tail":  tailString(result.RawOutput, 600),
			"terminal":     string(result.Terminal.Type),
			"terminal_set": result.Terminal.Type != "",
			"manager_msgs": len(result.ManagerMessages),
			"quarantined":  len(result.QuarantinedLines),
		})
		for _, message := range result.ManagerMessages {
			o.logEvent("manager_message", "agent manager-channel message", map[string]any{
				"attempt": attempt + 1,
				"body":    limitString(message, 4000),
			})
		}
		if len(result.QuarantinedLines) > 0 {
			o.logEvent("agent_quarantine", "agent emitted unprefixed output lines", map[string]any{
				"attempt": attempt + 1,
				"count":   len(result.QuarantinedLines),
				"lines":   result.QuarantinedLines,
			})
		}

		afterHead, validationErr := o.validatePostAgentState(ctx, result, options.ExpectedBranch, options.BeforeHead, options.RequireCommitForDone, options.AllowIdleOnMain)
		if validationErr == nil && options.ValidateResult != nil {
			validationErr = options.ValidateResult(result, options.BeforeHead, afterHead)
		}
		if journalErr := o.recordInFlightAttemptResult(attempt+1, afterHead, string(result.Terminal.Type), "", errorText(validationErr)); journalErr != nil {
			return agent.Result{}, "", journalErr
		}
		diagnostics := buildAttemptArchiveDiagnostics(result.RawOutput, &result, nil, validationErr)
		paths, archiveErr := o.archiveAgentAttempt(
			attempt+1,
			o.cfg.MaxRepairAttempts+1,
			options.ExpectedBranch,
			options.BeforeHead,
			afterHead,
			turn,
			promptForRun,
			result.RawOutput,
			string(result.Terminal.Type),
			result.Terminal.Changes,
			"",
			errorText(validationErr),
			diagnostics,
		)
		if archiveErr != nil {
			o.logEvent("agent_archive_error", "failed to archive codex attempt", map[string]any{
				"attempt": attempt + 1,
				"error":   archiveErr.Error(),
			})
		} else {
			o.logEvent("agent_archive", "archived codex attempt artifacts", map[string]any{
				"attempt":       attempt + 1,
				"protocol_turn": turn.TurnID,
				"metadata_path": paths.MetadataPath,
				"prompt_path":   paths.PromptPath,
				"output_path":   paths.OutputPath,
			})
		}

		if validationErr == nil {
			if err := o.clearInFlightAttemptJournal(); err != nil {
				return agent.Result{}, "", err
			}
			o.logEvent("invariant_decision", "post-agent validation passed", map[string]any{
				"pass":                 true,
				"attempt":              attempt + 1,
				"expected_branch":      options.ExpectedBranch,
				"before_head":          options.BeforeHead,
				"after_head":           afterHead,
				"terminal":             string(result.Terminal.Type),
				"terminal_has_changes": result.Terminal.Changes,
			})
			return result, afterHead, nil
		}
		o.logEvent("invariant_decision", "post-agent validation failed", map[string]any{
			"pass":                 false,
			"attempt":              attempt + 1,
			"expected_branch":      options.ExpectedBranch,
			"before_head":          options.BeforeHead,
			"after_head":           afterHead,
			"terminal":             string(result.Terminal.Type),
			"terminal_has_changes": result.Terminal.Changes,
			"error":                validationErr.Error(),
		})
		if options.FailClosedOnHeadAdvance && strings.TrimSpace(afterHead) != strings.TrimSpace(options.BeforeHead) {
			return agent.Result{}, "", fmt.Errorf("bootstrap attempt advanced HEAD from %q to %q before successful validation; refusing automatic repair after validation failure: %w", options.BeforeHead, afterHead, validationErr)
		}
		if attempt >= o.cfg.MaxRepairAttempts {
			return agent.Result{}, "", fmt.Errorf("agent failed validation after %d attempts: %w", attempt+1, validationErr)
		}

		currentPrompt = o.buildRepairPrompt(options.ExpectedBranch, validationErr, options.ScopeLock)
	}
	return agent.Result{}, "", fmt.Errorf("unreachable")
}

func (o *orchestrator) validatePostAgentState(ctx context.Context, result agent.Result, expectedBranch, beforeHead string, requireCommitForDone, allowIdleOnMain bool) (string, error) {
	branch, err := git.CurrentBranch(ctx, o.repoRoot)
	if err != nil {
		return "", fmt.Errorf("read current branch: %w", err)
	}
	if result.Terminal.Type == agent.ActionIdle && allowIdleOnMain {
		if branch != o.cfg.MainBranch {
			return "", fmt.Errorf("idle action requires staying on %q, got branch %q", o.cfg.MainBranch, branch)
		}
	} else {
		if branch != expectedBranch {
			return "", fmt.Errorf("expected current branch %q, got %q", expectedBranch, branch)
		}
		if expectedBranch != o.cfg.MainBranch && !o.cfg.BranchPattern.MatchString(branch) {
			return "", fmt.Errorf("branch %q does not match required pattern %q", branch, o.cfg.BranchPattern.String())
		}
	}

	clean, status, err := git.IsClean(ctx, o.repoRoot)
	if err != nil {
		return "", fmt.Errorf("check git cleanliness: %w", err)
	}
	if !clean {
		return "", fmt.Errorf("working tree is dirty after agent run:\n%s", status)
	}

	afterHead, err := git.HeadSHA(ctx, o.repoRoot)
	if err != nil {
		return "", fmt.Errorf("read post-agent head: %w", err)
	}

	switch result.Terminal.Type {
	case agent.ActionDone:
		if requireCommitForDone && !result.Terminal.Changes {
			return "", fmt.Errorf("done action must set changes=true in this workflow stage")
		}
		if result.Terminal.Changes && beforeHead == afterHead {
			return "", fmt.Errorf("done action indicates changes=true but no new commit exists")
		}
		if !result.Terminal.Changes && beforeHead != afterHead {
			return "", fmt.Errorf("done action indicates changes=false but commits changed during run")
		}
	case agent.ActionIdle:
		if beforeHead != afterHead {
			return "", fmt.Errorf("idle action emitted but commits changed during run")
		}
	}

	return afterHead, nil
}

func validateBootstrapExecutionCommitCount(ctx context.Context, repoRoot, beforeHead, afterHead string, terminalType agent.ActionType) error {
	if terminalType != agent.ActionDone {
		return nil
	}
	count, err := git.CommitCountBetween(ctx, repoRoot, beforeHead, afterHead)
	if err != nil {
		return fmt.Errorf("count bootstrap execution commits between %s and %s: %w", beforeHead, afterHead, err)
	}
	if count != 1 {
		return fmt.Errorf("bootstrap execution must produce exactly 1 commit from staged baseline; observed %d commit(s)", count)
	}
	return nil
}

func validateIssueTriageResult(result agent.Result, expectedIssue int) (agent.Action, error) {
	reportCount := 0
	reportIndex := -1
	terminalIndex := -1
	var report agent.Action

	for i, a := range result.Actions {
		switch a.Type {
		case agent.ActionIssueReport:
			reportCount++
			report = a
			reportIndex = i
		case agent.ActionDone, agent.ActionIdle:
			terminalIndex = i
		default:
			return agent.Action{}, fmt.Errorf("issue_triage mode does not allow action %q", a.Type)
		}
	}

	if reportCount != 1 {
		return agent.Action{}, fmt.Errorf("issue_triage mode requires exactly one issue_report action, got %d", reportCount)
	}
	if terminalIndex < 0 {
		return agent.Action{}, fmt.Errorf("issue_triage mode requires terminal action after issue_report")
	}
	if reportIndex > terminalIndex {
		return agent.Action{}, fmt.Errorf("issue_report action must appear before terminal action")
	}
	if report.IssueNumber != expectedIssue {
		return agent.Action{}, fmt.Errorf("issue_report issue_number=%d does not match selected issue %d", report.IssueNumber, expectedIssue)
	}
	if strings.TrimSpace(report.Analysis) == "" {
		return agent.Action{}, fmt.Errorf("issue_report requires non-empty analysis")
	}
	if report.NeedsTask {
		if strings.TrimSpace(report.TaskTitle) == "" || strings.TrimSpace(report.TaskBody) == "" {
			return agent.Action{}, fmt.Errorf("issue_report with needs_task=true requires non-empty task_title and task_body")
		}
	}
	if result.Terminal.Type == agent.ActionDone && result.Terminal.Changes {
		return agent.Action{}, fmt.Errorf("issue_triage mode does not allow done action with changes=true")
	}

	return report, nil
}

func validateIssueUpdateActions(actions []agent.Action) error {
	for _, a := range actions {
		if a.Type != agent.ActionIssueUpdate {
			continue
		}
		if a.IssueNumber <= 0 {
			return fmt.Errorf("issue_update action requires positive issue_number")
		}
		switch a.Relation {
		case agent.IssueRelationFixes, agent.IssueRelationImpacts, agent.IssueRelationRelates:
		default:
			return fmt.Errorf("issue_update action invalid relation %q", a.Relation)
		}
		if strings.TrimSpace(a.CommentBody) == "" {
			return fmt.Errorf("issue_update action requires non-empty comment")
		}
	}
	return nil
}

func (o *orchestrator) validateBootstrapIntentResult(result agent.Result, pendingTaskID string) (state.BootstrapIntent, error) {
	if result.Terminal.Type == agent.ActionIdle {
		if len(result.Actions) != 1 {
			return state.BootstrapIntent{}, fmt.Errorf("bootstrap intent idle result must not include non-terminal actions")
		}
		return state.BootstrapIntent{}, nil
	}
	if result.Terminal.Type != agent.ActionDone {
		return state.BootstrapIntent{}, fmt.Errorf("bootstrap intent requires terminal done or idle action")
	}
	if result.Terminal.Changes {
		return state.BootstrapIntent{}, fmt.Errorf("bootstrap intent done action must set changes=false")
	}

	commentCount := 0
	var commentBody string
	for _, a := range result.Actions {
		switch a.Type {
		case agent.ActionComment:
			commentCount++
			commentBody = a.Body
		case agent.ActionDone:
		default:
			return state.BootstrapIntent{}, fmt.Errorf("bootstrap intent mode does not allow action %q", a.Type)
		}
	}
	if commentCount != 1 {
		return state.BootstrapIntent{}, fmt.Errorf("bootstrap intent mode requires exactly one comment action, got %d", commentCount)
	}

	proposal, err := parseBootstrapIntentProposal(commentBody)
	if err != nil {
		return state.BootstrapIntent{}, err
	}
	if strings.TrimSpace(pendingTaskID) != "" {
		if !strings.Contains(strings.ToLower(strings.TrimSpace(proposal.TaskRef)), strings.ToLower(strings.TrimSpace(pendingTaskID))) {
			return state.BootstrapIntent{}, fmt.Errorf("intent task_ref %q does not match required pending task %q", proposal.TaskRef, pendingTaskID)
		}
	}
	if _, err := parseTaskIDFromRef(proposal.TaskRef); err != nil {
		return state.BootstrapIntent{}, fmt.Errorf("intent task_ref validation failed: %w", err)
	}

	slug := sanitizeBranchSlug(proposal.BranchSlug)
	if !bootstrapIntentSlugPattern.MatchString(slug) {
		return state.BootstrapIntent{}, fmt.Errorf("intent branch_slug %q is invalid after normalization", proposal.BranchSlug)
	}

	checks := make([]string, 0, len(proposal.Checks))
	for _, c := range proposal.Checks {
		if trimmed := strings.TrimSpace(c); trimmed != "" {
			checks = append(checks, trimmed)
		}
	}

	return state.BootstrapIntent{
		TaskRef:    strings.TrimSpace(proposal.TaskRef),
		Summary:    strings.TrimSpace(proposal.Summary),
		BranchSlug: slug,
		BranchName: o.generateBranchName(slug),
		PRTitle:    strings.TrimSpace(proposal.PRTitle),
		PRBody:     strings.TrimSpace(proposal.PRBody),
		Checks:     checks,
		ApprovedAt: time.Now().UTC(),
	}, nil
}

func parseBootstrapIntentProposal(commentBody string) (bootstrapIntentProposal, error) {
	body := strings.TrimSpace(commentBody)
	if !strings.HasPrefix(body, "INTENT_JSON:") {
		return bootstrapIntentProposal{}, fmt.Errorf("bootstrap intent comment must start with INTENT_JSON:")
	}
	rawJSON := strings.TrimSpace(strings.TrimPrefix(body, "INTENT_JSON:"))
	if rawJSON == "" {
		return bootstrapIntentProposal{}, fmt.Errorf("bootstrap intent json payload is empty")
	}

	var proposal bootstrapIntentProposal
	if err := json.Unmarshal([]byte(rawJSON), &proposal); err != nil {
		return bootstrapIntentProposal{}, fmt.Errorf("decode bootstrap intent payload: %w", err)
	}
	if strings.TrimSpace(proposal.TaskRef) == "" {
		return bootstrapIntentProposal{}, fmt.Errorf("bootstrap intent requires non-empty task_ref")
	}
	if strings.TrimSpace(proposal.Summary) == "" {
		return bootstrapIntentProposal{}, fmt.Errorf("bootstrap intent requires non-empty summary")
	}
	if strings.TrimSpace(proposal.BranchSlug) == "" {
		return bootstrapIntentProposal{}, fmt.Errorf("bootstrap intent requires non-empty branch_slug")
	}
	if strings.TrimSpace(proposal.PRTitle) == "" {
		return bootstrapIntentProposal{}, fmt.Errorf("bootstrap intent requires non-empty pr_title")
	}
	if strings.TrimSpace(proposal.PRBody) == "" {
		return bootstrapIntentProposal{}, fmt.Errorf("bootstrap intent requires non-empty pr_body")
	}
	return proposal, nil
}

func sanitizeBranchSlug(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	normalized := strings.Trim(b.String(), "-")
	if len(normalized) > 41 {
		normalized = strings.Trim(normalized[:41], "-")
	}
	return normalized
}

func parseTaskIDFromRef(taskRef string) (string, error) {
	match := taskRefIDPattern.FindStringSubmatch(strings.TrimSpace(taskRef))
	if len(match) != 2 {
		return "", fmt.Errorf("task_ref %q must include canonical 'Task <id>'", strings.TrimSpace(taskRef))
	}
	return strings.TrimSpace(match[1]), nil
}

func capturePlanningStatus(repoRoot string) (planningStatusSnapshot, error) {
	return capturePlanningStatusWithCandidates(repoRoot, defaultPlanningGuidanceCandidates)
}

func capturePlanningStatusWithCandidates(repoRoot string, candidates []string) (planningStatusSnapshot, error) {
	path, ok := firstExistingRelativePath(repoRoot, candidates)
	if !ok {
		return planningStatusSnapshot{
			TasksByID:    map[string]planningTaskStatus{},
			InProgressID: nil,
		}, nil
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		if os.IsNotExist(err) {
			return planningStatusSnapshot{
				TasksByID:    map[string]planningTaskStatus{},
				InProgressID: nil,
			}, nil
		}
		return planningStatusSnapshot{}, fmt.Errorf("read planning file: %w", err)
	}

	snapshot := planningStatusSnapshot{
		Path:            path,
		Exists:          true,
		SupportedFormat: false,
		TasksByID:       make(map[string]planningTaskStatus),
		InProgressID:    nil,
	}
	lines := strings.Split(string(data), "\n")
	matchedTasks := 0
	for _, line := range lines {
		match := planningTaskStatusPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) != 3 {
			continue
		}
		matchedTasks++
		check := strings.TrimSpace(match[1])
		taskID := strings.TrimSpace(match[2])
		status := planningTaskTODO
		if check == "x" {
			status = planningTaskDone
		} else if strings.Contains(line, "**[IN_PROGRESS] Task ") {
			status = planningTaskInProgress
			snapshot.InProgressID = append(snapshot.InProgressID, taskID)
		}
		snapshot.TasksByID[taskID] = status
	}
	snapshot.SupportedFormat = matchedTasks > 0
	return snapshot, nil
}

func (s planningStatusSnapshot) supportsTask(taskID string) bool {
	if !s.Exists || !s.SupportedFormat {
		return false
	}
	_, ok := s.TasksByID[strings.TrimSpace(taskID)]
	return ok
}

func (s planningStatusSnapshot) displayPath() string {
	if strings.TrimSpace(s.Path) == "" {
		return "planning status file"
	}
	return s.Path
}

func (o *orchestrator) newExecutionScopeLock(intent state.BootstrapIntent) (*executionScopeLock, error) {
	taskID, err := parseTaskIDFromRef(intent.TaskRef)
	if err != nil {
		return nil, fmt.Errorf("derive execution scope lock from intent: %w", err)
	}
	planningBaseline, err := capturePlanningStatusWithCandidates(o.repoRoot, o.cfg.planningCandidates())
	if err != nil {
		return nil, err
	}
	return &executionScopeLock{
		TaskRef:          strings.TrimSpace(intent.TaskRef),
		TaskID:           taskID,
		BranchName:       strings.TrimSpace(intent.BranchName),
		PlanningBaseline: planningBaseline,
		PlanningEnforced: planningBaseline.supportsTask(taskID),
	}, nil
}

func (o *orchestrator) validateExecutionScopeLock(lock *executionScopeLock) error {
	if lock == nil || !lock.PlanningEnforced {
		return nil
	}

	current, err := capturePlanningStatusWithCandidates(o.repoRoot, o.cfg.planningCandidates())
	if err != nil {
		return err
	}
	baselinePath := lock.PlanningBaseline.displayPath()
	if !current.Exists || current.Path != lock.PlanningBaseline.Path {
		return fmt.Errorf("execution scope lock violation: %s disappeared during locked task %s", baselinePath, lock.TaskID)
	}
	if !current.SupportedFormat {
		return fmt.Errorf("execution scope lock violation: %s no longer exposes supported task status markers during locked task %s", baselinePath, lock.TaskID)
	}
	if len(current.InProgressID) > 1 {
		return fmt.Errorf("execution scope lock violation: multiple [IN_PROGRESS] tasks detected: %s", strings.Join(current.InProgressID, ", "))
	}
	if len(current.InProgressID) == 1 && strings.TrimSpace(current.InProgressID[0]) != lock.TaskID {
		return fmt.Errorf("execution scope lock violation: [IN_PROGRESS] task drifted to %s (locked task is %s)", strings.TrimSpace(current.InProgressID[0]), lock.TaskID)
	}

	for taskID, baselineStatus := range lock.PlanningBaseline.TasksByID {
		if taskID == lock.TaskID {
			continue
		}
		currentStatus, ok := current.TasksByID[taskID]
		if !ok {
			return fmt.Errorf("execution scope lock violation: task %s disappeared from planning during locked execution", taskID)
		}
		if currentStatus != baselineStatus {
			return fmt.Errorf("execution scope lock violation: task %s status changed from %s to %s during locked task %s", taskID, baselineStatus, currentStatus, lock.TaskID)
		}
	}
	if _, ok := current.TasksByID[lock.TaskID]; !ok {
		return fmt.Errorf("execution scope lock violation: locked task %s is missing from planning", lock.TaskID)
	}
	return nil
}

func validateExecutionReport(result agent.Result, intent state.BootstrapIntent, expectedBranch, beforeHead, afterHead string) (executionReport, []agent.Action, error) {
	if result.Terminal.Type == agent.ActionIdle {
		return executionReport{}, result.Actions, nil
	}
	if result.Terminal.Type != agent.ActionDone {
		return executionReport{}, nil, fmt.Errorf("execution report validation requires terminal done or idle action")
	}

	var report executionReport
	reportCount := 0
	filtered := make([]agent.Action, 0, len(result.Actions))
	for _, action := range result.Actions {
		if action.Type == agent.ActionComment {
			body := strings.TrimSpace(action.Body)
			if strings.HasPrefix(body, executionReportPrefix) {
				reportCount++
				if reportCount > 1 {
					return executionReport{}, nil, fmt.Errorf("execution report validation requires exactly one REPORT_JSON comment, got %d", reportCount)
				}
				reportJSON := strings.TrimSpace(strings.TrimPrefix(body, executionReportPrefix))
				if reportJSON == "" {
					return executionReport{}, nil, fmt.Errorf("execution report payload is empty")
				}
				if err := json.Unmarshal([]byte(reportJSON), &report); err != nil {
					return executionReport{}, nil, fmt.Errorf("decode execution report payload: %w", err)
				}
				continue
			}
		}
		filtered = append(filtered, action)
	}
	if reportCount != 1 {
		return executionReport{}, nil, fmt.Errorf("execution report validation requires exactly one REPORT_JSON comment, got %d", reportCount)
	}
	if strings.TrimSpace(report.TaskRef) == "" {
		return executionReport{}, nil, fmt.Errorf("execution report requires non-empty task_ref")
	}
	if strings.TrimSpace(report.Summary) == "" {
		return executionReport{}, nil, fmt.Errorf("execution report requires non-empty summary")
	}
	if strings.TrimSpace(report.Branch) == "" {
		return executionReport{}, nil, fmt.Errorf("execution report requires non-empty branch")
	}
	if strings.TrimSpace(report.Head) == "" {
		return executionReport{}, nil, fmt.Errorf("execution report requires non-empty head")
	}

	intentTaskID, err := parseTaskIDFromRef(intent.TaskRef)
	if err != nil {
		return executionReport{}, nil, fmt.Errorf("execution report intent task_ref invalid: %w", err)
	}
	reportTaskID, err := parseTaskIDFromRef(report.TaskRef)
	if err != nil {
		return executionReport{}, nil, fmt.Errorf("execution report task_ref invalid: %w", err)
	}
	if reportTaskID != intentTaskID {
		return executionReport{}, nil, fmt.Errorf("execution report task_ref %q does not match approved intent task_ref %q", report.TaskRef, intent.TaskRef)
	}
	if strings.TrimSpace(report.Branch) != strings.TrimSpace(expectedBranch) {
		return executionReport{}, nil, fmt.Errorf("execution report branch %q does not match expected branch %q", report.Branch, expectedBranch)
	}
	if strings.TrimSpace(report.Head) != strings.TrimSpace(afterHead) {
		return executionReport{}, nil, fmt.Errorf("execution report head %q does not match post-run head %q", report.Head, afterHead)
	}
	if strings.TrimSpace(report.Head) == strings.TrimSpace(beforeHead) {
		return executionReport{}, nil, fmt.Errorf("execution report head %q did not advance from pre-run head", report.Head)
	}
	return report, filtered, nil
}

func (o *orchestrator) buildRepairPrompt(expectedBranch string, validationErr error, scopeLock *executionScopeLock) string {
	var b strings.Builder
	b.WriteString("Your previous run violated simug validation checks.\n")
	b.WriteString("Fix repository consistency and emit protocol lines again.\n")
	b.WriteString("Violation:\n")
	b.WriteString(validationErr.Error())
	b.WriteString("\n\n")
	b.WriteString("Rules:\n")
	b.WriteString(strings.ToLower(o.promptGuidanceInstruction()))
	b.WriteString("- commit local changes when task is complete\n")
	b.WriteString("- maintain task records: history/, CHANGELOG.md, and .git/SIMUG_COMMIT_MSG\n")
	b.WriteString("- use issue_update actions for issue linkage intent; do not comment on issues directly\n")
	b.WriteString("- never push or create/update PR directly\n")
	b.WriteString("- do NOT run environment-sensitive validation gates in this repair turn (for example scripts/canary-real-codex-gate.sh, self-host canaries, or network-dependent release checks)\n")
	b.WriteString("- finish the repair turn once repository consistency is restored and the coordinator envelope is emitted; gate follow-up can happen separately\n")
	b.WriteString("- emit machine actions only inside one bounded SIMUG coordinator envelope\n")
	b.WriteString("- emit exactly one coordinator begin envelope and one matching coordinator end envelope for the active turn\n")
	b.WriteString("- each coordinator action envelope must use event=action and carry the action JSON in payload\n")
	b.WriteString("- when the coordinator provides a non-empty session_id for the active turn, include that same session_id in every coordinator envelope\n")
	b.WriteString("- use SIMUG_MANAGER: for manager-facing messages; unprefixed text is quarantined\n")
	b.WriteString("- coordinator ignores SIMUG lines outside the active turn envelope\n")
	b.WriteString(fmt.Sprintf("- branch must be %q (or %q if terminal action is idle)\n", expectedBranch, o.cfg.MainBranch))
	b.WriteString("- keep the working tree clean before finishing\n")
	if scopeLock != nil {
		b.WriteString(fmt.Sprintf("- execution scope lock: stay on %q and implement only %s\n", scopeLock.BranchName, scopeLock.TaskRef))
		if scopeLock.PlanningEnforced {
			b.WriteString(fmt.Sprintf("- in %s, do not change status markers for tasks other than Task %s\n", scopeLock.PlanningBaseline.displayPath(), scopeLock.TaskID))
			b.WriteString(fmt.Sprintf("- at most one [IN_PROGRESS] task is allowed in %s, and if present it must be Task %s\n", scopeLock.PlanningBaseline.displayPath(), scopeLock.TaskID))
		} else {
			b.WriteString(fmt.Sprintf("- no supported planning status file was discovered for Task %s; if you update task-tracking docs, limit those changes to the locked task only\n", scopeLock.TaskID))
		}
		b.WriteString("- when terminal action is done, emit one REPORT_JSON comment with task_ref, summary, branch, and head from this run\n")
	}
	b.WriteString("Coordinator envelope schema for this repair turn:\n")
	b.WriteString("- SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString("- begin envelope: coordinator event=begin for the active turn_id (and session_id when provided)\n")
	b.WriteString("- action envelope payload.action may be comment(body), reply(comment_id, body), issue_update(issue_number, relation, comment), done(summary, changes), or idle(reason)\n")
	b.WriteString("- end envelope: coordinator event=end matching the same active turn identity\n")
	return b.String()
}
