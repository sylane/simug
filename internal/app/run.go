package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"

	"simug/internal/agent"
	"simug/internal/git"
	"simug/internal/github"
	"simug/internal/runtimepaths"
	"simug/internal/state"
)

const (
	defaultPollInterval      = 30 * time.Second
	defaultMainBranch        = "main"
	defaultBranchPrefix      = "agent/"
	defaultMaxRepairAttempts = 2
	maxCommentBodyChars      = 4000
	defaultAllowedVerbs      = "do,retry,status,continue,comment,report,help"
	executionReportPrefix    = "REPORT_JSON:"
)

var bootstrapIntentSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,40}$`)
var taskRefIDPattern = regexp.MustCompile(`(?i)\btask\s+([0-9]+\.[0-9]+[a-z]?)\b`)
var planningTaskStatusPattern = regexp.MustCompile(`^- \[( |x)\] \*\*(?:\[IN_PROGRESS\] )?Task ([0-9]+\.[0-9]+[a-z]?):`)
var codexSessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F-]{8,}$`)
var defaultPromptGuidanceCandidates = []string{
	"AGENTS.md",
	"AGENT.md",
	"docs/WORKFLOW.md",
	"WORKFLOW.md",
	"docs/PLANNING.md",
	"PLANNING.md",
	"README.md",
}
var defaultPlanningGuidanceCandidates = []string{
	"docs/PLANNING.md",
	"PLANNING.md",
}

type config struct {
	PollInterval       time.Duration
	MainBranch         string
	BranchPrefix       string
	BranchPattern      *regexp.Regexp
	AgentCommand       string
	MaxRepairAttempts  int
	AllowedUsers       map[string]struct{}
	AllowedVerbs       map[string]struct{}
	GuidanceCandidates []string
	PlanningCandidates []string
}

type RunOptions struct {
	VerboseConsole bool
	Console        io.Writer
}

type orchestrator struct {
	repoRoot string
	repo     git.Repo
	user     string
	state    *state.State
	cfg      config
	runner   agent.Runner
	logger   *eventLogger
	runID    string
	tickSeq  int64

	verboseConsole bool
	console        io.Writer
}

type event struct {
	Source               string
	ID                   int64
	Author               string
	Body                 string
	CreatedAt            time.Time
	ReviewContext        *reviewCommentContext
	Commands             []string
	UnauthorizedCommands []string
}

type reviewCommentContext struct {
	Path         string
	DiffHunk     string
	Line         *int
	OriginalLine *int
	Side         string
	StartLine    *int
	StartSide    string
}

type eventPoll struct {
	Events         []event
	IssueByID      map[int64]event
	ReviewByID     map[int64]event
	MaxIssueID     int64
	MaxReviewID    int64
	MaxReviewComID int64
}

func Run(ctx context.Context, startDir string) error {
	return RunWithOptions(ctx, startDir, RunOptions{})
}

// RunOnce executes exactly one orchestration tick, persists state, and exits.
func RunOnce(ctx context.Context, startDir string) error {
	return RunOnceWithOptions(ctx, startDir, RunOptions{})
}

func RunWithOptions(ctx context.Context, startDir string, options RunOptions) error {
	return run(ctx, startDir, false, options)
}

func RunOnceWithOptions(ctx context.Context, startDir string, options RunOptions) error {
	return run(ctx, startDir, true, options)
}

