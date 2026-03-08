package app

import (
	"fmt"
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

	prompt := o.buildBootstrapIntentPrompt("", "")
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

func TestBuildBootstrapIntentPromptIncludesPendingTaskTarget(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}

	prompt := o.buildBootstrapIntentPrompt("5.4a", "")
	if !strings.Contains(prompt, "Prioritize pending issue-derived task context: Task 5.4a.") {
		t.Fatalf("missing pending task targeting instruction in bootstrap intent prompt:\n%s", prompt)
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
		"Do NOT push. Do NOT create PR.",
		"Use issue_update actions to declare issue linkage intent (fixes/impacts/relates); orchestrator owns all issue comments.",
		`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"...","pr_body":"..."}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in bootstrap execution prompt:\n%s", needle, prompt)
		}
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
		TaskRef:    "Task 7.2b",
		TaskID:     "7.2b",
		BranchName: "agent/20260308-120000-execution-scope-lock",
	}

	prompt := o.buildRepairPrompt("agent/20260308-120000-execution-scope-lock", fmt.Errorf("scope violation"), scopeLock)
	required := []string{
		`execution scope lock: stay on "agent/20260308-120000-execution-scope-lock" and implement only Task 7.2b`,
		"in docs/PLANNING.md, do not change status markers for tasks other than Task 7.2b",
		"at most one [IN_PROGRESS] task is allowed, and if present it must be Task 7.2b",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in scope-locked repair prompt:\n%s", needle, prompt)
		}
	}
}
