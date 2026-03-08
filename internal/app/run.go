package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

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
)

type config struct {
	PollInterval      time.Duration
	MainBranch        string
	BranchPrefix      string
	BranchPattern     *regexp.Regexp
	AgentCommand      string
	MaxRepairAttempts int
	AllowedUsers      map[string]struct{}
	AllowedVerbs      map[string]struct{}
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
}

type event struct {
	Source               string
	ID                   int64
	Author               string
	Body                 string
	CreatedAt            time.Time
	Commands             []string
	UnauthorizedCommands []string
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
	return run(ctx, startDir, false)
}

// RunOnce executes exactly one orchestration tick, persists state, and exits.
func RunOnce(ctx context.Context, startDir string) error {
	return run(ctx, startDir, true)
}

func run(ctx context.Context, startDir string, once bool) error {
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

	files := []string{
		filepath.Join(repoRoot, "AGENT.md"),
		filepath.Join(repoRoot, "docs", "WORKFLOW.md"),
		filepath.Join(repoRoot, "docs", "PLANNING.md"),
	}
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			fmt.Printf("context: found %s\n", strings.TrimPrefix(f, repoRoot+string(os.PathSeparator)))
		}
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

	o := &orchestrator{
		repoRoot: repoRoot,
		repo:     repo,
		user:     user,
		state:    st,
		cfg:      cfg,
		runner:   agent.Runner{Command: cfg.AgentCommand},
		logger:   logger,
		runID:    runID,
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

func (o *orchestrator) tick(ctx context.Context) error {
	prs, err := github.ListOpenPRsByAuthor(ctx, o.repoRoot, o.user)
	if err != nil {
		return fmt.Errorf("list open prs: %w", err)
	}

	if len(prs) > 1 {
		var items []string
		for _, pr := range prs {
			items = append(items, fmt.Sprintf("#%d (%s)", pr.Number, pr.HeadRefName))
		}
		sort.Strings(items)
		o.logEvent("invariant_violation", "multiple open PRs for current user", map[string]any{"prs": items})
		return fmt.Errorf("found %d open PRs authored by %s; expected at most one managed PR: %s", len(prs), o.user, strings.Join(items, ", "))
	}

	if len(prs) == 1 {
		pr := prs[0]
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
		o.state.Mode = state.ModeManagedPR
		o.state.ActiveIssue = 0
		o.state.PendingTaskID = ""
		return o.handleManagedPR(ctx, pr.Number)
	}

	if o.state.ActivePR != 0 {
		if err := o.handleInactiveManagedPR(ctx, o.state.ActivePR); err != nil {
			return err
		}
		fmt.Printf("status: active PR #%d is no longer open; transitioning to next-task bootstrap\n", o.state.ActivePR)
		o.logEvent("pr_transition", "active PR is no longer open", map[string]any{"active_pr": o.state.ActivePR})
	}
	o.state.ActivePR = 0
	o.state.ActiveBranch = ""
	if o.state.Mode == state.ModeManagedPR {
		o.logEvent("mode_transition", "managed PR closed, entering issue_triage mode", map[string]any{
			"from": string(state.ModeManagedPR),
			"to":   string(state.ModeIssueTriage),
		})
		o.state.Mode = state.ModeIssueTriage
	}
	if o.state.Mode == "" {
		o.state.Mode = state.ModeIssueTriage
	}
	return o.handleNoOpenPR(ctx)
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
	result, afterHead, err := o.runAgentWithValidation(ctx, prompt, pr.HeadRefName, beforeHead, false, false, func(result agent.Result) error {
		return validateIssueUpdateActions(result.Actions)
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

func (o *orchestrator) handleInactiveManagedPR(ctx context.Context, prNumber int) error {
	if prNumber <= 0 {
		return nil
	}
	pr, err := github.GetPR(ctx, o.repoRoot, prNumber)
	if err != nil {
		return fmt.Errorf("read inactive PR #%d: %w", prNumber, err)
	}

	merged := pr.MergedAt != nil || strings.EqualFold(strings.TrimSpace(pr.State), "MERGED")
	if strings.EqualFold(strings.TrimSpace(pr.State), "OPEN") && !merged {
		return fmt.Errorf("active PR #%d is still open but missing from authored open-PR set", prNumber)
	}
	if !merged {
		o.logEvent("pr_transition", "inactive PR is not merged; skipping issue finalization", map[string]any{
			"pr":    pr.Number,
			"state": pr.State,
		})
		return nil
	}

	o.logEvent("pr_transition", "inactive PR is merged; finalizing tracked issue links", map[string]any{
		"pr":    pr.Number,
		"state": pr.State,
	})
	return o.processMergedPRIssueFinalization(ctx, pr.Number)
}

func (o *orchestrator) processMergedPRIssueFinalization(ctx context.Context, prNumber int) error {
	if prNumber <= 0 {
		return nil
	}
	for i := range o.state.IssueLinks {
		link := &o.state.IssueLinks[i]
		if link.PRNumber != prNumber || link.Finalized {
			continue
		}
		if link.IssueNumber <= 0 || strings.TrimSpace(link.IdempotencyKey) == "" {
			link.Finalized = true
			continue
		}

		issue, err := github.GetIssue(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("read issue #%d for merged PR #%d finalization: %w", link.IssueNumber, prNumber, err)
		}
		if !strings.EqualFold(strings.TrimSpace(issue.Author.Login), strings.TrimSpace(o.user)) {
			o.logEvent("issue_finalize", "skipping merged-PR finalization for issue outside authenticated-user scope", map[string]any{
				"pr":           prNumber,
				"issue":        link.IssueNumber,
				"issue_author": issue.Author.Login,
				"user":         o.user,
				"relation":     link.Relation,
				"key":          link.IdempotencyKey,
			})
			link.Finalized = true
			continue
		}

		marker := issueFinalizationMarker(*link)
		comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("list issue comments for finalization marker on issue #%d: %w", link.IssueNumber, err)
		}
		hasMarker := false
		for _, comment := range comments {
			if comment.User.Login != o.user {
				continue
			}
			if strings.Contains(comment.Body, marker) {
				hasMarker = true
				break
			}
		}

		if hasMarker {
			o.logEvent("issue_finalize", "issue finalization marker already present; skipping duplicate finalization comment", map[string]any{
				"pr":       prNumber,
				"issue":    link.IssueNumber,
				"relation": link.Relation,
				"key":      link.IdempotencyKey,
				"marker":   marker,
			})
		} else {
			body := buildIssueFinalizationCommentBody(o.repo.FullName(), *link)
			o.logEvent("issue_finalize", "posting merged-PR issue finalization comment", map[string]any{
				"pr":       prNumber,
				"issue":    link.IssueNumber,
				"relation": link.Relation,
				"key":      link.IdempotencyKey,
				"marker":   marker,
			})
			if err := github.CommentIssue(ctx, o.repoRoot, link.IssueNumber, limitString(body, 60000)); err != nil {
				return fmt.Errorf("post merged-PR finalization comment on issue #%d: %w", link.IssueNumber, err)
			}
		}

		if strings.EqualFold(strings.TrimSpace(link.Relation), string(agent.IssueRelationFixes)) &&
			strings.EqualFold(strings.TrimSpace(issue.State), "OPEN") {
			o.logEvent("issue_finalize", "closing issue after merged PR fixed relation", map[string]any{
				"pr":    prNumber,
				"issue": link.IssueNumber,
				"key":   link.IdempotencyKey,
			})
			if err := github.CloseIssue(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber); err != nil {
				return fmt.Errorf("close issue #%d after merged PR finalization: %w", link.IssueNumber, err)
			}
		}

		link.Finalized = true
	}
	return nil
}

func (o *orchestrator) handleNoOpenPR(ctx context.Context) error {
	if o.state.Mode == "" || o.state.Mode == state.ModeManagedPR {
		o.logEvent("mode_transition", "initializing issue-first intake mode", map[string]any{
			"from": string(o.state.Mode),
			"to":   string(state.ModeIssueTriage),
		})
		o.state.Mode = state.ModeIssueTriage
	}

	if err := o.ensureMainReady(ctx); err != nil {
		return err
	}

	if o.state.Mode == state.ModeIssueTriage && strings.TrimSpace(o.state.PendingTaskID) != "" {
		o.logEvent("mode_transition", "pending issue-derived task exists; skipping issue triage", map[string]any{
			"from":            string(state.ModeIssueTriage),
			"to":              string(state.ModeTaskBootstrap),
			"pending_task_id": o.state.PendingTaskID,
		})
		o.state.Mode = state.ModeTaskBootstrap
	}

	if o.state.Mode == state.ModeIssueTriage {
		issues, err := github.ListOpenIssuesByAuthor(ctx, o.repoRoot, o.repo.FullName(), o.user)
		if err != nil {
			return fmt.Errorf("list authored open issues: %w", err)
		}
		o.state.ActiveIssue = 0
		o.state.PendingTaskID = ""
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
			_, _, err = o.runAgentWithValidation(ctx, prompt, o.cfg.MainBranch, beforeHead, false, true, func(result agent.Result) error {
				r, validateErr := validateIssueTriageResult(result, selected.Number)
				if validateErr != nil {
					return validateErr
				}
				report = r
				return nil
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
			o.state.PendingTaskID = ""
			if report.NeedsTask {
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
	}
	if o.state.Mode != state.ModeTaskBootstrap {
		return fmt.Errorf("unexpected no-PR mode %q", o.state.Mode)
	}

	beforeHead, err := git.HeadSHA(ctx, o.repoRoot)
	if err != nil {
		return fmt.Errorf("read main HEAD before agent run: %w", err)
	}

	expectedBranch := o.generateBranchName()
	prompt := o.buildBootstrapPrompt(expectedBranch, o.state.PendingTaskID, "")
	result, afterHead, err := o.runAgentWithValidation(ctx, prompt, expectedBranch, beforeHead, true, true, func(result agent.Result) error {
		return validateIssueUpdateActions(result.Actions)
	})
	if err != nil {
		return err
	}

	if result.Terminal.Type == agent.ActionIdle {
		fmt.Printf("status: agent idle: %s\n", strings.TrimSpace(result.Terminal.Reason))
		o.logEvent("agent_idle", "bootstrap agent returned idle", map[string]any{"reason": strings.TrimSpace(result.Terminal.Reason)})
		o.state.ActivePR = 0
		o.state.ActiveBranch = ""
		o.state.Mode = state.ModeIssueTriage
		return nil
	}

	if !result.Terminal.Changes {
		return fmt.Errorf("bootstrap run produced done action with changes=false; refusing to create PR")
	}
	if beforeHead == afterHead {
		return fmt.Errorf("bootstrap run reported completion but no new commit was created")
	}

	title, body := normalizePRMetadata(result.Terminal)

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
	if err := o.maybePostIssueDerivedPRBacklink(ctx, prNumber); err != nil {
		return err
	}

	o.state.ActivePR = prNumber
	o.state.ActiveBranch = expectedBranch
	o.state.Mode = state.ModeManagedPR
	o.state.ActiveIssue = 0
	o.state.PendingTaskID = ""
	fmt.Printf("status: created managed PR #%d (%s)\n", prNumber, expectedBranch)
	o.logEvent("pr_created", "managed pull request created", map[string]any{"pr": prNumber, "branch": expectedBranch})

	emptyPoll := eventPoll{IssueByID: map[int64]event{}, ReviewByID: map[int64]event{}}
	if err := o.applyActions(ctx, prNumber, result.Actions, emptyPoll); err != nil {
		return err
	}
	if err := o.processPendingIssueUpdateComments(ctx, prNumber); err != nil {
		return err
	}

	return nil
}

func (o *orchestrator) runAgentWithValidation(ctx context.Context, prompt, expectedBranch, beforeHead string, requireCommitForDone, allowIdleOnMain bool, validateResult func(agent.Result) error) (agent.Result, string, error) {
	currentPrompt := prompt
	for attempt := 0; attempt <= o.cfg.MaxRepairAttempts; attempt++ {
		if err := o.recordInFlightAttemptStart(attempt+1, o.cfg.MaxRepairAttempts+1, expectedBranch, beforeHead, currentPrompt); err != nil {
			return agent.Result{}, "", err
		}

		fmt.Printf("agent: running Codex (attempt %d/%d)\n", attempt+1, o.cfg.MaxRepairAttempts+1)
		runStart := time.Now()
		o.logEvent("agent_attempt", "running codex attempt", map[string]any{
			"attempt":             attempt + 1,
			"max_attempts":        o.cfg.MaxRepairAttempts + 1,
			"expected_branch":     expectedBranch,
			"require_commit_done": requireCommitForDone,
			"allow_idle_on_main":  allowIdleOnMain,
		})

		result, err := o.runner.Run(ctx, currentPrompt)
		if err != nil {
			rawOutput := agent.RawOutputFromError(err)
			if journalErr := o.recordInFlightAttemptResult(attempt+1, "", "", err.Error(), ""); journalErr != nil {
				return agent.Result{}, "", journalErr
			}
			paths, archiveErr := o.archiveAgentAttempt(
				attempt+1,
				o.cfg.MaxRepairAttempts+1,
				expectedBranch,
				beforeHead,
				"",
				currentPrompt,
				rawOutput,
				"",
				false,
				err.Error(),
				"",
			)
			if archiveErr != nil {
				o.logEvent("agent_archive_error", "failed to archive codex attempt", map[string]any{
					"attempt": attempt + 1,
					"error":   archiveErr.Error(),
				})
			} else {
				o.logEvent("agent_archive", "archived codex attempt artifacts", map[string]any{
					"attempt":       attempt + 1,
					"metadata_path": paths.MetadataPath,
					"prompt_path":   paths.PromptPath,
					"output_path":   paths.OutputPath,
				})
			}

			o.logEvent("agent_attempt", "codex attempt failed", map[string]any{
				"attempt":      attempt + 1,
				"duration_ms":  time.Since(runStart).Milliseconds(),
				"error":        err.Error(),
				"prompt_tail":  tailString(currentPrompt, 600),
				"output_tail":  tailString(rawOutput, 600),
				"terminal":     "",
				"terminal_set": false,
			})
			o.logEvent("invariant_decision", "agent execution/protocol failed", map[string]any{
				"pass":    false,
				"attempt": attempt + 1,
				"error":   err.Error(),
			})

			if attempt >= o.cfg.MaxRepairAttempts {
				return agent.Result{}, "", fmt.Errorf("agent failed after %d attempts with execution/protocol errors: %w", attempt+1, err)
			}

			currentPrompt = o.buildRepairPrompt(expectedBranch, err)
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

		afterHead, validationErr := o.validatePostAgentState(ctx, result, expectedBranch, beforeHead, requireCommitForDone, allowIdleOnMain)
		if validationErr == nil && validateResult != nil {
			validationErr = validateResult(result)
		}
		if journalErr := o.recordInFlightAttemptResult(attempt+1, afterHead, string(result.Terminal.Type), "", errorText(validationErr)); journalErr != nil {
			return agent.Result{}, "", journalErr
		}
		paths, archiveErr := o.archiveAgentAttempt(
			attempt+1,
			o.cfg.MaxRepairAttempts+1,
			expectedBranch,
			beforeHead,
			afterHead,
			currentPrompt,
			result.RawOutput,
			string(result.Terminal.Type),
			result.Terminal.Changes,
			"",
			errorText(validationErr),
		)
		if archiveErr != nil {
			o.logEvent("agent_archive_error", "failed to archive codex attempt", map[string]any{
				"attempt": attempt + 1,
				"error":   archiveErr.Error(),
			})
		} else {
			o.logEvent("agent_archive", "archived codex attempt artifacts", map[string]any{
				"attempt":       attempt + 1,
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
				"expected_branch":      expectedBranch,
				"before_head":          beforeHead,
				"after_head":           afterHead,
				"terminal":             string(result.Terminal.Type),
				"terminal_has_changes": result.Terminal.Changes,
			})
			return result, afterHead, nil
		}
		o.logEvent("invariant_decision", "post-agent validation failed", map[string]any{
			"pass":                 false,
			"attempt":              attempt + 1,
			"expected_branch":      expectedBranch,
			"before_head":          beforeHead,
			"after_head":           afterHead,
			"terminal":             string(result.Terminal.Type),
			"terminal_has_changes": result.Terminal.Changes,
			"error":                validationErr.Error(),
		})
		if attempt >= o.cfg.MaxRepairAttempts {
			return agent.Result{}, "", fmt.Errorf("agent failed validation after %d attempts: %w", attempt+1, validationErr)
		}

		currentPrompt = o.buildRepairPrompt(expectedBranch, validationErr)
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
	case agent.ActionIdle:
		if beforeHead != afterHead {
			return "", fmt.Errorf("idle action emitted but commits changed during run")
		}
	}

	return afterHead, nil
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

func (o *orchestrator) ensureIssueTriageComment(ctx context.Context, report agent.Action) error {
	marker := issueTriageMarker(report)
	comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), report.IssueNumber)
	if err != nil {
		return fmt.Errorf("list issue comments for triage marker on issue #%d: %w", report.IssueNumber, err)
	}
	for _, comment := range comments {
		if comment.User.Login != o.user {
			continue
		}
		if strings.Contains(comment.Body, marker) {
			o.logEvent("issue_triage_comment", "triage marker already present; skipping duplicate issue comment", map[string]any{
				"issue":  report.IssueNumber,
				"marker": marker,
			})
			return nil
		}
	}

	body := buildIssueTriageCommentBody(report)
	o.logEvent("issue_triage_comment", "posting deterministic issue triage analysis comment", map[string]any{
		"issue":      report.IssueNumber,
		"marker":     marker,
		"needs_task": report.NeedsTask,
		"relevant":   report.Relevant,
	})
	if err := github.CommentIssue(ctx, o.repoRoot, report.IssueNumber, limitString(body, 60000)); err != nil {
		return fmt.Errorf("post issue triage comment on issue #%d: %w", report.IssueNumber, err)
	}
	return nil
}

func issueTriageMarker(report agent.Action) string {
	return fmt.Sprintf("<!-- simug:issue-triage:v1 issue=%d relevant=%t needs_task=%t -->", report.IssueNumber, report.Relevant, report.NeedsTask)
}

func buildIssueTriageCommentBody(report agent.Action) string {
	var b strings.Builder
	b.WriteString(issueTriageMarker(report))
	b.WriteString("\n")
	b.WriteString("### simug issue triage analysis\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", report.IssueNumber))
	b.WriteString(fmt.Sprintf("- Relevant: %t\n", report.Relevant))
	b.WriteString(fmt.Sprintf("- Needs task: %t\n", report.NeedsTask))
	b.WriteString("\n")
	b.WriteString("Analysis:\n")
	b.WriteString(strings.TrimSpace(report.Analysis))
	b.WriteString("\n")
	if report.NeedsTask {
		b.WriteString("\n")
		b.WriteString("Proposed task title:\n")
		b.WriteString(strings.TrimSpace(report.TaskTitle))
		b.WriteString("\n\n")
		b.WriteString("Proposed task body:\n")
		b.WriteString(strings.TrimSpace(report.TaskBody))
		b.WriteString("\n")
	}
	return b.String()
}

func (o *orchestrator) maybePostIssueDerivedPRBacklink(ctx context.Context, prNumber int) error {
	issueNumber := o.state.ActiveIssue
	taskID := strings.TrimSpace(o.state.PendingTaskID)
	if issueNumber <= 0 {
		return nil
	}

	marker := issuePRBacklinkMarker(issueNumber, taskID, prNumber)
	comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), issueNumber)
	if err != nil {
		return fmt.Errorf("list issue comments for PR backlink marker on issue #%d: %w", issueNumber, err)
	}
	for _, comment := range comments {
		if comment.User.Login != o.user {
			continue
		}
		if strings.Contains(comment.Body, marker) {
			o.logEvent("issue_backlink", "PR backlink marker already present; skipping duplicate issue comment", map[string]any{
				"issue":   issueNumber,
				"task_id": taskID,
				"pr":      prNumber,
				"marker":  marker,
			})
			return nil
		}
	}

	body := buildIssuePRBacklinkCommentBody(o.repo.FullName(), issueNumber, taskID, prNumber)
	o.logEvent("issue_backlink", "posting issue-to-PR backlink comment", map[string]any{
		"issue":   issueNumber,
		"task_id": taskID,
		"pr":      prNumber,
		"marker":  marker,
	})
	if err := github.CommentIssue(ctx, o.repoRoot, issueNumber, limitString(body, 60000)); err != nil {
		return fmt.Errorf("post issue-to-PR backlink comment on issue #%d: %w", issueNumber, err)
	}
	return nil
}

func issuePRBacklinkMarker(issueNumber int, taskID string, prNumber int) string {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Sprintf("<!-- simug:issue-pr-link:v1 issue=%d pr=%d -->", issueNumber, prNumber)
	}
	return fmt.Sprintf("<!-- simug:issue-pr-link:v1 issue=%d task=%s pr=%d -->", issueNumber, strings.TrimSpace(taskID), prNumber)
}

func buildIssuePRBacklinkCommentBody(repoFullName string, issueNumber int, taskID string, prNumber int) string {
	url := fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, prNumber)
	var b strings.Builder
	b.WriteString(issuePRBacklinkMarker(issueNumber, taskID, prNumber))
	b.WriteString("\n")
	b.WriteString("### simug implementation PR link\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", issueNumber))
	if strings.TrimSpace(taskID) != "" {
		b.WriteString(fmt.Sprintf("- Task: Task %s\n", strings.TrimSpace(taskID)))
	}
	b.WriteString(fmt.Sprintf("- PR: #%d (%s)\n", prNumber, url))
	return b.String()
}