func run(ctx context.Context, startDir string, once bool, options RunOptions) error {
	repoRoot, err := git.RepoRoot(ctx, startDir)
	if err != nil {
		return err
	}

	unlock, err := acquireLock(repoRoot)
	if err != nil {
		return err
	}
	defer unlock()

	logger, err := newEventLogger(repoRoot)
	if err != nil {
		return fmt.Errorf("initialize event log: %w", err)
	}

	runID := buildRunID()
	var tickSeq atomic.Int64
	var commandSeq atomic.Int64

	restoreGitTrace := git.SetCommandTraceHook(func(trace git.CommandTrace) {
		logCommandTrace(logger, runID, tickSeq.Load(), commandSeq.Add(1), "git", trace.Name, trace.Args, trace.Duration, trace.ExitCode, trace.StdoutTail, trace.StderrTail, trace.Error)
	})
	defer restoreGitTrace()
	restoreGitHubTrace := github.SetCommandTraceHook(func(trace github.CommandTrace) {
		logCommandTrace(logger, runID, tickSeq.Load(), commandSeq.Add(1), "github", trace.Name, trace.Args, trace.Duration, trace.ExitCode, trace.StdoutTail, trace.StderrTail, trace.Error)
	})
	defer restoreGitHubTrace()

	repo, err := git.ResolveGitHubRepo(ctx, repoRoot)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := preflightAgentCommand(cfg.AgentCommand); err != nil {
		return err
	}

	for _, path := range discoverGuidanceFilesWithCandidates(repoRoot, cfg.guidanceCandidates()) {
		fmt.Printf("context: found %s\n", path)
	}

	user, err := github.CurrentUser(ctx, repoRoot)
	if err != nil {
		return fmt.Errorf("resolve github user: %w", err)
	}
	if len(cfg.AllowedUsers) == 0 {
		cfg.AllowedUsers = map[string]struct{}{strings.ToLower(user): struct{}{}}
	}

	st, err := state.Load(repoRoot)
	if err != nil {
		return err
	}
	st.Repo = repo.FullName()
	if st.LastCommentID > 0 && st.LastIssueCommentID == 0 && st.LastReviewCommentID == 0 && st.LastReviewID == 0 {
		st.CursorUncertain = true
	}

	console := options.Console
	if console == nil {
		console = os.Stdout
	}

	o := &orchestrator{
		repoRoot: repoRoot,
		repo:     repo,
		user:     user,
		state:    st,
		cfg:      cfg,
		runner:   agent.Runner{Command: cfg.AgentCommand},
		logger:   logger,
		runID:    runID,
		console:  console,

		verboseConsole: options.VerboseConsole,
	}
	if err := o.recoverInterruptedAttempt(ctx); err != nil {
		return err
	}
	st.UpdatedAt = time.Now().UTC()
	if err := st.Save(repoRoot); err != nil {
		return err
	}

	fmt.Printf("repo: %s\n", repo.FullName())
	fmt.Printf("user: %s\n", user)
	fmt.Printf("poll_interval: %s\n", cfg.PollInterval)
	fmt.Printf("agent_command: %s\n", cfg.AgentCommand)
	fmt.Printf("command_authors: %s\n", strings.Join(sortedKeys(cfg.AllowedUsers), ","))
	o.logEvent("startup", "worker started", map[string]any{
		"repo":             repo.FullName(),
		"user":             user,
		"poll_interval":    cfg.PollInterval.String(),
		"agent_command":    cfg.AgentCommand,
		"command_authors":  sortedKeys(cfg.AllowedUsers),
		"allowed_commands": sortedKeys(cfg.AllowedVerbs),
		"once_mode":        once,
		"verbose_console":  options.VerboseConsole,
	})

	for {
		currentTick := tickSeq.Add(1)
		o.tickSeq = currentTick
		tickStart := time.Now()
		o.logEvent("tick_start", "tick started", nil)

		if err := o.tick(ctx); err != nil {
			o.logEvent("tick_end", "tick failed", map[string]any{
				"duration_ms": time.Since(tickStart).Milliseconds(),
				"error":       err.Error(),
			})
			return err
		}
		o.logEvent("tick_end", "tick completed", map[string]any{"duration_ms": time.Since(tickStart).Milliseconds()})

		o.state.UpdatedAt = time.Now().UTC()
		if err := o.state.Save(o.repoRoot); err != nil {
			return err
		}
		if once {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(o.cfg.PollInterval):
		}
	}
}

