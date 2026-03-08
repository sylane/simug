package app

import (
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestValidateIssueTriageResultAcceptsValidReport(t *testing.T) {
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type:        agent.ActionIssueReport,
				IssueNumber: 42,
				Relevant:    true,
				Analysis:    "Needs a follow-up task.",
				NeedsTask:   true,
				TaskTitle:   "Add deterministic planner insertion",
				TaskBody:    "Insert issue-derived task after last done task.",
			},
			{
				Type:    agent.ActionDone,
				Summary: "triaged",
				Changes: false,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "triaged",
			Changes: false,
		},
	}

	report, err := validateIssueTriageResult(result, 42)
	if err != nil {
		t.Fatalf("validateIssueTriageResult returned error: %v", err)
	}
	if report.Type != agent.ActionIssueReport {
		t.Fatalf("expected issue_report action, got %q", report.Type)
	}
}

func TestValidateIssueTriageResultRejectsMissingReport(t *testing.T) {
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type:    agent.ActionDone,
				Summary: "triaged",
				Changes: false,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "triaged",
			Changes: false,
		},
	}

	_, err := validateIssueTriageResult(result, 42)
	if err == nil {
		t.Fatalf("expected error for missing issue_report action")
	}
	if !strings.Contains(err.Error(), "exactly one issue_report") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateIssueTriageResultRejectsNeedsTaskWithoutMetadata(t *testing.T) {
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type:        agent.ActionIssueReport,
				IssueNumber: 42,
				Relevant:    true,
				Analysis:    "Need new task.",
				NeedsTask:   true,
			},
			{
				Type:    agent.ActionDone,
				Summary: "triaged",
				Changes: false,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "triaged",
			Changes: false,
		},
	}

	_, err := validateIssueTriageResult(result, 42)
	if err == nil {
		t.Fatalf("expected error for missing task metadata")
	}
	if !strings.Contains(err.Error(), "task_title and task_body") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildIssueTriageCommentBodyIncludesMarkerAndTaskProposal(t *testing.T) {
	report := agent.Action{
		Type:        agent.ActionIssueReport,
		IssueNumber: 9,
		Relevant:    true,
		Analysis:    "A new planning task is needed.",
		NeedsTask:   true,
		TaskTitle:   "Add replay marker checks",
		TaskBody:    "Persist triage marker and skip duplicates.",
	}

	body := buildIssueTriageCommentBody(report)
	required := []string{
		issueTriageMarker(report),
		"### simug issue triage analysis",
		"- Issue: #9",
		"- Relevant: true",
		"- Needs task: true",
		"Analysis:",
		"A new planning task is needed.",
		"Proposed task title:",
		"Add replay marker checks",
		"Proposed task body:",
		"Persist triage marker and skip duplicates.",
	}
	for _, needle := range required {
		if !strings.Contains(body, needle) {
			t.Fatalf("missing %q in issue triage comment body:\n%s", needle, body)
		}
	}
}

func TestValidateIssueUpdateActionsAcceptsValidUpdates(t *testing.T) {
	actions := []agent.Action{
		{
			Type:        agent.ActionIssueUpdate,
			IssueNumber: 11,
			Relation:    agent.IssueRelationFixes,
			CommentBody: "Implemented in current task.",
		},
		{
			Type:    agent.ActionComment,
			Body:    "regular PR note",
			Summary: "",
		},
	}

	if err := validateIssueUpdateActions(actions); err != nil {
		t.Fatalf("validateIssueUpdateActions returned error: %v", err)
	}
}

func TestValidateIssueUpdateActionsRejectsInvalidRelation(t *testing.T) {
	actions := []agent.Action{
		{
			Type:        agent.ActionIssueUpdate,
			IssueNumber: 11,
			Relation:    "bad",
			CommentBody: "x",
		},
	}

	err := validateIssueUpdateActions(actions)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "invalid relation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIssueUpdateIdempotencyKeyStable(t *testing.T) {
	action := agent.Action{
		Type:        agent.ActionIssueUpdate,
		IssueNumber: 11,
		Relation:    agent.IssueRelationFixes,
		CommentBody: "Implemented with test coverage.",
	}
	key1 := issueUpdateIdempotencyKey(42, action)
	key2 := issueUpdateIdempotencyKey(42, action)
	if key1 == "" || key2 == "" {
		t.Fatalf("expected non-empty keys")
	}
	if key1 != key2 {
		t.Fatalf("expected stable key, got %q vs %q", key1, key2)
	}
}
