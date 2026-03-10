package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simug/internal/state"
)

func TestParseTaskIDFromRef(t *testing.T) {
	taskID, err := parseTaskIDFromRef("Task 7.2b")
	if err != nil {
		t.Fatalf("parseTaskIDFromRef returned error: %v", err)
	}
	if taskID != "7.2b" {
		t.Fatalf("task_id=%q, want 7.2b", taskID)
	}
}

func TestValidateExecutionScopeLockAllowsLockedTaskStatusChange(t *testing.T) {
	tmp := t.TempDir()
	writePlanningFixture(t, tmp, `# simug Project Planning
- [x] **Task 7.2a: done**
- [ ] **[IN_PROGRESS] Task 7.2b: lock scope**
- [ ] **Task 7.2c: parser hardening**
`)

	o := orchestrator{repoRoot: tmp}
	lock, err := o.newExecutionScopeLock(state.BootstrapIntent{
		TaskRef:    "Task 7.2b",
		BranchName: "agent/20260308-120000-execution-scope-lock",
	})
	if err != nil {
		t.Fatalf("newExecutionScopeLock returned error: %v", err)
	}

	writePlanningFixture(t, tmp, `# simug Project Planning
- [x] **Task 7.2a: done**
- [x] **Task 7.2b: lock scope**
- [ ] **Task 7.2c: parser hardening**
`)

	if err := o.validateExecutionScopeLock(lock); err != nil {
		t.Fatalf("validateExecutionScopeLock returned error: %v", err)
	}
}

func TestValidateExecutionScopeLockRejectsUnrelatedStatusMutation(t *testing.T) {
	tmp := t.TempDir()
	writePlanningFixture(t, tmp, `# simug Project Planning
- [x] **Task 7.2a: done**
- [ ] **[IN_PROGRESS] Task 7.2b: lock scope**
- [ ] **Task 7.2c: parser hardening**
`)

	o := orchestrator{repoRoot: tmp}
	lock, err := o.newExecutionScopeLock(state.BootstrapIntent{
		TaskRef:    "Task 7.2b",
		BranchName: "agent/20260308-120000-execution-scope-lock",
	})
	if err != nil {
		t.Fatalf("newExecutionScopeLock returned error: %v", err)
	}

	writePlanningFixture(t, tmp, `# simug Project Planning
- [x] **Task 7.2a: done**
- [x] **Task 7.2b: lock scope**
- [x] **Task 7.2c: parser hardening**
`)

	err = o.validateExecutionScopeLock(lock)
	if err == nil {
		t.Fatalf("expected execution scope lock violation")
	}
	if !strings.Contains(err.Error(), "task 7.2c status changed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExecutionScopeLockRejectsForeignInProgress(t *testing.T) {
	tmp := t.TempDir()
	writePlanningFixture(t, tmp, `# simug Project Planning
- [x] **Task 7.2a: done**
- [ ] **[IN_PROGRESS] Task 7.2b: lock scope**
- [ ] **Task 7.2c: parser hardening**
`)

	o := orchestrator{repoRoot: tmp}
	lock, err := o.newExecutionScopeLock(state.BootstrapIntent{
		TaskRef:    "Task 7.2b",
		BranchName: "agent/20260308-120000-execution-scope-lock",
	})
	if err != nil {
		t.Fatalf("newExecutionScopeLock returned error: %v", err)
	}

	writePlanningFixture(t, tmp, `# simug Project Planning
- [x] **Task 7.2a: done**
- [ ] **Task 7.2b: lock scope**
- [ ] **[IN_PROGRESS] Task 7.2c: parser hardening**
`)

	err = o.validateExecutionScopeLock(lock)
	if err == nil {
		t.Fatalf("expected execution scope lock violation")
	}
	if !strings.Contains(err.Error(), "drifted to 7.2c") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExecutionScopeLockAllowsMissingPlanningFile(t *testing.T) {
	tmp := t.TempDir()

	o := orchestrator{repoRoot: tmp}
	lock, err := o.newExecutionScopeLock(state.BootstrapIntent{
		TaskRef:    "Task 7.3",
		BranchName: "agent/20260310-165033-bootstrap-context-abstraction",
	})
	if err != nil {
		t.Fatalf("newExecutionScopeLock returned error: %v", err)
	}
	if lock.PlanningEnforced {
		t.Fatalf("PlanningEnforced=%v, want false", lock.PlanningEnforced)
	}

	if err := o.validateExecutionScopeLock(lock); err != nil {
		t.Fatalf("validateExecutionScopeLock returned error: %v", err)
	}
}

func TestValidateExecutionScopeLockAllowsUnsupportedPlanningFormat(t *testing.T) {
	tmp := t.TempDir()
	docsDir := filepath.Join(tmp, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "PLANNING.md"), []byte("# custom planning\nnext task: bootstrap abstraction"), 0o644); err != nil {
		t.Fatalf("write planning fixture: %v", err)
	}

	o := orchestrator{repoRoot: tmp}
	lock, err := o.newExecutionScopeLock(state.BootstrapIntent{
		TaskRef:    "Task 7.3",
		BranchName: "agent/20260310-165033-bootstrap-context-abstraction",
	})
	if err != nil {
		t.Fatalf("newExecutionScopeLock returned error: %v", err)
	}
	if lock.PlanningEnforced {
		t.Fatalf("PlanningEnforced=%v, want false", lock.PlanningEnforced)
	}

	if err := o.validateExecutionScopeLock(lock); err != nil {
		t.Fatalf("validateExecutionScopeLock returned error: %v", err)
	}
}

func TestCapturePlanningStatusFindsRootPlanningFile(t *testing.T) {
	tmp := t.TempDir()
	body := `# Planning
- [ ] **[IN_PROGRESS] Task 7.3: bootstrap context abstraction**
`
	if err := os.WriteFile(filepath.Join(tmp, "PLANNING.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write planning fixture: %v", err)
	}

	snapshot, err := capturePlanningStatus(tmp)
	if err != nil {
		t.Fatalf("capturePlanningStatus returned error: %v", err)
	}
	if snapshot.Path != "PLANNING.md" {
		t.Fatalf("path=%q, want PLANNING.md", snapshot.Path)
	}
	if !snapshot.SupportedFormat {
		t.Fatalf("SupportedFormat=%v, want true", snapshot.SupportedFormat)
	}
	if !snapshot.supportsTask("7.3") {
		t.Fatalf("supportsTask(7.3)=false, want true")
	}
}

func writePlanningFixture(t *testing.T, repoRoot string, body string) {
	t.Helper()
	docsDir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "PLANNING.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write planning fixture: %v", err)
	}
}