func (o *orchestrator) buildManagedPRPrompt(pr github.PullRequest, events []event, cursorUncertain bool, repairInstruction string) string {
	var b strings.Builder
	b.WriteString("You are Codex running under simug orchestration.\n")
	b.WriteString("Hard rules:\n")
	b.WriteString(o.promptGuidanceInstruction())
	b.WriteString("- Do commit your completed changes locally.\n")
	b.WriteString("- Follow task records discipline: update history/, update CHANGELOG.md, and commit with .git/SIMUG_COMMIT_MSG.\n")
	b.WriteString("- Do NOT push, do NOT create or modify PRs directly.\n")
	b.WriteString("- Use issue_update actions to declare issue linkage intent (fixes/impacts/relates); orchestrator owns all issue comments.\n")
	b.WriteString(fmt.Sprintf("- Accept /agent commands only from authorized users: %s.\n", strings.Join(sortedKeys(o.cfg.AllowedUsers), ",")))
	b.WriteString(fmt.Sprintf("- Supported /agent verbs: %s.\n", strings.Join(sortedKeys(o.cfg.AllowedVerbs), ",")))
	b.WriteString("- Keep this turn focused on implementation, deterministic local checks, and coordinator output.\n")
	b.WriteString("- Do NOT run environment-sensitive validation gates in this turn (for example scripts/canary-real-codex-gate.sh, self-host canaries, or network-dependent release checks); leave them for follow-up after the turn completes.\n")
	b.WriteString("- Emit machine actions only inside one bounded SIMUG coordinator envelope.\n")
	b.WriteString("- Emit exactly one coordinator begin envelope and one matching coordinator end envelope for the active turn.\n")
	b.WriteString("- Each coordinator action envelope must use event=action and carry the action JSON in payload.\n")
	b.WriteString("- When the coordinator provides a non-empty session_id for the active turn, include that same session_id in every coordinator envelope.\n")
	b.WriteString("- Emit manager-facing human messages only with prefix SIMUG_MANAGER:.\n")
	b.WriteString("- Coordinator ignores SIMUG lines outside the active turn envelope.\n")
	b.WriteString("- Unprefixed narrative text is quarantined and ignored by the coordinator.\n")
	b.WriteString("- Terminal protocol action must be exactly one of done or idle.\n\n")

	b.WriteString("Coordinator envelope schema for this managed PR turn:\n")
	b.WriteString("- SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString("- begin envelope: coordinator event=begin for the active turn_id (and session_id when provided)\n")
	b.WriteString("- action envelope payload.action may be comment(body), reply(comment_id, body), issue_update(issue_number, relation, comment), done(summary, changes, optional pr_title, optional pr_body), or idle(reason)\n")
	b.WriteString("- end envelope: coordinator event=end matching the same active turn identity\n\n")

	b.WriteString(fmt.Sprintf("Repository: %s\n", o.repo.FullName()))
	b.WriteString(fmt.Sprintf("PR: #%d\n", pr.Number))
	b.WriteString(fmt.Sprintf("Branch: %s\n", pr.HeadRefName))

	if cursorUncertain {
		b.WriteString("Cursor confidence is LOW: previous run may have missed comments. Re-evaluate context conservatively.\n")
	}
	if repairInstruction != "" {
		b.WriteString("Repair instruction:\n")
		b.WriteString(repairInstruction)
		b.WriteString("\n")
	}

	if len(events) == 0 {
		b.WriteString("No new GitHub comments detected. Continue only if you need to repair consistency.\n")
	} else {
		b.WriteString("New GitHub events since last handled cursor:\n")
		for _, e := range events {
			b.WriteString(fmt.Sprintf("- [%s #%d by %s at %s]\n", e.Source, e.ID, e.Author, e.CreatedAt.UTC().Format(time.RFC3339)))
			if len(e.Commands) > 0 {
				b.WriteString("  Authorized /agent commands:\n")
				for _, cmd := range e.Commands {
					b.WriteString("  - ")
					b.WriteString(cmd)
					b.WriteString("\n")
				}
			}
			if len(e.UnauthorizedCommands) > 0 {
				b.WriteString("  Ignored /agent commands:\n")
				for _, cmd := range e.UnauthorizedCommands {
					b.WriteString("  - ")
					b.WriteString(cmd)
					b.WriteString("\n")
				}
			}
			if e.ReviewContext != nil && hasReviewCommentContext(*e.ReviewContext) {
				b.WriteString("  Inline review context:\n")
				if e.ReviewContext.Path != "" {
					b.WriteString(fmt.Sprintf("  - File: %s\n", e.ReviewContext.Path))
				}
				if e.ReviewContext.Line != nil {
					b.WriteString(fmt.Sprintf("  - Line: %d\n", *e.ReviewContext.Line))
				}
				if e.ReviewContext.OriginalLine != nil {
					b.WriteString(fmt.Sprintf("  - Original line: %d\n", *e.ReviewContext.OriginalLine))
				}
				if e.ReviewContext.Side != "" {
					b.WriteString(fmt.Sprintf("  - Side: %s\n", e.ReviewContext.Side))
				}
				if e.ReviewContext.StartLine != nil {
					b.WriteString(fmt.Sprintf("  - Start line: %d\n", *e.ReviewContext.StartLine))
				}
				if e.ReviewContext.StartSide != "" {
					b.WriteString(fmt.Sprintf("  - Start side: %s\n", e.ReviewContext.StartSide))
				}
				if e.ReviewContext.DiffHunk != "" {
					b.WriteString("  Diff hunk:\n")
					b.WriteString(indentLines(limitString(e.ReviewContext.DiffHunk, maxCommentBodyChars), "  > "))
					b.WriteString("\n")
				}
			}
			if strings.TrimSpace(e.Body) != "" {
				b.WriteString("  Body:\n")
				b.WriteString(indentLines(limitString(e.Body, maxCommentBodyChars), "  > "))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\nWhen done, emit the bounded coordinator envelope and finish.\n")
	return b.String()
}

func hasReviewCommentContext(ctx reviewCommentContext) bool {
	return ctx.Path != "" ||
		ctx.DiffHunk != "" ||
		ctx.Line != nil ||
		ctx.OriginalLine != nil ||
		ctx.Side != "" ||
		ctx.StartLine != nil ||
		ctx.StartSide != ""
}

func (o *orchestrator) buildIssueTriagePrompt(issue github.Issue, repairInstruction string) string {
	var b strings.Builder
	b.WriteString("You are Codex running under simug orchestration.\n")
	b.WriteString("No managed open PR currently exists. Perform issue triage for the selected authored issue.\n")
	b.WriteString("Hard rules:\n")
	b.WriteString(o.promptGuidanceInstruction())
	b.WriteString("- Do NOT push, do NOT create or modify PRs directly.\n")
	b.WriteString("- Do NOT create commits in issue triage mode.\n")
	b.WriteString("- Emit machine actions only inside one bounded SIMUG coordinator envelope.\n")
	b.WriteString("- Emit exactly one coordinator begin envelope and one matching coordinator end envelope for the active turn.\n")
	b.WriteString("- Each coordinator action envelope must use event=action and carry the action JSON in payload.\n")
	b.WriteString("- When the coordinator provides a non-empty session_id for the active turn, include that same session_id in every coordinator envelope.\n")
	b.WriteString("- Emit manager-facing human messages only with prefix SIMUG_MANAGER:.\n")
	b.WriteString("- Coordinator ignores SIMUG lines outside the active turn envelope.\n")
	b.WriteString("- Unprefixed narrative text is quarantined and ignored by the coordinator.\n")
	b.WriteString("- Emit exactly one issue_report action before terminal action.\n")
	b.WriteString("- Terminal protocol action must be exactly one of done or idle.\n\n")

	b.WriteString("Protocol JSON lines:\n")
	b.WriteString("SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"<ACTIVE_TURN_ID>"}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"issue_report","issue_number":123,"relevant":true,"analysis":"...","needs_task":true,"task_title":"...","task_body":"..."}}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"done","summary":"issue triaged","changes":false}}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"idle","reason":"..."}}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"<ACTIVE_TURN_ID>"}` + "\n\n")

	b.WriteString(fmt.Sprintf("Repository: %s\n", o.repo.FullName()))
	b.WriteString(fmt.Sprintf("Selected issue: #%d\n", issue.Number))
	b.WriteString(fmt.Sprintf("Issue title: %s\n", strings.TrimSpace(issue.Title)))
	if body := strings.TrimSpace(issue.Body); body != "" {
		b.WriteString("Issue body:\n")
		b.WriteString(indentLines(limitString(body, maxCommentBodyChars), "  > "))
		b.WriteString("\n")
	}
	if repairInstruction != "" {
		b.WriteString("Repair instruction:\n")
		b.WriteString(repairInstruction)
		b.WriteString("\n")
	}
	return b.String()
}

func (o *orchestrator) buildBootstrapIntentPrompt(issueTaskIntent *state.IssueTaskIntent, pendingTaskID, repairInstruction string) string {
	var b strings.Builder
	b.WriteString("You are Codex running under simug orchestration.\n")
	b.WriteString("No managed open PR currently exists. This turn is INTENT-ONLY planning; do not modify files.\n")
	b.WriteString("Hard rules:\n")
	b.WriteString("- Stay on main and do not create/switch branches in this turn.\n")
	b.WriteString("- Do NOT edit files. Do NOT commit. Do NOT push. Do NOT create PR.\n")
	b.WriteString(o.bootstrapIntentSelectionInstruction())
	if issueTaskIntent != nil {
		b.WriteString(fmt.Sprintf("- Issue-derived intake context is active: issue #%d.\n", issueTaskIntent.IssueNumber))
		b.WriteString(fmt.Sprintf("- Issue-derived proposed task title: %s\n", limitString(strings.TrimSpace(issueTaskIntent.TaskTitle), 200)))
		b.WriteString(fmt.Sprintf("- Issue-derived proposed task body:\n%s\n", limitString(strings.TrimSpace(issueTaskIntent.TaskBody), 2000)))
		b.WriteString("- Select a concrete canonical Task <id> reference for this issue in INTENT_JSON task_ref.\n")
		b.WriteString("- Do not perform issue triage again in this turn; prepare execution intent only.\n")
	}
	if strings.TrimSpace(pendingTaskID) != "" {
		b.WriteString(fmt.Sprintf("- Legacy pending task hint: prioritize Task %s.\n", strings.TrimSpace(pendingTaskID)))
	}
	b.WriteString("- Emit exactly one intent comment and one terminal action.\n")
	b.WriteString("- Intent comment body must start with INTENT_JSON: followed by compact JSON.\n")
	b.WriteString("- Emit machine actions only inside one bounded SIMUG coordinator envelope.\n")
	b.WriteString("- Emit exactly one coordinator begin envelope and one matching coordinator end envelope for the active turn.\n")
	b.WriteString("- Each coordinator action envelope must use event=action and carry the action JSON in payload.\n")
	b.WriteString("- When the coordinator provides a non-empty session_id for the active turn, include that same session_id in every coordinator envelope.\n")
	b.WriteString("- Use SIMUG_MANAGER: for manager-facing human text; unprefixed text is quarantined.\n")
	b.WriteString("- Coordinator ignores SIMUG lines outside the active turn envelope.\n")
	if repairInstruction != "" {
		b.WriteString("Repair instruction:\n")
		b.WriteString(repairInstruction)
		b.WriteString("\n")
	}
	b.WriteString("Protocol JSON lines:\n")
	b.WriteString("SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"<ACTIVE_TURN_ID>"}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"comment","body":"INTENT_JSON:{\"task_ref\":\"Task 7.2a\",\"summary\":\"...\",\"branch_slug\":\"intent-handshake\",\"pr_title\":\"...\",\"pr_body\":\"...\",\"checks\":[\"GOCACHE=/tmp/go-build go test ./...\"]}"}}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"done","summary":"intent prepared","changes":false}}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"idle","reason":"no task available"}}` + "\n")
	b.WriteString(`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"<ACTIVE_TURN_ID>"}` + "\n")
	b.WriteString("Exactly one terminal action (done or idle) is required.\n")
	return b.String()
}

func (o *orchestrator) buildBootstrapPrompt(intent state.BootstrapIntent, repairInstruction string) string {
	var b strings.Builder
	b.WriteString("You are Codex running under simug orchestration.\n")
	b.WriteString("No managed open PR currently exists. Execute the approved bootstrap intent from the previous intent turn.\n")
	b.WriteString("Hard rules:\n")
	b.WriteString(fmt.Sprintf("- Create and use branch EXACTLY named: %s\n", intent.BranchName))
	b.WriteString(fmt.Sprintf("- Approved task reference: %s\n", strings.TrimSpace(intent.TaskRef)))
	b.WriteString(fmt.Sprintf("- Approved summary: %s\n", limitString(strings.TrimSpace(intent.Summary), 500)))
	b.WriteString(fmt.Sprintf("- Approved branch slug: %s\n", intent.BranchSlug))
	b.WriteString(fmt.Sprintf("- Approved PR title draft: %s\n", limitString(strings.TrimSpace(intent.PRTitle), 300)))
	b.WriteString(fmt.Sprintf("- Approved PR body draft: %s\n", limitString(strings.TrimSpace(intent.PRBody), 1200)))
	if taskID, err := parseTaskIDFromRef(intent.TaskRef); err == nil {
		b.WriteString(o.bootstrapExecutionScopeInstruction(taskID))
	}
	if len(intent.Checks) > 0 {
		b.WriteString(fmt.Sprintf("- Approved validation checks: %s\n", strings.Join(intent.Checks, " | ")))
	}
	b.WriteString(o.promptGuidanceInstruction())
	b.WriteString("- Implement the next task and commit changes locally when complete.\n")
	b.WriteString("- Follow task records discipline: update history/, update CHANGELOG.md, and commit with .git/SIMUG_COMMIT_MSG.\n")
	b.WriteString("- Use issue_update actions to declare issue linkage intent (fixes/impacts/relates); orchestrator owns all issue comments.\n")
	b.WriteString("- Before terminal done, emit exactly one execution report comment body prefixed with REPORT_JSON: containing task_ref, summary, branch, and head.\n")
	b.WriteString("- Do NOT push. Do NOT create PR.\n")
	b.WriteString("- This execution turn is commit-producing only; do NOT run environment-sensitive validation gates in this turn (for example scripts/canary-real-codex-gate.sh, self-host canaries, or network-dependent release checks).\n")
	b.WriteString("- If later gate or reporting follow-up is still required, finish this turn after the commit, REPORT_JSON payload, and terminal action so follow-up can happen separately.\n")
	b.WriteString("- Emit machine actions only inside one bounded SIMUG coordinator envelope.\n")
	b.WriteString("- Emit exactly one coordinator begin envelope and one matching coordinator end envelope for the active turn.\n")
	b.WriteString("- Each coordinator action envelope must use event=action and carry the action JSON in payload.\n")
	b.WriteString("- When the coordinator provides a non-empty session_id for the active turn, include that same session_id in every coordinator envelope.\n")
	b.WriteString("- Use SIMUG_MANAGER: for manager-facing human text; unprefixed text is quarantined.\n")
	b.WriteString("- Coordinator ignores SIMUG lines outside the active turn envelope.\n")
	b.WriteString("- Keep working tree clean before finishing.\n")
	if repairInstruction != "" {
		b.WriteString("Repair instruction:\n")
		b.WriteString(repairInstruction)
		b.WriteString("\n")
	}

	b.WriteString("Coordinator envelope schema for this execution turn:\n")
	b.WriteString("- SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString("- begin envelope: coordinator event=begin for the active turn_id (and session_id when provided)\n")
	b.WriteString("- action envelope payload.action may be comment(body), issue_update(issue_number, relation, comment), done(summary, changes, optional pr_title, optional pr_body), or idle(reason)\n")
	b.WriteString("- when payload.action is comment and terminal action is done, exactly one comment body must start with REPORT_JSON: and include task_ref, summary, branch, and head from this run\n")
	b.WriteString("- end envelope: coordinator event=end matching the same active turn identity\n")
	b.WriteString("Exactly one terminal action (done or idle) is required.\n")
	return b.String()
}

func (o *orchestrator) generateBranchName(slug string) string {
	ts := time.Now().UTC().Format("20060102-150405")
	normalized := sanitizeBranchSlug(slug)
	if normalized == "" {
		normalized = "next-task"
	}
	return o.cfg.BranchPrefix + ts + "-" + normalized
}

func discoverGuidanceFiles(repoRoot string) []string {
	return discoverGuidanceFilesWithCandidates(repoRoot, defaultPromptGuidanceCandidates)
}

func discoverGuidanceFilesWithCandidates(repoRoot string, candidates []string) []string {
	if strings.TrimSpace(repoRoot) == "" {
		return nil
	}
	if len(candidates) == 0 {
		candidates = defaultPromptGuidanceCandidates
	}
	files := make([]string, 0, len(candidates))
	for _, path := range candidates {
		fullPath := filepath.Join(repoRoot, path)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			files = append(files, path)
		}
	}
	return files
}

func firstExistingRelativePath(repoRoot string, candidates []string) (string, bool) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", false
	}
	if len(candidates) == 0 {
		candidates = defaultPlanningGuidanceCandidates
	}
	for _, path := range candidates {
		fullPath := filepath.Join(repoRoot, path)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

func (o *orchestrator) promptGuidanceInstruction() string {
	files := discoverGuidanceFilesWithCandidates(o.repoRoot, o.cfg.guidanceCandidates())
	if len(files) == 0 {
		return "- Repository workflow/planning guidance files are optional; none were discovered, so inspect the repository directly and follow the explicit coordinator constraints.\n"
	}
	return fmt.Sprintf("- Use repository guidance files when present: %s.\n", strings.Join(files, ", "))
}

func (o *orchestrator) bootstrapIntentSelectionInstruction() string {
	files := discoverGuidanceFilesWithCandidates(o.repoRoot, o.cfg.guidanceCandidates())
	if len(files) == 0 {
		return "- Repository workflow/planning guidance files are optional; none were discovered, so infer the safest next task scope from the repository state and any issue-intake context.\n"
	}
	return fmt.Sprintf("- Evaluate repository guidance to select the next task scope: %s.\n", strings.Join(files, ", "))
}

func (o *orchestrator) bootstrapExecutionScopeInstruction(taskID string) string {
	snapshot, err := capturePlanningStatusWithCandidates(o.repoRoot, o.cfg.planningCandidates())
	if err != nil {
		return fmt.Sprintf("- Scope lock: do not switch tasks; if any planning or task-tracking docs change, keep those changes limited to Task %s.\n", taskID)
	}
	if snapshot.supportsTask(taskID) {
		return fmt.Sprintf("- Scope lock: do not switch tasks; planning status changes in %s for other tasks are forbidden while executing Task %s.\n", snapshot.displayPath(), taskID)
	}
	if snapshot.Exists {
		return fmt.Sprintf("- Scope lock: do not switch tasks; %s does not expose supported status markers for Task %s, so keep any task-tracking changes limited to the approved task only.\n", snapshot.displayPath(), taskID)
	}
	return fmt.Sprintf("- Scope lock: do not switch tasks; no planning status file was discovered, so keep any task-tracking changes limited to Task %s only.\n", taskID)
}

func normalizePRMetadata(done agent.Action) (string, string) {
	title := strings.TrimSpace(done.PRTitle)
	if title == "" {
		summary := strings.TrimSpace(done.Summary)
		if summary == "" {
			title = "Agent task"
		} else {
			title = summary
		}
	}
	if len(title) > 72 {
		title = title[:72]
	}

	body := strings.TrimSpace(done.PRBody)
	if body == "" {
		summary := strings.TrimSpace(done.Summary)
		if summary == "" {
			body = "Implemented by Codex under simug orchestration."
		} else {
			body = summary
		}
	}
	return title, body
}

func buildAttemptArchiveDiagnostics(rawOutput string, result *agent.Result, runErr error, validationErr error) attemptArchiveDiagnostics {
	diagnostics := attemptArchiveDiagnostics{}
	diagnostics.RolloutRefs, diagnostics.SessionRefs = extractCodexPathReferences(rawOutput)

	rawProtocolLines := collectRawProtocolLines(rawOutput)
	diagnostics.RawLineCount = len(rawProtocolLines)
	if result == nil {
		for _, line := range rawProtocolLines {
			switch {
			case strings.Contains(line, `"action":"done"`):
				diagnostics.TerminalCount++
				diagnostics.TerminalTypes = append(diagnostics.TerminalTypes, "done")
			case strings.Contains(line, `"action":"idle"`):
				diagnostics.TerminalCount++
				diagnostics.TerminalTypes = append(diagnostics.TerminalTypes, "idle")
			}
			if len(diagnostics.ActionsExcerpt) < 8 {
				diagnostics.ActionsExcerpt = append(diagnostics.ActionsExcerpt, limitString(line, 220))
			}
		}
		diagnostics.ActionCount = len(rawProtocolLines)
		if runErr != nil {
			diagnostics.ParserHint = limitString(strings.TrimSpace(runErr.Error()), 600)
		}
		return diagnostics
	}

	diagnostics.ActionCount = len(result.Actions)
	diagnostics.ManagerMessages = len(result.ManagerMessages)
	diagnostics.Quarantined = len(result.QuarantinedLines)
	for _, action := range result.Actions {
		actionType := strings.TrimSpace(string(action.Type))
		if actionType != "" {
			diagnostics.ActionTypes = append(diagnostics.ActionTypes, actionType)
		}
		if action.Type == agent.ActionDone || action.Type == agent.ActionIdle {
			diagnostics.TerminalCount++
			diagnostics.TerminalTypes = append(diagnostics.TerminalTypes, actionType)
		}
		if len(diagnostics.ActionsExcerpt) < 8 {
			diagnostics.ActionsExcerpt = append(diagnostics.ActionsExcerpt, summarizeProtocolAction(action))
		}
	}
	if validationErr != nil {
		diagnostics.ParserHint = limitString(strings.TrimSpace(validationErr.Error()), 600)
	}
	return diagnostics
}

func summarizeProtocolAction(action agent.Action) string {
	switch action.Type {
	case agent.ActionComment:
		return fmt.Sprintf("comment:%s", limitString(strings.TrimSpace(action.Body), 120))
	case agent.ActionReply:
		return fmt.Sprintf("reply:%d:%s", action.CommentID, limitString(strings.TrimSpace(action.Body), 120))
	case agent.ActionIssueReport:
		return fmt.Sprintf("issue_report:%d:needs_task=%t", action.IssueNumber, action.NeedsTask)
	case agent.ActionIssueUpdate:
		return fmt.Sprintf("issue_update:%d:%s", action.IssueNumber, strings.TrimSpace(string(action.Relation)))
	case agent.ActionDone:
		return fmt.Sprintf("done:changes=%t:%s", action.Changes, limitString(strings.TrimSpace(action.Summary), 120))
	case agent.ActionIdle:
		return fmt.Sprintf("idle:%s", limitString(strings.TrimSpace(action.Reason), 120))
	default:
		return string(action.Type)
	}
}

func collectRawProtocolLines(rawOutput string) []string {
	lines := strings.Split(rawOutput, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "SIMUG:") {
			out = append(out, trimmed)
		}
	}
	return out
}

func extractCodexPathReferences(rawOutput string) ([]string, []string) {
	rolloutSet := map[string]struct{}{}
	sessionSet := map[string]struct{}{}
	tokens := strings.FieldsFunc(rawOutput, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`"'()[]{}<>,;`, r)
	})
	for _, token := range tokens {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "/sessions/") {
			sessionSet[trimmed] = struct{}{}
		}
		if strings.Contains(trimmed, "rollout-") && strings.HasSuffix(trimmed, ".jsonl") && strings.Contains(trimmed, "/") {
			rolloutSet[trimmed] = struct{}{}
		}
	}
	return sortedKeys(rolloutSet), sortedKeys(sessionSet)
}

