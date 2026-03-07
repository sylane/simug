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
		Repo:                "owner/repo",
		ActivePR:            42,
		ActiveBranch:        "agent/20260307-120000-next-task",
		Mode:                ModeManagedPR,
		ActiveIssue:         0,
		PendingTaskID:       "",
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
}

func TestNormalizeInfersManagedPRModeAndClearsIssueFields(t *testing.T) {
	st := &State{
		ActivePR:      7,
		Mode:          ModeIssueTriage,
		ActiveIssue:   99,
		PendingTaskID: "task-5.9",
	}

	st.Normalize()
	if st.Mode != ModeManagedPR {
		t.Fatalf("mode=%q, want %q", st.Mode, ModeManagedPR)
	}
	if st.ActiveIssue != 0 || st.PendingTaskID != "" {
		t.Fatalf("expected managed mode to clear issue metadata, got issue=%d task=%q", st.ActiveIssue, st.PendingTaskID)
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
