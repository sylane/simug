package app

import (
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestParseBootstrapIntentProposalAcceptsValidPayload(t *testing.T) {
	comment := `INTENT_JSON:{"task_ref":"Task 7.2a","summary":"intent stage","branch_slug":"intent-stage","pr_title":"feat: intent stage","pr_body":"Implements intent stage","checks":["GOCACHE=/tmp/go-build go test ./..."]}`

	proposal, err := parseBootstrapIntentProposal(comment)
	if err != nil {
		t.Fatalf("parseBootstrapIntentProposal returned error: %v", err)
	}
	if proposal.TaskRef != "Task 7.2a" {
		t.Fatalf("task_ref=%q, want Task 7.2a", proposal.TaskRef)
	}
	if proposal.BranchSlug != "intent-stage" {
		t.Fatalf("branch_slug=%q, want intent-stage", proposal.BranchSlug)
	}
}

func TestParseBootstrapIntentProposalRejectsMissingPrefix(t *testing.T) {
	_, err := parseBootstrapIntentProposal(`{"task_ref":"Task 7.2a"}`)
	if err == nil {
		t.Fatalf("expected missing prefix error")
	}
	if !strings.Contains(err.Error(), "INTENT_JSON:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBootstrapIntentResultAcceptsDone(t *testing.T) {
	o := orchestrator{
		cfg: config{
			BranchPrefix: "agent/",
		},
	}
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type: agent.ActionComment,
				Body: `INTENT_JSON:{"task_ref":"Task 7.2a","summary":"intent stage","branch_slug":"intent-stage","pr_title":"feat: intent stage","pr_body":"Implements intent stage","checks":[" GOCACHE=/tmp/go-build go test ./... "]}`,
			},
			{
				Type:    agent.ActionDone,
				Summary: "intent prepared",
				Changes: false,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "intent prepared",
			Changes: false,
		},
	}

	intent, err := o.validateBootstrapIntentResult(result, "7.2a")
	if err != nil {
		t.Fatalf("validateBootstrapIntentResult returned error: %v", err)
	}
	if intent.TaskRef != "Task 7.2a" {
		t.Fatalf("task_ref=%q, want Task 7.2a", intent.TaskRef)
	}
	if intent.BranchSlug != "intent-stage" {
		t.Fatalf("branch_slug=%q, want intent-stage", intent.BranchSlug)
	}
	if len(intent.Checks) != 1 || intent.Checks[0] != "GOCACHE=/tmp/go-build go test ./..." {
		t.Fatalf("checks=%v, want trimmed single check", intent.Checks)
	}
	if !strings.HasPrefix(intent.BranchName, "agent/") || !strings.HasSuffix(intent.BranchName, "-intent-stage") {
		t.Fatalf("branch_name=%q, want agent/<timestamp>-intent-stage", intent.BranchName)
	}
}

func TestValidateBootstrapIntentResultRejectsPendingTaskMismatch(t *testing.T) {
	o := orchestrator{
		cfg: config{
			BranchPrefix: "agent/",
		},
	}
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type: agent.ActionComment,
				Body: `INTENT_JSON:{"task_ref":"Task 7.2b","summary":"intent stage","branch_slug":"intent-stage","pr_title":"feat: intent stage","pr_body":"Implements intent stage"}`,
			},
			{
				Type:    agent.ActionDone,
				Summary: "intent prepared",
				Changes: false,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "intent prepared",
			Changes: false,
		},
	}

	_, err := o.validateBootstrapIntentResult(result, "7.2a")
	if err == nil {
		t.Fatalf("expected pending task mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match required pending task") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBootstrapIntentResultRejectsNonCanonicalTaskRef(t *testing.T) {
	o := orchestrator{
		cfg: config{
			BranchPrefix: "agent/",
		},
	}
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type: agent.ActionComment,
				Body: `INTENT_JSON:{"task_ref":"Improve task handling","summary":"intent stage","branch_slug":"intent-stage","pr_title":"feat: intent stage","pr_body":"Implements intent stage"}`,
			},
			{
				Type:    agent.ActionDone,
				Summary: "intent prepared",
				Changes: false,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "intent prepared",
			Changes: false,
		},
	}

	_, err := o.validateBootstrapIntentResult(result, "")
	if err == nil {
		t.Fatalf("expected task_ref validation error")
	}
	if !strings.Contains(err.Error(), "canonical 'Task <id>'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSanitizeBranchSlugNormalizesAndTruncates(t *testing.T) {
	raw := "  Intent Stage / with$Symbols and VERY LONG TEXT THAT SHOULD BE TRUNCATED  "
	slug := sanitizeBranchSlug(raw)
	if slug == "" {
		t.Fatalf("expected non-empty slug")
	}
	if strings.ContainsAny(slug, " $/") {
		t.Fatalf("slug=%q contains invalid characters", slug)
	}
	if len(slug) > 41 {
		t.Fatalf("slug length=%d, want <=41", len(slug))
	}
}