func extractCodexSessionIDFromRawOutput(rawOutput string) string {
	_, sessionRefs := extractCodexPathReferences(rawOutput)
	for _, ref := range sessionRefs {
		parts := strings.Split(ref, "/")
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] != "sessions" {
				continue
			}
			candidate := strings.TrimSpace(parts[i+1])
			if codexSessionIDPattern.MatchString(candidate) {
				return candidate
			}
		}
	}
	return ""
}

func buildSessionResumeCommand(baseCommand, sessionID string) (string, error) {
	base := strings.TrimSpace(baseCommand)
	if strings.TrimSpace(sessionID) == "" {
		return base, nil
	}
	if !codexSessionIDPattern.MatchString(strings.TrimSpace(sessionID)) {
		return "", fmt.Errorf("invalid codex session id %q", sessionID)
	}
	if base == "codex exec" {
		return fmt.Sprintf("codex exec resume %s -", strings.TrimSpace(sessionID)), nil
	}
	return "", fmt.Errorf("session continuity requested but agent command %q does not support automatic resume", base)
}

func loadConfig() (config, error) {
	cfg := config{
		PollInterval:       defaultPollInterval,
		MainBranch:         defaultMainBranch,
		BranchPrefix:       defaultBranchPrefix,
		AgentCommand:       strings.TrimSpace(os.Getenv("SIMUG_AGENT_CMD")),
		MaxRepairAttempts:  defaultMaxRepairAttempts,
		AllowedUsers:       map[string]struct{}{},
		AllowedVerbs:       splitCSVSet(defaultAllowedVerbs),
		GuidanceCandidates: append([]string(nil), defaultPromptGuidanceCandidates...),
		PlanningCandidates: append([]string(nil), defaultPlanningGuidanceCandidates...),
	}

	if raw := strings.TrimSpace(os.Getenv("SIMUG_POLL_SECONDS")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return config{}, fmt.Errorf("invalid SIMUG_POLL_SECONDS %q", raw)
		}
		cfg.PollInterval = time.Duration(v) * time.Second
	}
	if raw := strings.TrimSpace(os.Getenv("SIMUG_MAIN_BRANCH")); raw != "" {
		cfg.MainBranch = raw
	}
	if raw := strings.TrimSpace(os.Getenv("SIMUG_BRANCH_PREFIX")); raw != "" {
		cfg.BranchPrefix = raw
	}
	if !strings.HasSuffix(cfg.BranchPrefix, "/") {
		cfg.BranchPrefix += "/"
	}
	if raw := strings.TrimSpace(os.Getenv("SIMUG_MAX_REPAIR_ATTEMPTS")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 || v > 10 {
			return config{}, fmt.Errorf("invalid SIMUG_MAX_REPAIR_ATTEMPTS %q", raw)
		}
		cfg.MaxRepairAttempts = v
	}
	if cfg.AgentCommand == "" {
		cfg.AgentCommand = defaultAgentCommand()
	}
	if raw := strings.TrimSpace(os.Getenv("SIMUG_ALLOWED_COMMAND_USERS")); raw != "" {
		cfg.AllowedUsers = splitCSVSet(raw)
	}
	if raw := strings.TrimSpace(os.Getenv("SIMUG_ALLOWED_COMMAND_VERBS")); raw != "" {
		cfg.AllowedVerbs = splitCSVSet(raw)
	}
	if raw := strings.TrimSpace(os.Getenv("SIMUG_GUIDANCE_PATHS")); raw != "" {
		paths, err := parseRelativeCSVPaths(raw)
		if err != nil {
			return config{}, fmt.Errorf("invalid SIMUG_GUIDANCE_PATHS: %w", err)
		}
		cfg.GuidanceCandidates = mergeStringSlicesUnique(paths, defaultPromptGuidanceCandidates)
	}
	if raw := strings.TrimSpace(os.Getenv("SIMUG_PLANNING_PATHS")); raw != "" {
		paths, err := parseRelativeCSVPaths(raw)
		if err != nil {
			return config{}, fmt.Errorf("invalid SIMUG_PLANNING_PATHS: %w", err)
		}
		cfg.PlanningCandidates = mergeStringSlicesUnique(paths, defaultPlanningGuidanceCandidates)
	}
	if len(cfg.AllowedVerbs) == 0 {
		return config{}, fmt.Errorf("allowed command verbs set cannot be empty")
	}

	pattern := "^" + regexp.QuoteMeta(cfg.BranchPrefix) + `[0-9]{8}-[0-9]{6}-[a-z0-9][a-z0-9-]{2,40}$`
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return config{}, fmt.Errorf("compile branch pattern: %w", err)
	}
	cfg.BranchPattern = compiled

	return cfg, nil
}

