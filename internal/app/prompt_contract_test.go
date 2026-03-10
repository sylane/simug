package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simug/internal/git"
	"simug/internal/github"
	"simug/internal/state"
)

func TestBuildManagedPRPromptContainsProtocolContract(t *testing.T) {
	o := orchestrator{
		repo: git.Repo{Owner: "example", Name: "simug"},
		cfg: config{
			AllowedUsers: map[string]struct{}{"alice": {}},
			AllowedVerbs: map[string]struct{}{"do": {}, "retry": {}},
		},
	}

	pr := github.PullRequest{
		Number:      42,
		HeadRefName: "agent/20260307-120000-next-task",
	}

	prompt := o.buildManagedPRPrompt(pr, nil, false, "")
	required := []string{
		"Emit machine actions only with protocol lines starting exactly with SIMUG:",
		"Emit manager-facing human messages only with prefix SIMUG_MANAGER:",
		"Unprefixed narrative text is quarantined and ignored by the coordinator.",
		"Terminal protocol action must be exactly one of done or idle.",
		"Do NOT push, do NOT create or modify PRs directly.",
		"Use issue_update actions to declare issue linkage intent (fixes/impacts/relates); orchestrator owns all issue comments.",
		"SIMUG_MANAGER: <human-friendly manager message>",
		`SIMUG: {"action":"comment","body":"..."}`,
		`SIMUG: {"action":"reply","comment_id":123,"body":"..."}`,
		`SIMUG: {"action":"issue_update","issue_number":123,"relation":"fixes","comment":"Task implementation covers this issue because ..."}`,
		`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"optional","pr_body":"optional"}`,
		`SIMUG: {"action":"idle","reason":"..."}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in managed prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildBootstrapIntentPromptContainsProtocolContract(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}

	prompt := o.buildBootstrapIntentPrompt(nil, "", "")
	required := []string{
		"This turn is INTENT-ONLY planning; do not modify files.",
		"Do NOT edit files. Do NOT commit. Do NOT push. Do NOT create PR.",
		"Intent comment body must start with INTENT_JSON:",
		"Use SIMUG_MANAGER: for manager-facing human text; unprefixed text is quarantined.",
		"Exactly one terminal action (done or idle) is required.",
		"SIMUG_MANAGER: <human-friendly manager message>",
		`SIMUG: {"action":"comment","body":"INTENT_JSON:{\"task_ref\":\"Task 7.2a\",\"summary\":\"...\",\"branch_slug\":\"intent-handshake\",\"pr_title\":\"...\",\"pr_body\":\"...\",\"checks\":[\"GOCACHE=/tmp/go-build go test ./...\"]}"}`,
		`SIMUG: {"action":"done","summary":"intent prepared","changes":false}`,
		`SIMUG: {"action":"idle","reason":"no task available"}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in bootstrap intent prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildBootstrapIntentPromptUsesDiscoveredGuidance(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("agent guidance"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "WORKFLOW.md"), []byte("workflow guidance"), 0o644); err != nil {
		t.Fatalf("write WORKFLOW.md: %v", err)
	}

	o := orchestrator{
		repoRoot: tmp,
		cfg: config{
			MainBranch: "main",
		},
	}

	prompt := o.buildBootstrapIntentPrompt(nil, "", "")
	if !strings.Contains(prompt, "Evaluate repository guidance to select the next task scope: AGENTS.md, WORKFLOW.md.") {
		t.Fatalf("missing discovered guidance instruction in bootstrap intent prompt:\n%s", prompt)
	}
}

func TestBuildBootstrapIntentPromptIncludesPendingTaskTarget(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}

	prompt := o.buildBootstrapIntentPrompt(nil, "5.4a", "")
	if !strings.Contains(prompt, "Legacy pending task hint: prioritize Task 5.4a.") {
		t.Fatalf("missing pending task targeting instruction in bootstrap intent prompt:\n%s", prompt)
	}
}