func (o *orchestrator) processPendingIssueUpdateComments(ctx context.Context, prNumber int) error {
	if prNumber <= 0 {
		return nil
	}
	for i := range o.state.IssueLinks {
		link := &o.state.IssueLinks[i]
		if link.PRNumber != prNumber || link.CommentPosted {
			continue
		}
		if link.IssueNumber <= 0 || strings.TrimSpace(link.IdempotencyKey) == "" {
			continue
		}

		issue, err := github.GetIssue(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("read issue #%d for issue update intent: %w", link.IssueNumber, err)
		}
		if !strings.EqualFold(strings.TrimSpace(issue.Author.Login), strings.TrimSpace(o.user)) {
			o.logEvent("issue_update_comment", "skipping issue update for issue outside authenticated-user scope", map[string]any{
				"pr":           prNumber,
				"issue":        link.IssueNumber,
				"issue_author": issue.Author.Login,
				"user":         o.user,
				"relation":     link.Relation,
				"key":          link.IdempotencyKey,
			})
			continue
		}

		marker := issueUpdateMarker(*link)
		comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("list comments for issue update marker on issue #%d: %w", link.IssueNumber, err)
		}
		exists := false
		for _, comment := range comments {
			if comment.User.Login != o.user {
				continue
			}
			if strings.Contains(comment.Body, marker) {
				exists = true
				break
			}
		}
		if exists {
			link.CommentPosted = true
			o.logEvent("issue_update_comment", "issue update marker already present; marking as posted", map[string]any{
				"pr":       prNumber,
				"issue":    link.IssueNumber,
				"relation": link.Relation,
				"key":      link.IdempotencyKey,
				"marker":   marker,
			})
			continue
		}

		body := buildIssueUpdateCommentBody(o.repo.FullName(), *link, o.state.PendingTaskID)
		o.logEvent("issue_update_comment", "posting issue update comment from tracked linkage intent", map[string]any{
			"pr":       prNumber,
			"issue":    link.IssueNumber,
			"relation": link.Relation,
			"key":      link.IdempotencyKey,
			"marker":   marker,
		})
		if err := github.CommentIssue(ctx, o.repoRoot, link.IssueNumber, limitString(body, 60000)); err != nil {
			return fmt.Errorf("post issue update comment on issue #%d: %w", link.IssueNumber, err)
		}
		link.CommentPosted = true
	}
	return nil
}

