package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"simug/internal/agent"
	"simug/internal/git"
	"simug/internal/github"
	"simug/internal/state"
)

func (o *orchestrator) tick(ctx context.Context) error {
	prs, err := github.ListOpenPRsByAuthor(ctx, o.repoRoot, o.user)
	if err != nil {
		return fmt.Errorf("list open prs: %w", err)
	}

	switch len(prs) {
	case 0:
		return o.handleNoManagedPR(ctx)
	case 1:
		return o.handleSingleManagedPR(ctx, prs[0])
	default:
		var items []string
		for _, pr := range prs {
			items = append(items, fmt.Sprintf("#%d (%s)", pr.Number, pr.HeadRefName))
		}
		sort.Strings(items)
		o.logEvent("invariant_violation", "multiple open PRs for current user", map[string]any{"prs": items})
		return fmt.Errorf("found %d open PRs authored by %s; expected at most one managed PR: %s", len(prs), o.user, strings.Join(items, ", "))
	}
}

func (o *orchestrator) handleSingleManagedPR(ctx context.Context, pr github.PullRequest) error {
	if !o.cfg.BranchPattern.MatchString(pr.HeadRefName) {
		err := fmt.Errorf("open PR #%d branch %q does not match managed pattern %q", pr.Number, pr.HeadRefName, o.cfg.BranchPattern.String())
		o.logEvent("invariant_decision", "managed branch policy failed", map[string]any{
			"pass":             false,
			"pr":               pr.Number,
			"branch":           pr.HeadRefName,
			"required_pattern": o.cfg.BranchPattern.String(),
			"error":            err.Error(),
		})
		return err
	}
	o.logEvent("invariant_decision", "managed branch policy passed", map[string]any{
		"pass":             true,
		"pr":               pr.Number,
		"branch":           pr.HeadRefName,
		"required_pattern": o.cfg.BranchPattern.String(),
	})
	if o.state.Mode != state.ModeManagedPR {
		o.logEvent("mode_transition", "switching to managed_pr mode", map[string]any{
			"from": string(o.state.Mode),
			"to":   string(state.ModeManagedPR),
		})
	}
	o.enterManagedPRMode(pr)
	return o.handleManagedPR(ctx, pr.Number)
}

type mergedBranchTransitionContext struct {
	PRNumber int
	Branch   string
}

func (o *orchestrator) handleNoManagedPR(ctx context.Context) error {
	var mergedBranchCtx mergedBranchTransitionContext
	if o.state.ActivePR != 0 {
		var err error
		mergedBranchCtx, err = o.handleInactiveManagedPR(ctx, o.state.ActivePR)
		if err != nil {
			return err
		}
		fmt.Printf("status: active PR #%d is no longer open; transitioning to next-task bootstrap\n", o.state.ActivePR)
		o.logEvent("pr_transition", "active PR is no longer open", map[string]any{"active_pr": o.state.ActivePR})
	}
	if o.state.Mode == state.ModeManagedPR {
		o.logEvent("mode_transition", "managed PR closed, entering issue_triage mode", map[string]any{
			"from": string(state.ModeManagedPR),
			"to":   string(state.ModeIssueTriage),
		})
	}
	if o.state.Mode == "" || o.state.Mode == state.ModeManagedPR {
		o.transitionToIssueTriageMode()
	}
	o.clearActivePRState()
	return o.handleNoOpenPR(ctx, mergedBranchCtx)
}

