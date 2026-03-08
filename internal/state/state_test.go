package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingReturnsEmptyState(t *testing.T) {
	tmp := t.TempDir()
	st, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if st == nil {
		t.Fatalf("Load returned nil state")
	}
	if st.ActivePR != 0 || st.Repo != "" {
		t.Fatalf("unexpected default state: %#v", st)
	}
	if st.Mode != ModeIssueTriage {
		t.Fatalf("default mode=%q, want %q", st.Mode, ModeIssueTriage)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	want := &State{
		Repo:          "owner/repo",
		ActivePR:      42,
		ActiveBranch:  "agent/20260307-120000-next-task",
		Mode:          ModeManagedPR,
		ActiveIssue:   0,
		PendingTaskID: "",
		BootstrapIntent: &BootstrapIntent{
			TaskRef:    "Task 7.2a",
			Summary:    "stage bootstrap through validated intent",
			BranchSlug: "intent-handshake",
			BranchName: "agent/20260307-120000-intent-handshake",
			PRTitle:    "task(7.2a): add bootstrap intent handshake",
			PRBody:     "Introduce a staged intent flow before execution.",
			Checks:     []string{"GOCACHE=/tmp/go-build go test ./..."},
			ApprovedAt: time.Now().UTC().Truncate(time.Second),
		},
		IssueLinks: []IssueLink{
			{
				PRNumber:       42,
				IssueNumber:    13,
				Relation:       "fixes",
				CommentBody:    "Implemented by this PR.",
				Provenance:     "run=abc tick=1",
				IdempotencyKey: "k1",
				RecordedAt:     time.Now().UTC().Truncate(time.Second),
			},
		},
		InFlightAttempt: &Attempt{
			RunID:          "run-x",
			TickSeq:        3,
			AttemptIndex:   1,
			MaxAttempts:    3,
			ExpectedBranch: "agent/20260307-120000-next-task",
			Mode:           ModeManagedPR,
			Phase:          AttemptPhaseStarted,
			PromptHash:     "abc123",
			BeforeHead:     "deadbeef",
			StartedAt:      time.Now().UTC().Truncate(time.Second),
			UpdatedAt:      time.Now().UTC().Truncate(time.Second),
		},
		LastRecovery: &Recovery{
			Action:     RecoveryReplay,
			Reason:     "journal phase started",
			AttemptRun: "run-x",
			AttemptSeq: 3,
			RecordedAt: time.Now().UTC().Truncate(time.Second),
		},
		LastCommentID:       1,
		LastIssueCommentID:  2,
		LastReviewCommentID: 3,
		LastReviewID:        4,
		CursorUncertain:     true,
		UpdatedAt:           time.Now().UTC().Truncate(time.Second),
	}

	if err := want.Save(tmp); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	got, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got.Repo != want.Repo || got.ActivePR != want.ActivePR || got.ActiveBranch != want.ActiveBranch || got.Mode != want.Mode {
		t.Fatalf("round trip mismatch: got=%#v want=%#v", got, want)
	}
	if got.LastCommentID != want.LastCommentID || got.LastIssueCommentID != want.LastIssueCommentID || got.LastReviewCommentID != want.LastReviewCommentID || got.LastReviewID != want.LastReviewID {
		t.Fatalf("cursor mismatch: got=%#v want=%#v", got, want)
	}
	if got.CursorUncertain != want.CursorUncertain {
		t.Fatalf("cursor uncertain mismatch: got=%v want=%v", got.CursorUncertain, want.CursorUncertain)
	}
	if len(got.IssueLinks) != 1 || got.IssueLinks[0].IssueNumber != 13 || got.IssueLinks[0].IdempotencyKey != "k1" {
		t.Fatalf("issue links mismatch: got=%#v", got.IssueLinks)
	}
	if got.InFlightAttempt == nil || got.InFlightAttempt.PromptHash != "abc123" || got.InFlightAttempt.AttemptIndex != 1 {
		t.Fatalf("in_flight_attempt mismatch: got=%#v", got.InFlightAttempt)
	}
	if got.LastRecovery == nil || got.LastRecovery.Action != RecoveryReplay {
		t.Fatalf("last_recovery mismatch: got=%#v", got.LastRecovery)
	}
}

func TestNormalizeInfersManagedPRModeAndClearsIssueFields(t *testing.T) {
	st := &State{
		ActivePR:      7,
		Mode:          ModeIssueTriage,
		ActiveIssue:   99,
		PendingTaskID: "task-5.9",
		BootstrapIntent: &BootstrapIntent{
			TaskRef:    "Task 7.2a",
			Summary:    "x",
			BranchSlug: "intent",
			BranchName: "agent/20260307-120000-intent",
			PRTitle:    "title",
			PRBody:     "body",
		},
	}

	st.Normalize()
	if st.Mode != ModeManagedPR {
		t.Fatalf("mode=%q, want %q", st.Mode, ModeManagedPR)
	}
	if st.ActiveIssue != 0 || st.PendingTaskID != "" || st.BootstrapIntent != nil {
		t.Fatalf("expected managed mode to clear issue metadata/intent, got issue=%d task=%q intent=%#v", st.ActiveIssue, st.PendingTaskID, st.BootstrapIntent)
	}
}

func TestNormalizeClearsBootstrapIntentInIssueTriage(t *testing.T) {
	st := &State{
		Mode: ModeIssueTriage,
		BootstrapIntent: &BootstrapIntent{
			TaskRef:    "Task 7.2a",
			Summary:    "x",
			BranchSlug: "intent",
			BranchName: "agent/20260307-120000-intent",
			PRTitle:    "title",
			PRBody:     "body",
		},
	}

	st.Normalize()
	if st.BootstrapIntent != nil {
		t.Fatalf("expected issue triage mode to clear bootstrap intent, got %#v", st.BootstrapIntent)
	}
}

func TestNormalizeDropsInvalidBootstrapIntent(t *testing.T) {
	st := &State{
		Mode: ModeTaskBootstrap,
		BootstrapIntent: &BootstrapIntent{
			TaskRef:    "Task 7.2a",
			Summary:    "",
			BranchSlug: "intent",
			BranchName: "agent/20260307-120000-intent",
			PRTitle:    "title",
			PRBody:     "body",
		},
	}

	st.Normalize()
	if st.BootstrapIntent != nil {
		t.Fatalf("expected invalid bootstrap intent to be dropped, got %#v", st.BootstrapIntent)
	}
}

func TestLoadMalformedJSONFails(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".simug")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write malformed state: %v", err)
	}

	_, err := Load(tmp)
	if err == nil {
		t.Fatalf("expected error for malformed json")
	}
}