func issueUpdateMarker(link state.IssueLink) string {
	return fmt.Sprintf(
		"<!-- simug:issue-update:v1 issue=%d relation=%s key=%s pr=%d -->",
		link.IssueNumber,
		strings.TrimSpace(link.Relation),
		strings.TrimSpace(link.IdempotencyKey),
		link.PRNumber,
	)
}

func issueFinalizationMarker(link state.IssueLink) string {
	return fmt.Sprintf(
		"<!-- simug:issue-finalize:v1 issue=%d relation=%s key=%s pr=%d -->",
		link.IssueNumber,
		strings.TrimSpace(link.Relation),
		strings.TrimSpace(link.IdempotencyKey),
		link.PRNumber,
	)
}

func buildIssueUpdateCommentBody(repoFullName string, link state.IssueLink, taskID string) string {
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, link.PRNumber)
	var b strings.Builder
	b.WriteString(issueUpdateMarker(link))
	b.WriteString("\n")
	b.WriteString("### simug issue linkage update\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", link.IssueNumber))
	b.WriteString(fmt.Sprintf("- Relation: %s\n", strings.TrimSpace(link.Relation)))
	b.WriteString(fmt.Sprintf("- PR: #%d (%s)\n", link.PRNumber, prURL))
	if strings.TrimSpace(taskID) != "" {
		b.WriteString(fmt.Sprintf("- Task context: Task %s\n", strings.TrimSpace(taskID)))
	}
	b.WriteString("\n")
	b.WriteString("Context:\n")
	b.WriteString(strings.TrimSpace(link.CommentBody))
	b.WriteString("\n")
	return b.String()
}