func (o *orchestrator) handleManagedPR(ctx context.Context, prNumber int) error {
	pr, err := github.GetPR(ctx, o.repoRoot, prNumber)
	if err != nil {
		return fmt.Errorf("read PR #%d: %w", prNumber, err)
	}
	if !strings.EqualFold(pr.State, "OPEN") {
		return fmt.Errorf("expected PR #%d to be open, got state=%q", pr.Number, pr.State)
	}

	if err := git.FetchOrigin(ctx, o.repoRoot); err != nil {
		return fmt.Errorf("fetch origin before PR validation: %w", err)
	}
	if err := o.validateCheckoutMatchesPR(ctx, pr); err != nil {
		o.logEvent("invariant_decision", "checkout synchronization failed", map[string]any{
			"pass":         false,
			"pr":           pr.Number,
			"expected_ref": pr.HeadRefName,
			"expected_oid": pr.HeadRefOid,
			"error":        err.Error(),
		})
		return err
	}
	o.logEvent("invariant_decision", "checkout synchronization passed", map[string]any{
		"pass":         true,
		"pr":           pr.Number,
		"expected_ref": pr.HeadRefName,
		"expected_oid": pr.HeadRefOid,
	})

	o.state.ActivePR = pr.Number
	o.state.ActiveBranch = pr.HeadRefName

	poll, err := o.pollEvents(ctx, pr.Number)
	if err != nil {
		return err
	}
	if len(poll.Events) == 0 && !o.state.CursorUncertain {
		fmt.Printf("status: PR #%d in sync, no new comments\n", pr.Number)
		return nil
	}

	if len(poll.Events) > 0 {
		fmt.Printf("status: PR #%d has %d new event(s)\n", pr.Number, len(poll.Events))
		o.logEvent("pr_events", "new PR events detected", map[string]any{"pr": pr.Number, "count": len(poll.Events)})
	} else if o.state.CursorUncertain {
		fmt.Printf("status: cursor uncertainty detected, replaying context to Codex\n")
		o.logEvent("cursor_replay", "cursor uncertainty triggered replay", map[string]any{"pr": pr.Number})
	}

	beforeHead, err := git.HeadSHA(ctx, o.repoRoot)
	if err != nil {
		return fmt.Errorf("read pre-agent head: %w", err)
	}

	prompt := o.buildManagedPRPrompt(pr, poll.Events, o.state.CursorUncertain, "")
	result, afterHead, err := o.runAgentWithValidationOptions(ctx, agentRunOptions{
		Prompt:               prompt,
		ExpectedBranch:       pr.HeadRefName,
		BeforeHead:           beforeHead,
		ValidateResult:       func(result agent.Result, _, _ string) error { return validateIssueUpdateActions(result.Actions) },
		RequireCommitForDone: false,
		AllowIdleOnMain:      false,
	})
	if err != nil {
		return err
	}

	if beforeHead != afterHead {
		fmt.Printf("git: pushing branch %s\n", pr.HeadRefName)
		o.logEvent("git_push", "pushing managed branch updates", map[string]any{"branch": pr.HeadRefName, "pr": pr.Number})
		if err := git.Push(ctx, o.repoRoot, "origin", pr.HeadRefName); err != nil {
			return fmt.Errorf("push branch %s: %w", pr.HeadRefName, err)
		}
	}

	if err := o.applyActions(ctx, pr.Number, result.Actions, poll); err != nil {
		return err
	}
	if err := o.processPendingIssueUpdateComments(ctx, pr.Number); err != nil {
		return err
	}

	o.state.LastIssueCommentID = maxInt64(o.state.LastIssueCommentID, poll.MaxIssueID)
	o.state.LastReviewCommentID = maxInt64(o.state.LastReviewCommentID, poll.MaxReviewComID)
	o.state.LastReviewID = maxInt64(o.state.LastReviewID, poll.MaxReviewID)
	o.state.CursorUncertain = false

	return nil
}

func (o *orchestrator) handleInactiveManagedPR(ctx context.Context, prNumber int) (mergedBranchTransitionContext, error) {
	var mergedBranchCtx mergedBranchTransitionContext
	if prNumber <= 0 {
		return mergedBranchCtx, nil
	}
	pr, err := github.GetPR(ctx, o.repoRoot, prNumber)
	if err != nil {
		return mergedBranchCtx, fmt.Errorf("read inactive PR #%d: %w", prNumber, err)
	}

	merged := pr.MergedAt != nil || strings.EqualFold(strings.TrimSpace(pr.State), "MERGED")
	if strings.EqualFold(strings.TrimSpace(pr.State), "OPEN") && !merged {
		return mergedBranchCtx, fmt.Errorf("active PR #%d is still open but missing from authored open-PR set", prNumber)
	}
	if !merged {
		o.logEvent("pr_transition", "inactive PR is not merged; skipping issue finalization", map[string]any{
			"pr":    pr.Number,
			"state": pr.State,
		})
		return mergedBranchCtx, nil
	}

	mergedBranchCtx = mergedBranchTransitionContext{
		PRNumber: pr.Number,
		Branch:   strings.TrimSpace(pr.HeadRefName),
	}
	o.logEvent("pr_transition", "inactive PR is merged; finalizing tracked issue links", map[string]any{
		"pr":     pr.Number,
		"state":  pr.State,
		"branch": mergedBranchCtx.Branch,
	})
	if err := o.processMergedPRIssueFinalization(ctx, pr.Number); err != nil {
		return mergedBranchCtx, err
	}
	return mergedBranchCtx, nil
}

