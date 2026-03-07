package app

import (
	"fmt"
	"strings"
	"testing"

	"simug/internal/git"
	"simug/internal/github"
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
		"Terminal protocol action must be exactly one of done or idle.",
		"Do NOT push, do NOT create or modify PRs directly.",
		`SIMUG: {"action":"comment","body":"..."}`,
		`SIMUG: {"action":"reply","comment_id":123,"body":"..."}`,
		`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"optional","pr_body":"optional"}`,
		`SIMUG: {"action":"idle","reason":"..."}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in managed prompt:\n%s", needle, prompt)
		}
	}
}

func TestBuildBootstrapPromptContainsProtocolContract(t *testing.T) {
	o := orchestrator{
		cfg: config{
			MainBranch: "main",
		},
	}

	expectedBranch := "agent/20260307-120000-next-task"
	prompt := o.buildBootstrapPrompt(expectedBranch, "")
	required := []string{
		fmt.Sprintf("Create and use branch EXACTLY named: %s", expectedBranch),
		"Do NOT push. Do NOT create PR.",
		"Exactly one terminal action (done or idle) is required.",
		`SIMUG: {"action":"comment","body":"..."}`,
		`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"...","pr_body":"..."}`,
		`SIMUG: {"action":"idle","reason":"no task available"}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in bootstrap prompt:\n%s", needle, prompt)
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
	prompt := o.buildRepairPrompt(expectedBranch, fmt.Errorf("boom"))
	required := []string{
		"never push or create/update PR directly",
		fmt.Sprintf("- branch must be %q (or %q if terminal action is idle)", expectedBranch, o.cfg.MainBranch),
		`SIMUG: {"action":"comment","body":"..."}`,
		`SIMUG: {"action":"reply","comment_id":123,"body":"..."}`,
		`SIMUG: {"action":"done","summary":"...","changes":true}`,
		`SIMUG: {"action":"idle","reason":"..."}`,
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in repair prompt:\n%s", needle, prompt)
		}
	}
}