func buildIssueFinalizationCommentBody(repoFullName string, link state.IssueLink) string {
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, link.PRNumber)
	relation := strings.TrimSpace(link.Relation)
	var b strings.Builder
	b.WriteString(issueFinalizationMarker(link))
	b.WriteString("\n")
	b.WriteString("### simug merged PR issue finalization\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", link.IssueNumber))
	b.WriteString(fmt.Sprintf("- Relation: %s\n", relation))
	b.WriteString(fmt.Sprintf("- PR: #%d (%s)\n", link.PRNumber, prURL))
	if strings.EqualFold(relation, string(agent.IssueRelationFixes)) {
		b.WriteString("- Outcome: closing issue because this PR is marked as a fix\n")
	} else {
		b.WriteString("- Outcome: informational update only (issue remains open)\n")
	}
	b.WriteString("\n")
	b.WriteString("Context:\n")
	b.WriteString(strings.TrimSpace(link.CommentBody))
	b.WriteString("\n")
	return b.String()
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

func (o *orchestrator) ensureMainReady(ctx context.Context) error {
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
	if currentBranch != o.cfg.MainBranch {
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
		if err := git.Checkout(ctx, o.repoRoot, o.cfg.MainBranch); err != nil {
			wrapped := fmt.Errorf("checkout %s: %w", o.cfg.MainBranch, err)
			o.logEvent("invariant_decision", "main readiness failed", map[string]any{"pass": false, "stage": "checkout_main", "error": wrapped.Error()})
			return wrapped
		}
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

func (o *orchestrator) pollEvents(ctx context.Context, prNumber int) (eventPoll, error) {
	issueComments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), prNumber)
	if err != nil {
		return eventPoll{}, fmt.Errorf("list issue comments: %w", err)
	}
	reviewComments, err := github.ListReviewComments(ctx, o.repoRoot, o.repo.FullName(), prNumber)
	if err != nil {
		return eventPoll{}, fmt.Errorf("list review comments: %w", err)
	}
	reviews, err := github.ListReviews(ctx, o.repoRoot, o.repo.FullName(), prNumber)
	if err != nil {
		return eventPoll{}, fmt.Errorf("list reviews: %w", err)
	}

	out := eventPoll{
		IssueByID:  make(map[int64]event),
		ReviewByID: make(map[int64]event),
	}

	for _, c := range issueComments {
		out.MaxIssueID = maxInt64(out.MaxIssueID, c.ID)
		if !o.state.CursorUncertain && c.ID <= o.state.LastIssueCommentID {
			continue
		}
		e := event{Source: "issue_comment", ID: c.ID, Author: c.User.Login, Body: limitString(c.Body, maxCommentBodyChars), CreatedAt: c.CreatedAt}
		e.Commands, e.UnauthorizedCommands = parseAgentCommands(e.Body, e.Author, o.cfg.AllowedUsers, o.cfg.AllowedVerbs)
		out.Events = append(out.Events, e)
		out.IssueByID[c.ID] = e
	}

	for _, c := range reviewComments {
		out.MaxReviewComID = maxInt64(out.MaxReviewComID, c.ID)
		if !o.state.CursorUncertain && c.ID <= o.state.LastReviewCommentID {
			continue
		}
		e := event{Source: "review_comment", ID: c.ID, Author: c.User.Login, Body: limitString(c.Body, maxCommentBodyChars), CreatedAt: c.CreatedAt}
		e.Commands, e.UnauthorizedCommands = parseAgentCommands(e.Body, e.Author, o.cfg.AllowedUsers, o.cfg.AllowedVerbs)
		out.Events = append(out.Events, e)
		out.ReviewByID[c.ID] = e
	}

	for _, r := range reviews {
		out.MaxReviewID = maxInt64(out.MaxReviewID, r.ID)
		if !o.state.CursorUncertain && r.ID <= o.state.LastReviewID {
			continue
		}
		createdAt := time.Now().UTC()
		if r.SubmittedAt != nil {
			createdAt = *r.SubmittedAt
		}
		e := event{Source: "review", ID: r.ID, Author: r.User.Login, Body: limitString(r.Body, maxCommentBodyChars), CreatedAt: createdAt}
		e.Commands, e.UnauthorizedCommands = parseAgentCommands(e.Body, e.Author, o.cfg.AllowedUsers, o.cfg.AllowedVerbs)
		out.Events = append(out.Events, e)
	}

	sort.Slice(out.Events, func(i, j int) bool {
		if out.Events[i].CreatedAt.Equal(out.Events[j].CreatedAt) {
			return out.Events[i].ID < out.Events[j].ID
		}
		return out.Events[i].CreatedAt.Before(out.Events[j].CreatedAt)
	})

	return out, nil
}