func (o *orchestrator) handleNoOpenPR(ctx context.Context, mergedBranchCtx mergedBranchTransitionContext) error {
	o.initializeNoPRMode()
	if err := o.ensureMainReady(ctx, mergedBranchCtx); err != nil {
		return err
	}

	o.promoteIssueTriageContextToBootstrap()
	if o.state.Mode == state.ModeIssueTriage {
		if err := o.runIssueTriage(ctx); err != nil {
			return err
		}
	}
	if o.state.Mode != state.ModeTaskBootstrap {
		return fmt.Errorf("unexpected no-PR mode %q", o.state.Mode)
	}

	if o.state.BootstrapIntent == nil {
		return o.runBootstrapIntent(ctx)
	}
	return o.runBootstrapExecution(ctx)
}

func (o *orchestrator) initializeNoPRMode() {
	if o.state.Mode != "" && o.state.Mode != state.ModeManagedPR {
		return
	}
	o.logEvent("mode_transition", "initializing issue-first intake mode", map[string]any{
		"from": string(o.state.Mode),
		"to":   string(state.ModeIssueTriage),
	})
	o.state.Mode = state.ModeIssueTriage
	o.clearBootstrapContext()
}

func (o *orchestrator) promoteIssueTriageContextToBootstrap() bool {
	if o.state.Mode != state.ModeIssueTriage {
		return false
	}
	legacyPendingTaskID := strings.TrimSpace(o.state.PendingTaskID)
	if o.state.IssueTaskIntent == nil && legacyPendingTaskID == "" {
		return false
	}

	fields := map[string]any{
		"from": string(state.ModeIssueTriage),
		"to":   string(state.ModeTaskBootstrap),
	}
	if o.state.IssueTaskIntent != nil {
		fields["issue"] = o.state.IssueTaskIntent.IssueNumber
		fields["task_title"] = limitString(strings.TrimSpace(o.state.IssueTaskIntent.TaskTitle), 200)
	}
	if legacyPendingTaskID != "" {
		fields["pending_task_id"] = legacyPendingTaskID
	}
	o.logEvent("mode_transition", "issue-derived bootstrap context exists; skipping issue triage", fields)
	o.state.Mode = state.ModeTaskBootstrap
	return true
}

