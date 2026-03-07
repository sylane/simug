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
