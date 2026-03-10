package app

import (
	"testing"
	"time"

	"simug/internal/github"
	"simug/internal/state"
)

func TestEnterManagedPRModeClearsBootstrapContext(t *testing.T) {
	o := orchestrator{
		state: &state.State{
			Mode:               state.ModeTaskBootstrap,
			ActivePR:           7,
			ActiveBranch:       "agent/old-task",
			ActiveTaskRef:      "Task 7.3",
			ActiveIssue:        11,
			PendingTaskID:      "7.4",
			IssueTaskIntent:    &state.IssueTaskIntent{IssueNumber: 11, TaskTitle: "x", TaskBody: "y", RecordedAt: time.Now().UTC()},
			BootstrapIntent:    &state.BootstrapIntent{TaskRef: "Task 7.4", BranchName: "agent/new-task"},
			BootstrapSessionID: "session-123",
		},
	}

	o.enterManagedPRMode(github.PullRequest{
		Number:      42,
		HeadRefName: "agent/20260310-183133-modularize-orchestration-loop",
	})

	if o.state.Mode != state.ModeManagedPR {
		t.Fatalf("mode=%q, want %q", o.state.Mode, state.ModeManagedPR)
	}
	if o.state.ActivePR != 42 || o.state.ActiveBranch != "agent/20260310-183133-modularize-orchestration-loop" {
		t.Fatalf("managed PR state not updated: %#v", o.state)
	}
	if o.state.ActiveIssue != 0 || o.state.PendingTaskID != "" || o.state.IssueTaskIntent != nil || o.state.BootstrapIntent != nil || o.state.BootstrapSessionID != "" {
		t.Fatalf("bootstrap context was not cleared: %#v", o.state)
	}
	if o.state.ActiveTaskRef != "" {
		t.Fatalf("active task ref=%q, want cleared for new managed PR", o.state.ActiveTaskRef)
	}
}

func TestTransitionToIssueTriageModeClearsManagedAndBootstrapState(t *testing.T) {
	o := orchestrator{
		state: &state.State{
			Mode:               state.ModeManagedPR,
			ActivePR:           42,
			ActiveBranch:       "agent/task",
			ActiveTaskRef:      "Task 7.4",
			ActiveIssue:        9,
			PendingTaskID:      "7.4",
			IssueTaskIntent:    &state.IssueTaskIntent{IssueNumber: 9, TaskTitle: "x", TaskBody: "y", RecordedAt: time.Now().UTC()},
			BootstrapIntent:    &state.BootstrapIntent{TaskRef: "Task 7.4", BranchName: "agent/task"},
			BootstrapSessionID: "session-123",
		},
	}

	o.transitionToIssueTriageMode()

	if o.state.Mode != state.ModeIssueTriage {
		t.Fatalf("mode=%q, want %q", o.state.Mode, state.ModeIssueTriage)
	}
	if o.state.ActivePR != 0 || o.state.ActiveBranch != "" || o.state.ActiveTaskRef != "" || o.state.ActiveIssue != 0 {
		t.Fatalf("managed PR context not cleared: %#v", o.state)
	}
	if o.state.PendingTaskID != "" || o.state.IssueTaskIntent != nil || o.state.BootstrapIntent != nil || o.state.BootstrapSessionID != "" {
		t.Fatalf("bootstrap context not cleared: %#v", o.state)
	}
}

func TestPromoteIssueTriageContextToBootstrapTransitionsOnce(t *testing.T) {
	o := orchestrator{
		state: &state.State{
			Mode:          state.ModeIssueTriage,
			PendingTaskID: "7.4",
		},
	}

	if !o.promoteIssueTriageContextToBootstrap() {
		t.Fatalf("expected bootstrap promotion")
	}
	if o.state.Mode != state.ModeTaskBootstrap {
		t.Fatalf("mode=%q, want %q", o.state.Mode, state.ModeTaskBootstrap)
	}
	if o.promoteIssueTriageContextToBootstrap() {
		t.Fatalf("did not expect repeated promotion outside issue_triage mode")
	}
}