func (o *orchestrator) runIssueTriage(ctx context.Context) error {
	issues, err := github.ListOpenIssuesByAuthor(ctx, o.repoRoot, o.repo.FullName(), o.user)
	if err != nil {
		return fmt.Errorf("list authored open issues: %w", err)
	}
	o.state.ActiveIssue = 0
	o.clearBootstrapContext()

	var selected *github.Issue
	if len(issues) > 0 {
		selected = &issues[0]
		o.state.ActiveIssue = selected.Number
		o.logEvent("issue_selection", "selected authored issue candidate for triage", map[string]any{
			"issue": selected.Number,
			"title": selected.Title,
		})
	} else {
		o.logEvent("issue_selection", "no authored open issues found before bootstrap", map[string]any{
			"user": o.user,
		})
	}

	if selected != nil {
		beforeHead, err := git.HeadSHA(ctx, o.repoRoot)
		if err != nil {
			return fmt.Errorf("read main HEAD before issue triage: %w", err)
		}

		var report agent.Action
		prompt := o.buildIssueTriagePrompt(*selected, "")
		_, _, err = o.runAgentWithValidationOptions(ctx, agentRunOptions{
			Prompt:          prompt,
			ExpectedBranch:  o.cfg.MainBranch,
			BeforeHead:      beforeHead,
			AllowIdleOnMain: true,
			ValidateResult: func(result agent.Result, _, _ string) error {
				var validateErr error
				report, validateErr = validateIssueTriageResult(result, selected.Number)
				return validateErr
			},
			RequireCommitForDone: false,
		})
		if err != nil {
			return err
		}

		o.logEvent("issue_triage_report", "issue triage report accepted", map[string]any{
			"issue":       report.IssueNumber,
			"relevant":    report.Relevant,
			"needs_task":  report.NeedsTask,
			"analysis":    limitString(strings.TrimSpace(report.Analysis), 2000),
			"task_title":  limitString(strings.TrimSpace(report.TaskTitle), 200),
			"task_body":   limitString(strings.TrimSpace(report.TaskBody), 2000),
			"active_mode": string(state.ModeIssueTriage),
		})
		if err := o.ensureIssueTriageComment(ctx, report); err != nil {
			return err
		}
		o.clearBootstrapContext()
		if report.NeedsTask {
			o.state.IssueTaskIntent = &state.IssueTaskIntent{
				IssueNumber: report.IssueNumber,
				TaskTitle:   strings.TrimSpace(report.TaskTitle),
				TaskBody:    strings.TrimSpace(report.TaskBody),
				RecordedAt:  time.Now().UTC(),
			}
			o.logEvent("issue_task_intent", "accepted issue-derived task intent without orchestrator project-file mutation", map[string]any{
				"issue":      report.IssueNumber,
				"task_title": limitString(strings.TrimSpace(report.TaskTitle), 200),
				"task_body":  limitString(strings.TrimSpace(report.TaskBody), 2000),
			})
		}
	}

	o.logEvent("mode_transition", "issue triage complete, transitioning to task_bootstrap", map[string]any{
		"from": string(state.ModeIssueTriage),
		"to":   string(state.ModeTaskBootstrap),
	})
	o.state.Mode = state.ModeTaskBootstrap
	return nil
}

func (o *orchestrator) runBootstrapIntent(ctx context.Context) error {
	beforeHead, err := git.HeadSHA(ctx, o.repoRoot)
	if err != nil {
		return fmt.Errorf("read main HEAD before bootstrap intent run: %w", err)
	}

	var approved state.BootstrapIntent
	intentPrompt := o.buildBootstrapIntentPrompt(o.state.IssueTaskIntent, o.state.PendingTaskID, "")
	intentResult, afterHead, err := o.runAgentWithValidationOptions(ctx, agentRunOptions{
		Prompt:          intentPrompt,
		ExpectedBranch:  o.cfg.MainBranch,
		BeforeHead:      beforeHead,
		AllowIdleOnMain: true,
		SessionID:       o.state.BootstrapSessionID,
		ValidateResult: func(result agent.Result, _, _ string) error {
			var validateErr error
			approved, validateErr = o.validateBootstrapIntentResult(result, o.state.PendingTaskID)
			return validateErr
		},
		RequireCommitForDone: false,
	})
	if err != nil {
		return err
	}

	if intentResult.Terminal.Type == agent.ActionIdle {
		fmt.Printf("status: agent idle during bootstrap intent: %s\n", strings.TrimSpace(intentResult.Terminal.Reason))
		o.logEvent("agent_idle", "bootstrap intent returned idle", map[string]any{"reason": strings.TrimSpace(intentResult.Terminal.Reason)})
		o.transitionToIssueTriageMode()
		return nil
	}
	if beforeHead != afterHead {
		return fmt.Errorf("bootstrap intent run must not move commits")
	}

	o.state.BootstrapIntent = &approved
	if taskID, err := parseTaskIDFromRef(approved.TaskRef); err == nil {
		o.state.PendingTaskID = taskID
	} else {
		o.state.PendingTaskID = ""
	}
	if observedSessionID := extractCodexSessionIDFromRawOutput(intentResult.RawOutput); observedSessionID != "" {
		o.state.BootstrapSessionID = observedSessionID
		o.logEvent("bootstrap_session", "captured codex session id during intent turn", map[string]any{
			"session_id": observedSessionID,
			"task_ref":   approved.TaskRef,
		})
	}
	o.logEvent("bootstrap_intent", "approved bootstrap intent before execution", map[string]any{
		"task_ref":    approved.TaskRef,
		"summary":     limitString(approved.Summary, 500),
		"branch_slug": approved.BranchSlug,
		"branch_name": approved.BranchName,
		"pr_title":    limitString(approved.PRTitle, 300),
		"checks":      approved.Checks,
	})
	fmt.Printf("status: bootstrap intent approved for branch %s; rerun to execute task\n", approved.BranchName)
	return nil
}