func (c config) guidanceCandidates() []string {
	if len(c.GuidanceCandidates) == 0 {
		return append([]string(nil), defaultPromptGuidanceCandidates...)
	}
	return append([]string(nil), c.GuidanceCandidates...)
}

func (c config) planningCandidates() []string {
	if len(c.PlanningCandidates) == 0 {
		return append([]string(nil), defaultPlanningGuidanceCandidates...)
	}
	return append([]string(nil), c.PlanningCandidates...)
}

func parseRelativeCSVPaths(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		path := strings.TrimSpace(part)
		if path == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		if cleaned == "." {
			return nil, fmt.Errorf("path %q resolves to repository root", path)
		}
		if filepath.IsAbs(cleaned) {
			return nil, fmt.Errorf("path %q must be repo-relative", path)
		}
		parentPrefix := ".." + string(os.PathSeparator)
		if cleaned == ".." || strings.HasPrefix(cleaned, parentPrefix) {
			return nil, fmt.Errorf("path %q escapes repository root", path)
		}
		paths = append(paths, cleaned)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no repo-relative paths configured")
	}
	return mergeStringSlicesUnique(paths), nil
}

func mergeStringSlicesUnique(slices ...[]string) []string {
	var merged []string
	seen := make(map[string]struct{})
	for _, slice := range slices {
		for _, item := range slice {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			merged = append(merged, trimmed)
		}
	}
	return merged
}

