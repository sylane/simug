package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestIncrementTaskIDSuffix(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{id: "5.4", want: "5.4a"},
		{id: "5.4a", want: "5.4b"},
		{id: "5.4z", want: "5.4aa"},
		{id: "5.4az", want: "5.4ba"},
	}

	for _, tc := range tests {
		got, err := incrementTaskIDSuffix(tc.id)
		if err != nil {
			t.Fatalf("incrementTaskIDSuffix(%q) returned error: %v", tc.id, err)
		}
		if got != tc.want {
			t.Fatalf("incrementTaskIDSuffix(%q)=%q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestEnsureIssueDerivedPlanningTaskInsertsAfterLastDone(t *testing.T) {
	repo := t.TempDir()
	planningPath := filepath.Join(repo, "docs", "PLANNING.md")
	if err := os.MkdirAll(filepath.Dir(planningPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	initial := strings.Join([]string{
		"# Plan",
		"",
		"- [x] **Task 5.4: Existing done task**",
		"  - Scope: done scope",
		"  - Done when: done",
		"",
		"- [ ] **Task 5.6: Existing todo task**",
		"  - Scope: todo scope",
		"  - Done when: todo",
		"",
	}, "\n")
	if err := os.WriteFile(planningPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial planning: %v", err)
	}

	report := agent.Action{
		IssueNumber: 7,
		TaskTitle:   "Add deterministic planner insertion",
		TaskBody:    "Insert task from validated issue report.",
	}
	id, inserted, err := ensureIssueDerivedPlanningTask(repo, report)
	if err != nil {
		t.Fatalf("ensureIssueDerivedPlanningTask returned error: %v", err)
	}
	if !inserted {
		t.Fatalf("expected insertion to occur")
	}
	if id != "5.4a" {
		t.Fatalf("inserted id=%q, want 5.4a", id)
	}

	updated, err := os.ReadFile(planningPath)
	if err != nil {
		t.Fatalf("read updated planning: %v", err)
	}
	content := string(updated)
	if !strings.Contains(content, "- [ ] **Task 5.4a: Add deterministic planner insertion**") {
		t.Fatalf("missing inserted task block:\n%s", content)
	}
	if !strings.Contains(content, issueTaskSourceLine(7)) {
		t.Fatalf("missing source marker line:\n%s", content)
	}

	insertPos := strings.Index(content, "Task 5.4a")
	nextPos := strings.Index(content, "Task 5.6")
	if insertPos == -1 || nextPos == -1 || insertPos > nextPos {
		t.Fatalf("inserted task is not before next TODO task:\n%s", content)
	}
}

func TestEnsureIssueDerivedPlanningTaskSkipsDuplicateForSameIssueAndTitle(t *testing.T) {
	repo := t.TempDir()
	planningPath := filepath.Join(repo, "docs", "PLANNING.md")
	if err := os.MkdirAll(filepath.Dir(planningPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	initial := strings.Join([]string{
		"# Plan",
		"",
		"- [x] **Task 5.4: Existing done task**",
		"  - Scope: done scope",
		"  - Done when: done",
		"",
		"- [ ] **Task 5.4a: Add deterministic planner insertion**",
		"  - Scope: Insert task from validated issue report.",
		"  - Done when: issue-derived requirements from issue #7 are implemented and validated.",
		issueTaskSourceLine(7),
		"",
	}, "\n")
	if err := os.WriteFile(planningPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial planning: %v", err)
	}

	report := agent.Action{
		IssueNumber: 7,
		TaskTitle:   "Add deterministic planner insertion",
		TaskBody:    "Insert task from validated issue report.",
	}
	id, inserted, err := ensureIssueDerivedPlanningTask(repo, report)
	if err != nil {
		t.Fatalf("ensureIssueDerivedPlanningTask returned error: %v", err)
	}
	if inserted {
		t.Fatalf("expected duplicate insertion to be skipped")
	}
	if id != "5.4a" {
		t.Fatalf("returned id=%q, want existing 5.4a", id)
	}

	updated, err := os.ReadFile(planningPath)
	if err != nil {
		t.Fatalf("read updated planning: %v", err)
	}
	if strings.Count(string(updated), "Task 5.4a: Add deterministic planner insertion") != 1 {
		t.Fatalf("expected exactly one existing issue-derived task block:\n%s", string(updated))
	}
}

func TestEnsureIssueDerivedPlanningTaskHandlesExistingSuffixCollision(t *testing.T) {
	repo := t.TempDir()
	planningPath := filepath.Join(repo, "docs", "PLANNING.md")
	if err := os.MkdirAll(filepath.Dir(planningPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	initial := strings.Join([]string{
		"# Plan",
		"",
		"- [x] **Task 5.4: Existing done task**",
		"  - Scope: done scope",
		"  - Done when: done",
		"",
		"- [ ] **Task 5.4a: Existing inserted task**",
		"  - Scope: existing scope",
		"  - Done when: existing",
		"",
		"- [ ] **Task 5.6: Existing todo task**",
		"  - Scope: todo scope",
		"  - Done when: todo",
		"",
	}, "\n")
	if err := os.WriteFile(planningPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial planning: %v", err)
	}

	report := agent.Action{
		IssueNumber: 8,
		TaskTitle:   "Second issue-derived task",
		TaskBody:    "Add follow-up deterministic insertion.",
	}
	id, inserted, err := ensureIssueDerivedPlanningTask(repo, report)
	if err != nil {
		t.Fatalf("ensureIssueDerivedPlanningTask returned error: %v", err)
	}
	if !inserted {
		t.Fatalf("expected insertion to occur")
	}
	if id != "5.4b" {
		t.Fatalf("inserted id=%q, want 5.4b", id)
	}
}