func (o *orchestrator) runBootstrapExecution(ctx context.Context) error {
	intent := *o.state.BootstrapIntent
	scopeLock, err := o.newExecutionScopeLock(intent)
	if err != nil {
		return err
	}
	beforeHead, err := git.HeadSHA(ctx, o.repoRoot)
	if err != nil {
		return fmt.Errorf("read HEAD before bootstrap execution run: %w", err)
	}

	expectedBranch := intent.BranchName
	prompt := o.buildBootstrapPrompt(intent, "")
	var report executionReport
	var actionsForApply []agent.Action
	result, afterHead, err := o.runAgentWithValidationOptions(ctx, agentRunOptions{
		Prompt:                  prompt,
		ExpectedBranch:          expectedBranch,
		BeforeHead:              beforeHead,
		RequireCommitForDone:    true,
		AllowIdleOnMain:         true,
		ScopeLock:               scopeLock,
		SessionID:               o.state.BootstrapSessionID,
		FailClosedOnHeadAdvance: true,
		ValidateResult: func(result agent.Result, before, after string) error {
			if err := validateIssueUpdateActions(result.Actions); err != nil {
				return err
			}
			if err := o.validateExecutionScopeLock(scopeLock); err != nil {
				return err
			}
			validatedReport, filteredActions, err := validateExecutionReport(result, intent, expectedBranch, before, after)
			if err != nil {
				return err
			}
			if err := validateBootstrapExecutionCommitCount(ctx, o.repoRoot, before, after, result.Terminal.Type); err != nil {
				return err
			}
			report = validatedReport
			actionsForApply = filteredActions
			return nil
		},
	})
	if err != nil {
		return err
	}
	if actionsForApply != nil {
		result.Actions = actionsForApply
	}
	if expectedSessionID := strings.TrimSpace(o.state.BootstrapSessionID); expectedSessionID != "" {
		if observedSessionID := extractCodexSessionIDFromRawOutput(result.RawOutput); observedSessionID != "" && observedSessionID != expectedSessionID {
			return fmt.Errorf("bootstrap session continuity violation: expected session %q, observed %q", expectedSessionID, observedSessionID)
		}
	}

	if result.Terminal.Type == agent.ActionIdle {
		fmt.Printf("status: agent idle: %s\n", strings.TrimSpace(result.Terminal.Reason))
		o.logEvent("agent_idle", "bootstrap execution returned idle", map[string]any{"reason": strings.TrimSpace(result.Terminal.Reason)})
		o.transitionToIssueTriageMode()
		return nil
	}

	if !result.Terminal.Changes {
		return fmt.Errorf("bootstrap execution produced done action with changes=false; refusing to create PR")
	}
	if beforeHead == afterHead {
		return fmt.Errorf("bootstrap execution reported completion but no new commit was created")
	}

	title, body := normalizePRMetadata(result.Terminal)
	if strings.TrimSpace(result.Terminal.Summary) == "" && strings.TrimSpace(report.Summary) != "" {
		result.Terminal.Summary = strings.TrimSpace(report.Summary)
		title, body = normalizePRMetadata(result.Terminal)
	}
	if strings.TrimSpace(result.Terminal.PRTitle) == "" && strings.TrimSpace(intent.PRTitle) != "" {
		title = strings.TrimSpace(intent.PRTitle)
	}
	if strings.TrimSpace(result.Terminal.PRBody) == "" && strings.TrimSpace(intent.PRBody) != "" {
		body = strings.TrimSpace(intent.PRBody)
	}

	fmt.Printf("git: pushing new branch %s\n", expectedBranch)
	o.logEvent("git_push", "pushing new branch before PR creation", map[string]any{"branch": expectedBranch})
	if err := git.Push(ctx, o.repoRoot, "origin", expectedBranch); err != nil {
		return fmt.Errorf("push new branch %s: %w", expectedBranch, err)
	}

	fmt.Printf("github: creating PR from %s to %s\n", expectedBranch, o.cfg.MainBranch)
	o.logEvent("pr_create", "creating managed pull request", map[string]any{"branch": expectedBranch, "base": o.cfg.MainBranch})
	prNumber, err := github.CreatePR(ctx, o.repoRoot, title, body, o.cfg.MainBranch, expectedBranch, true)
	if err != nil {
		return fmt.Errorf("create pull request: %w", err)
	}
	taskIDForBacklink, _ := parseTaskIDFromRef(intent.TaskRef)
	if err := o.maybePostIssueDerivedPRBacklink(ctx, prNumber, o.state.ActiveIssue, taskIDForBacklink); err != nil {
		return err
	}

	o.state.ActivePR = prNumber
	o.state.ActiveBranch = expectedBranch
	o.state.ActiveTaskRef = intent.TaskRef
	o.state.Mode = state.ModeManagedPR
	o.state.ActiveIssue = 0
	fmt.Printf("status: created managed PR #%d (%s)\n", prNumber, expectedBranch)
	o.logEvent("pr_created", "managed pull request created", map[string]any{"pr": prNumber, "branch": expectedBranch})

	emptyPoll := eventPoll{IssueByID: map[int64]event{}, ReviewByID: map[int64]event{}}
	if err := o.applyActions(ctx, prNumber, result.Actions, emptyPoll); err != nil {
		return err
	}
	if err := o.processPendingIssueUpdateComments(ctx, prNumber); err != nil {
		return err
	}

	o.clearBootstrapContext()
	return nil
}