func (o *orchestrator) applyActions(ctx context.Context, prNumber int, actions []agent.Action, poll eventPoll) error {
	for _, a := range actions {
		switch a.Type {
		case agent.ActionComment:
			o.logEvent("github_comment", "posting PR comment", map[string]any{"pr": prNumber})
			if err := github.CommentPR(ctx, o.repoRoot, prNumber, limitString(strings.TrimSpace(a.Body), 60000)); err != nil {
				return fmt.Errorf("post PR comment: %w", err)
			}
		case agent.ActionReply:
			body := limitString(strings.TrimSpace(a.Body), 60000)
			if replyEvent, ok := poll.ReviewByID[a.CommentID]; ok {
				o.logEvent("github_reply", "replying to review comment", map[string]any{"pr": prNumber, "comment_id": replyEvent.ID})
				if err := github.ReplyToReviewComment(ctx, o.repoRoot, o.repo.FullName(), replyEvent.ID, body); err != nil {
					return fmt.Errorf("reply to review comment %d: %w", replyEvent.ID, err)
				}
				continue
			}

			if issueEvent, ok := poll.IssueByID[a.CommentID]; ok {
				mention := ""
				if issueEvent.Author != "" {
					mention = "@" + issueEvent.Author + " "
				}
				o.logEvent("github_reply_fallback", "replying through PR comment fallback", map[string]any{"pr": prNumber, "comment_id": issueEvent.ID})
				if err := github.CommentPR(ctx, o.repoRoot, prNumber, mention+body); err != nil {
					return fmt.Errorf("fallback reply for issue comment %d: %w", issueEvent.ID, err)
				}
				continue
			}

			o.logEvent("github_reply_fallback", "reply target not found, posting regular comment", map[string]any{"pr": prNumber, "comment_id": a.CommentID})
			if err := github.CommentPR(ctx, o.repoRoot, prNumber, body); err != nil {
				return fmt.Errorf("reply fallback comment for unknown comment id %d: %w", a.CommentID, err)
			}
		case agent.ActionDone, agent.ActionIdle:
			// Terminal actions are consumed by orchestrator state flow.
		case agent.ActionIssueUpdate:
			key := issueUpdateIdempotencyKey(prNumber, a)
			if o.hasIssueLinkByKey(key) {
				o.logEvent("issue_update_intent", "duplicate issue update intent already tracked", map[string]any{
					"pr":    prNumber,
					"issue": a.IssueNumber,
					"key":   key,
				})
				continue
			}
			link := state.IssueLink{
				PRNumber:       prNumber,
				IssueNumber:    a.IssueNumber,
				Relation:       string(a.Relation),
				CommentBody:    strings.TrimSpace(a.CommentBody),
				Provenance:     fmt.Sprintf("run=%s tick=%d", o.runID, o.tickSeq),
				IdempotencyKey: key,
				RecordedAt:     time.Now().UTC(),
			}
			o.state.IssueLinks = append(o.state.IssueLinks, link)
			o.logEvent("issue_update_intent", "tracked issue update intent in worker state", map[string]any{
				"pr":           prNumber,
				"issue":        a.IssueNumber,
				"relation":     string(a.Relation),
				"key":          key,
				"comment_tail": tailString(strings.TrimSpace(a.CommentBody), 200),
			})
		default:
			return fmt.Errorf("unsupported action type %q", a.Type)
		}
	}
	return nil
}