func parseAgentCommands(body, author string, allowedUsers, allowedVerbs map[string]struct{}) ([]string, []string) {
	var commands []string
	var ignored []string

	_, authorAllowed := allowedUsers[strings.ToLower(strings.TrimSpace(author))]
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "/agent") {
			if !authorAllowed {
				ignored = append(ignored, trimmed+" (unauthorized author)")
				continue
			}

			parts := strings.Fields(trimmed)
			if len(parts) < 2 {
				ignored = append(ignored, trimmed+" (missing command verb)")
				continue
			}
			verb := strings.ToLower(strings.TrimSpace(parts[1]))
			if _, ok := allowedVerbs[verb]; !ok {
				ignored = append(ignored, trimmed+" (unsupported command)")
				continue
			}
			commands = append(commands, trimmed)
		}
	}
	return commands, ignored
}

func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func limitString(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func tailString(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) <= max {
		return trimmed
	}
	if max < 4 {
		return trimmed[len(trimmed)-max:]
	}
	return "..." + trimmed[len(trimmed)-(max-3):]
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func splitCSVSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		v := strings.ToLower(strings.TrimSpace(part))
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

func (o *orchestrator) logEvent(kind, message string, fields map[string]any) {
	if o == nil || o.logger == nil {
		return
	}

	enriched := map[string]any{
		"run_id":   o.runID,
		"tick_seq": o.tickSeq,
	}
	for k, v := range fields {
		enriched[k] = v
	}

	if err := o.logger.log(kind, message, enriched); err != nil {
		fmt.Printf("warn: event log write failed: %v\n", err)
	}
}

func logCommandTrace(logger *eventLogger, runID string, tickSeq, commandSeq int64, component, name string, args []string, duration time.Duration, exitCode int, stdoutTail, stderrTail, errText string) {
	if logger == nil {
		return
	}

	fields := map[string]any{
		"run_id":      runID,
		"tick_seq":    tickSeq,
		"command_seq": commandSeq,
		"component":   component,
		"name":        name,
		"args":        append([]string(nil), args...),
		"duration_ms": duration.Milliseconds(),
		"exit_code":   exitCode,
		"stdout_tail": stdoutTail,
		"stderr_tail": stderrTail,
	}
	if strings.TrimSpace(errText) != "" {
		fields["error"] = errText
	}

	if err := logger.log("command_trace", "command executed", fields); err != nil {
		fmt.Printf("warn: event log write failed: %v\n", err)
	}
}

func buildRunID() string {
	return fmt.Sprintf("run-%s-pid-%d", time.Now().UTC().Format("20060102T150405.000000000Z"), os.Getpid())
}

func acquireLock(repoRoot string) (func(), error) {
	lockDir, err := runtimepaths.EnsureDataDir(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime dir for lock: %w", err)
	}

	lockPath := filepath.Join(lockDir, "lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(fmt.Sprintf("pid=%d\ncreated_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)))
			_ = f.Close()
			return func() {
				_ = os.Remove(lockPath)
			}, nil
		}

		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create lock file: %w", err)
		}

		stale, pid, staleErr := isLockStale(lockPath)
		if staleErr != nil {
			return nil, fmt.Errorf("existing lock %s could not be validated: %w", lockPath, staleErr)
		}
		if !stale {
			return nil, fmt.Errorf("another simug process appears to be running (lock exists: %s, pid=%d)", lockPath, pid)
		}
		if err := os.Remove(lockPath); err != nil {
			return nil, fmt.Errorf("remove stale lock file %s: %w", lockPath, err)
		}
	}

	return nil, fmt.Errorf("could not acquire lock file after stale lock recovery attempt (%s)", lockPath)
}

func isLockStale(lockPath string) (bool, int, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false, 0, fmt.Errorf("read lock file: %w", err)
	}

	var pid int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "pid=") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "pid="))
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return false, 0, fmt.Errorf("invalid pid in lock file: %q", raw)
		}
		pid = parsed
		break
	}
	if pid == 0 {
		return false, 0, fmt.Errorf("lock file missing pid field")
	}

	exists, err := processExists(pid)
	if err != nil {
		return false, pid, err
	}
	return !exists, pid, nil
}

func processExists(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, fmt.Errorf("check process %d existence: %w", pid, err)
}