func (o *orchestrator) validateCheckoutMatchesPR(ctx context.Context, pr github.PullRequest) error {
	branch, err := git.CurrentBranch(ctx, o.repoRoot)
	if err != nil {
		return fmt.Errorf("read current branch: %w", err)
	}
	if branch != pr.HeadRefName {
		return fmt.Errorf("checkout mismatch for PR #%d: current branch is %q, expected %q", pr.Number, branch, pr.HeadRefName)
	}

	clean, status, err := git.IsClean(ctx, o.repoRoot)
	if err != nil {
		return fmt.Errorf("check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("checkout mismatch for PR #%d: working tree is dirty:\n%s", pr.Number, status)
	}

	localHead, err := git.HeadSHA(ctx, o.repoRoot)
	if err != nil {
		return fmt.Errorf("read local HEAD: %w", err)
	}
	if pr.HeadRefOid != "" && localHead != pr.HeadRefOid {
		return fmt.Errorf("checkout mismatch for PR #%d: local HEAD %s != PR head %s", pr.Number, localHead, pr.HeadRefOid)
	}

	remoteHead, err := git.RefSHA(ctx, o.repoRoot, "origin/"+pr.HeadRefName)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", pr.HeadRefName, err)
	}
	if pr.HeadRefOid != "" && remoteHead != pr.HeadRefOid {
		return fmt.Errorf("checkout mismatch for PR #%d: origin/%s is %s, PR head is %s", pr.Number, pr.HeadRefName, remoteHead, pr.HeadRefOid)
	}
	if remoteHead != localHead {
		return fmt.Errorf("checkout mismatch for PR #%d: local HEAD %s != origin/%s %s", pr.Number, localHead, pr.HeadRefName, remoteHead)
	}
	return nil
}