func TestBuildBootstrapIntentPromptIncludesIssueTaskIntentContext(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}

	intent := &state.IssueTaskIntent{
		IssueNumber: 17,
		TaskTitle:   "stabilize issue-first bootstrap handoff",
		TaskBody:    "Use issue triage proposal as the bootstrap context.",
	}
	prompt := o.buildBootstrapIntentPrompt(intent, "", "")
	required := []string{
		"Issue-derived intake context is active: issue #17.",
		"Issue-derived proposed task title: stabilize issue-first bootstrap handoff",
		"Select a concrete canonical Task <id> reference for this issue in INTENT_JSON task_ref.",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in bootstrap intent prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildBootstrapExecutionPromptContainsApprovedIntent(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}

	intent := state.BootstrapIntent{
		TaskRef:    "Task 7.2a",
		Summary:    "introduce staged intent flow",
		BranchSlug: "intent-handshake",
		BranchName: "agent/20260308-120000-intent-handshake",
		PRTitle:    "feat(app): stage bootstrap through intent handshake",
		PRBody:     "Adds read-only planning intent before execution.",
		Checks:     []string{"GOCACHE=/tmp/go-build go test ./..."},
	}

	prompt := o.buildBootstrapPrompt(intent, "")
	required := []string{
		fmt.Sprintf("Create and use branch EXACTLY named: %s", intent.BranchName),
		fmt.Sprintf("Approved task reference: %s", intent.TaskRef),
		fmt.Sprintf("Approved branch slug: %s", intent.BranchSlug),
		"Scope lock: do not switch tasks;",
		"Before terminal done, emit exactly one execution report comment body prefixed with REPORT_JSON:",
		"Do NOT push. Do NOT create PR.",
		"Use issue_update actions to declare issue linkage intent (fixes/impacts/relates); orchestrator owns all issue comments.",
		`SIMUG: {"action":"comment","body":"REPORT_JSON:{\"task_ref\":\"Task 7.2d\",\"summary\":\"...\",\"branch\":\"agent/20260308-120000-task\",\"head\":\"<post-run-head-sha>\",\"checks\":[\"GOCACHE=/tmp/go-build go test ./...\"]}"}`,
		`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"...","pr_body":"..."}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in bootstrap execution prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildBootstrapExecutionPromptFallsBackWithoutSupportedPlanningStatus(t *testing.T) {
	tmp := t.TempDir()
	docsDir := filepath.Join(tmp, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "PLANNING.md"), []byte("# custom planning\nnext: bootstrap safety"), 0o644); err != nil {
		t.Fatalf("write planning file: %v", err)
	}

	o := orchestrator{
		repoRoot: tmp,
		cfg: config{
			MainBranch: "main",
		},
	}

	intent := state.BootstrapIntent{
		TaskRef:    "Task 7.3",
		Summary:    "make guidance optional",
		BranchSlug: "bootstrap-context-abstraction",
		BranchName: "agent/20260310-165033-bootstrap-context-abstraction",
		PRTitle:    "feat(bootstrap): make guidance context optional",
		PRBody:     "Makes bootstrap guidance optional.",
	}

	prompt := o.buildBootstrapPrompt(intent, "")
	if !strings.Contains(prompt, "docs/PLANNING.md does not expose supported status markers for Task 7.3") {
		t.Fatalf("missing unsupported-planning fallback in bootstrap execution prompt:\n%s", prompt)
	}
}

func TestBuildIssueTriagePromptContainsProtocolContract(t *testing.T) {
	o := orchestrator{
		repo: git.Repo{Owner: "example", Name: "simug"},
	}

	issue := github.Issue{
		Number: 17,
		Title:  "Improve issue intake",
		Body:   "Need deterministic triage.",
	}
	prompt := o.buildIssueTriagePrompt(issue, "")
	required := []string{
		"Perform issue triage for the selected authored issue.",
		"Do NOT create commits in issue triage mode.",
		"Emit exactly one issue_report action before terminal action.",
		"Terminal protocol action must be exactly one of done or idle.",
		"SIMUG_MANAGER: <human-friendly manager message>",
		`SIMUG: {"action":"issue_report","issue_number":123,"relevant":true,"analysis":"...","needs_task":true,"task_title":"...","task_body":"..."}`,
		`SIMUG: {"action":"done","summary":"issue triaged","changes":false}`,
		"Selected issue: #17",
		"Issue title: Improve issue intake",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in issue triage prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildRepairPromptContainsProtocolContract(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}

	expectedBranch := "agent/20260307-120000-next-task"
	prompt := o.buildRepairPrompt(expectedBranch, fmt.Errorf("boom"), nil)
	required := []string{
		"never push or create/update PR directly",
		"use issue_update actions for issue linkage intent; do not comment on issues directly",
		"use SIMUG_MANAGER: for manager-facing messages; unprefixed text is quarantined",
		fmt.Sprintf("- branch must be %q (or %q if terminal action is idle)", expectedBranch, o.cfg.MainBranch),
		"SIMUG_MANAGER: <human-friendly manager message>",
		`SIMUG: {"action":"comment","body":"..."}`,
		`SIMUG: {"action":"reply","comment_id":123,"body":"..."}`,
		`SIMUG: {"action":"issue_update","issue_number":123,"relation":"impacts","comment":"This work affects this issue because ..."}`,
		`SIMUG: {"action":"done","summary":"...","changes":true}`,
		`SIMUG: {"action":"idle","reason":"..."}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in repair prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildRepairPromptIncludesExecutionScopeLockConstraints(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}
	scopeLock := &executionScopeLock{
		TaskRef:          "Task 7.2b",
		TaskID:           "7.2b",
		BranchName:       "agent/20260308-120000-execution-scope-lock",
		PlanningEnforced: true,
		PlanningBaseline: planningStatusSnapshot{Path: "docs/PLANNING.md"},
	}

	prompt := o.buildRepairPrompt("agent/20260308-120000-execution-scope-lock", fmt.Errorf("scope violation"), scopeLock)
	required := []string{
		`execution scope lock: stay on "agent/20260308-120000-execution-scope-lock" and implement only Task 7.2b`,
		"in docs/PLANNING.md, do not change status markers for tasks other than Task 7.2b",
		"at most one [IN_PROGRESS] task is allowed in docs/PLANNING.md, and if present it must be Task 7.2b",
		"when terminal action is done, emit one REPORT_JSON comment with task_ref, summary, branch, and head from this run",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in scope-locked repair prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildRepairPromptFallsBackWithoutPlanningStatusLock(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}
	scopeLock := &executionScopeLock{
		TaskRef:          "Task 7.3",
		TaskID:           "7.3",
		BranchName:       "agent/20260310-165033-bootstrap-context-abstraction",
		PlanningBaseline: planningStatusSnapshot{},
	}

	prompt := o.buildRepairPrompt("agent/20260310-165033-bootstrap-context-abstraction", fmt.Errorf("scope violation"), scopeLock)
	if !strings.Contains(prompt, "no supported planning status file was discovered for Task 7.3") {
		t.Fatalf("missing planning fallback in repair prompt:\n%s", prompt)
	}
}