func TestNormalizeFiltersInvalidIssueLinks(t *testing.T) {
	st := &State{
		IssueLinks: []IssueLink{
			{IssueNumber: 0, IdempotencyKey: "bad"},
			{IssueNumber: 5, IdempotencyKey: ""},
			{IssueNumber: 6, IdempotencyKey: "ok"},
		},
	}

	st.Normalize()
	if len(st.IssueLinks) != 1 || st.IssueLinks[0].IssueNumber != 6 {
		t.Fatalf("unexpected filtered issue links: %#v", st.IssueLinks)
	}
}

func TestNormalizeDropsInvalidInFlightAttempt(t *testing.T) {
	st := &State{
		InFlightAttempt: &Attempt{
			AttemptIndex:   0,
			MaxAttempts:    2,
			ExpectedBranch: "agent/x",
			PromptHash:     "h",
			Phase:          AttemptPhaseStarted,
		},
	}

	st.Normalize()
	if st.InFlightAttempt != nil {
		t.Fatalf("expected invalid in_flight_attempt to be removed, got %#v", st.InFlightAttempt)
	}
}

func TestNormalizeDropsInvalidLastRecovery(t *testing.T) {
	st := &State{
		LastRecovery: &Recovery{
			Action: "bad-action",
		},
	}
	st.Normalize()
	if st.LastRecovery != nil {
		t.Fatalf("expected invalid last_recovery to be removed, got %#v", st.LastRecovery)
	}
}