func (o *orchestrator) ensureMainReady(ctx context.Context, mergedBranchCtx mergedBranchTransitionContext) error {
	clean, status, err := git.IsClean(ctx, o.repoRoot)
	if err != nil {
		wrapped := fmt.Errorf("check working tree before main sync: %w", err)
		o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "check_clean", "error": wrapped.Error()})
		return wrapped
	}
	if !clean {
		wrapped := fmt.Errorf("cannot bootstrap new task with dirty working tree:\n%s", status)
		o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "check_clean", "error": wrapped.Error()})
		return wrapped
	}

	if err := git.FetchOrigin(ctx, o.repoRoot); err != nil {
		wrapped := fmt.Errorf("fetch origin: %w", err)
		o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "fetch_origin", "error": wrapped.Error()})
		return wrapped
	}

	currentBranch, err := git.CurrentBranch(ctx, o.repoRoot)
	if err != nil {
		wrapped := fmt.Errorf("read current branch: %w", err)
		o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "read_branch", "error": wrapped.Error()})
		return wrapped
	}

	mainRemoteRef := "origin/" + o.cfg.MainBranch
	mergedBranchToDelete := ""
	if currentBranch != o.cfg.MainBranch {
		allowMergedPRBranch := mergedBranchCtx.Branch != "" && currentBranch == mergedBranchCtx.Branch
		if allowMergedPRBranch {
			o.logEvent("invariant_decision", "main readiness accepted GitHub-confirmed merged branch", map[string]any{
				"pass":      true,
				"stage":     "check_merged_via_pr_state",
				"branch":    currentBranch,
				"pr":        mergedBranchCtx.PRNumber,
				"target":    mainRemoteRef,
				"pr_branch": mergedBranchCtx.Branch,
			})
		} else {
			merged, err := git.IsAncestor(ctx, o.repoRoot, "HEAD", mainRemoteRef)
			if err != nil {
				wrapped := fmt.Errorf("check whether current branch is merged into %s: %w", mainRemoteRef, err)
				o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "check_merged", "branch": currentBranch, "target": mainRemoteRef, "error": wrapped.Error()})
				return wrapped
			}
			if !merged {
				wrapped := fmt.Errorf("current branch %q is not merged into %s; refusing to start a new task", currentBranch, mainRemoteRef)
				o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "check_merged", "branch": currentBranch, "target": mainRemoteRef, "error": wrapped.Error()})
				return wrapped
			}
		}
		if err := git.Checkout(ctx, o.repoRoot, o.cfg.MainBranch); err != nil {
			wrapped := fmt.Errorf("checkout %s: %w", o.cfg.MainBranch, err)
			o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "checkout_main", "error": wrapped.Error()})
			return wrapped
		}
		mergedBranchToDelete = currentBranch
	}

	ahead, behind, err := git.AheadBehind(ctx, o.repoRoot, "HEAD", mainRemoteRef)
	if err != nil {
		wrapped := fmt.Errorf("compare HEAD with %s: %w", mainRemoteRef, err)
		o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "ahead_behind", "target": mainRemoteRef, "error": wrapped.Error()})
		return wrapped
	}
	if ahead > 0 {
		wrapped := fmt.Errorf("local %s is %d commit(s) ahead of %s; refusing automation", o.cfg.MainBranch, ahead, mainRemoteRef)
		o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "ahead_behind", "ahead": ahead, "behind": behind, "target": mainRemoteRef, "error": wrapped.Error()})
		return wrapped
	}
	if behind > 0 {
		if err := git.PullFFOnly(ctx, o.repoRoot, "origin", o.cfg.MainBranch); err != nil {
			wrapped := fmt.Errorf("fast-forward pull %s: %w", o.cfg.MainBranch, err)
			o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "pull_ff_only", "ahead": ahead, "behind": behind, "target": mainRemoteRef, "error": wrapped.Error()})
			return wrapped
		}
	}
	if mergedBranchToDelete != "" {
		if err := git.DeleteLocalBranch(ctx, o.repoRoot, mergedBranchToDelete); err != nil {
			wrapped := fmt.Errorf("delete merged local branch %s: %w", mergedBranchToDelete, err)
			o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "delete_merged_branch", "branch": mergedBranchToDelete, "error": wrapped.Error()})
			return wrapped
		}
		o.logEvent("branch_cleanup", "deleted merged local branch after main sync", map[string]any{
			"branch":      mergedBranchToDelete,
			"main_branch": o.cfg.MainBranch,
			"main_remote": mainRemoteRef,
		})
	}
	o.logEvent("invariant_decision", "main readiness passed", map[string]any{
		"pass":           true,
		"current_branch": currentBranch,
		"main_branch":    o.cfg.MainBranch,
		"main_remote":    mainRemoteRef,
		"ahead":          ahead,
		"behind":         behind,
	})
	return nil
}