func (o *orchestrator) hasIssueLinkByKey(key string) bool {
	for _, link := range o.state.IssueLinks {
		if strings.TrimSpace(link.IdempotencyKey) == strings.TrimSpace(key) {
			return true
		}
	}
	return false
}

func issueUpdateIdempotencyKey(prNumber int, action agent.Action) string {
	canonical := fmt.Sprintf(
		"pr=%d|issue=%d|relation=%s|comment=%s",
		prNumber,
		action.IssueNumber,
		strings.TrimSpace(string(action.Relation)),
		normalizeOneLine(action.CommentBody),
	)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func (o *orchestrator) buildManagedPRPrompt(pr github.PullRequest, events []event, cursorUncertain bool, repairInstruction string) string {
	var b strings.Builder
	b.WriteString("You are Codex running under simug orchestration.\n")
	b.WriteString("Hard rules:\n")
	b.WriteString("- Follow docs/WORKFLOW.md and docs/PLANNING.md for process and task planning.\n")
	b.WriteString("- Do commit your completed changes locally.\n")
	b.WriteString("- Follow task records discipline: update history/, update CHANGELOG.md, and commit with .git/SIMUG_COMMIT_MSG.\n")
	b.WriteString("- Do NOT push, do NOT create or modify PRs directly.\n")
	b.WriteString("- Use issue_update actions to declare issue linkage intent (fixes/impacts/relates); orchestrator owns all issue comments.\n")
	b.WriteString(fmt.Sprintf("- Accept /agent commands only from authorized users: %s.\n", strings.Join(sortedKeys(o.cfg.AllowedUsers), ",")))
	b.WriteString(fmt.Sprintf("- Supported /agent verbs: %s.\n", strings.Join(sortedKeys(o.cfg.AllowedVerbs), ",")))
	b.WriteString("- Emit machine actions only with protocol lines starting exactly with SIMUG:.\n")
	b.WriteString("- Emit manager-facing human messages only with prefix SIMUG_MANAGER:.\n")
	b.WriteString("- Unprefixed narrative text is quarantined and ignored by the coordinator.\n")
	b.WriteString("- Terminal protocol action must be exactly one of done or idle.\n\n")

	b.WriteString("Protocol JSON lines:\n")
	b.WriteString("SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString(`SIMUG: {"action":"comment","body":"..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"reply","comment_id":123,"body":"..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"issue_update","issue_number":123,"relation":"fixes","comment":"Task implementation covers this issue because ..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"optional","pr_body":"optional"}` + "\n")
	b.WriteString(`SIMUG: {"action":"idle","reason":"..."}` + "\n\n")

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
			if strings.TrimSpace(e.Body) != "" {
				b.WriteString("  Body:\n")
				b.WriteString(indentLines(limitString(e.Body, maxCommentBodyChars), "  > "))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\nWhen done, emit protocol lines and finish.\n")
	return b.String()
}

func (o *orchestrator) buildIssueTriagePrompt(issue github.Issue, repairInstruction string) string {
	var b strings.Builder
	b.WriteString("You are Codex running under simug orchestration.\n")
	b.WriteString("No managed open PR currently exists. Perform issue triage for the selected authored issue.\n")
	b.WriteString("Hard rules:\n")
	b.WriteString("- Follow docs/WORKFLOW.md and docs/PLANNING.md for process and task planning.\n")
	b.WriteString("- Do NOT push, do NOT create or modify PRs directly.\n")
	b.WriteString("- Do NOT create commits in issue triage mode.\n")
	b.WriteString("- Emit machine actions only with protocol lines starting exactly with SIMUG:.\n")
	b.WriteString("- Emit manager-facing human messages only with prefix SIMUG_MANAGER:.\n")
	b.WriteString("- Unprefixed narrative text is quarantined and ignored by the coordinator.\n")
	b.WriteString("- Emit exactly one issue_report action before terminal action.\n")
	b.WriteString("- Terminal protocol action must be exactly one of done or idle.\n\n")

	b.WriteString("Protocol JSON lines:\n")
	b.WriteString("SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString(`SIMUG: {"action":"issue_report","issue_number":123,"relevant":true,"analysis":"...","needs_task":true,"task_title":"...","task_body":"..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"done","summary":"issue triaged","changes":false}` + "\n")
	b.WriteString(`SIMUG: {"action":"idle","reason":"..."}` + "\n\n")

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

func (o *orchestrator) buildBootstrapPrompt(expectedBranch, pendingTaskID, repairInstruction string) string {
	var b strings.Builder
	b.WriteString("You are Codex running under simug orchestration.\n")
	b.WriteString("No managed open PR currently exists. Start the next task from docs/PLANNING.md and docs/WORKFLOW.md.\n")
	b.WriteString("Hard rules:\n")
	b.WriteString(fmt.Sprintf("- Create and use branch EXACTLY named: %s\n", expectedBranch))
	if strings.TrimSpace(pendingTaskID) != "" {
		b.WriteString(fmt.Sprintf("- Start specifically with Task %s from docs/PLANNING.md before any other pending task.\n", strings.TrimSpace(pendingTaskID)))
	}
	b.WriteString("- Implement the next task and commit changes locally when complete.\n")
	b.WriteString("- Follow task records discipline: update history/, update CHANGELOG.md, and commit with .git/SIMUG_COMMIT_MSG.\n")
	b.WriteString("- Use issue_update actions to declare issue linkage intent (fixes/impacts/relates); orchestrator owns all issue comments.\n")
	b.WriteString("- Do NOT push. Do NOT create PR.\n")
	b.WriteString("- Use SIMUG_MANAGER: for manager-facing human text; unprefixed text is quarantined.\n")
	b.WriteString("- Keep working tree clean before finishing.\n")
	if repairInstruction != "" {
		b.WriteString("Repair instruction:\n")
		b.WriteString(repairInstruction)
		b.WriteString("\n")
	}

	b.WriteString("Protocol JSON lines:\n")
	b.WriteString("SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString(`SIMUG: {"action":"comment","body":"..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"issue_update","issue_number":123,"relation":"relates","comment":"This task has direct impact on this issue because ..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"...","pr_body":"..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"idle","reason":"no task available"}` + "\n")
	b.WriteString("Exactly one terminal action (done or idle) is required.\n")
	return b.String()
}

func (o *orchestrator) buildRepairPrompt(expectedBranch string, validationErr error) string {
	var b strings.Builder
	b.WriteString("Your previous run violated simug validation checks.\n")
	b.WriteString("Fix repository consistency and emit protocol lines again.\n")
	b.WriteString("Violation:\n")
	b.WriteString(validationErr.Error())
	b.WriteString("\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- follow docs/WORKFLOW.md and docs/PLANNING.md\n")
	b.WriteString("- commit local changes when task is complete\n")
	b.WriteString("- maintain task records: history/, CHANGELOG.md, and .git/SIMUG_COMMIT_MSG\n")
	b.WriteString("- use issue_update actions for issue linkage intent; do not comment on issues directly\n")
	b.WriteString("- never push or create/update PR directly\n")
	b.WriteString("- use SIMUG_MANAGER: for manager-facing messages; unprefixed text is quarantined\n")
	b.WriteString(fmt.Sprintf("- branch must be %q (or %q if terminal action is idle)\n", expectedBranch, o.cfg.MainBranch))
	b.WriteString("- keep the working tree clean before finishing\n")
	b.WriteString("Protocol JSON lines:\n")
	b.WriteString("SIMUG_MANAGER: <human-friendly manager message>\n")
	b.WriteString(`SIMUG: {"action":"comment","body":"..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"reply","comment_id":123,"body":"..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"issue_update","issue_number":123,"relation":"impacts","comment":"This work affects this issue because ..."}` + "\n")
	b.WriteString(`SIMUG: {"action":"done","summary":"...","changes":true}` + "\n")
	b.WriteString(`SIMUG: {"action":"idle","reason":"..."}` + "\n")
	return b.String()
}

func (o *orchestrator) generateBranchName() string {
	ts := time.Now().UTC().Format("20060102-150405")
	return o.cfg.BranchPrefix + ts + "-next-task"
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

func loadConfig() (config, error) {
	cfg := config{
		PollInterval:      defaultPollInterval,
		MainBranch:        defaultMainBranch,
		BranchPrefix:      defaultBranchPrefix,
		AgentCommand:      strings.TrimSpace(os.Getenv("SIMUG_AGENT_CMD")),
		MaxRepairAttempts: defaultMaxRepairAttempts,
		AllowedUsers:      map[string]struct{}{},
		AllowedVerbs:      splitCSVSet(defaultAllowedVerbs),
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
