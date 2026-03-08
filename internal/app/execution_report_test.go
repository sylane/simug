package app

import (
	"strings"
	"testing"

	"simug/internal/agent"
	"simug/internal/state"
)

func TestValidateExecutionReportAcceptsValidDoneReport(t *testing.T) {
	intent := state.BootstrapIntent{TaskRef: "Task 7.2d"}
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type: agent.ActionComment,
				Body: `REPORT_JSON:{"task_ref":"Task 7.2d","summary":"Implemented report gate","branch":"agent/20260308-120000-task","head":"def456","checks":["GOCACHE=/tmp/go-build go test ./..."]}`,
			},
			{
				Type: agent.ActionComment,
				Body: "human-visible update",
			},
			{
				Type:    agent.ActionDone,
				Summary: "done",
				Changes: true,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "done",
			Changes: true,
		},
	}

	report, filtered, err := validateExecutionReport(result, intent, "agent/20260308-120000-task", "abc123", "def456")
	if err != nil {
		t.Fatalf("validateExecutionReport returned error: %v", err)
	}
	if report.TaskRef != "Task 7.2d" || report.Head != "def456" {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered actions=%d, want 2", len(filtered))
	}
	if filtered[0].Type != agent.ActionComment || filtered[1].Type != agent.ActionDone {
		t.Fatalf("unexpected filtered actions: %#v", filtered)
	}
}

func TestValidateExecutionReportRejectsMissingReport(t *testing.T) {
	intent := state.BootstrapIntent{TaskRef: "Task 7.2d"}
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type:    agent.ActionDone,
				Summary: "done",
				Changes: true,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "done",
			Changes: true,
		},
	}

	_, _, err := validateExecutionReport(result, intent, "agent/20260308-120000-task", "abc123", "def456")
	if err == nil {
		t.Fatalf("expected execution report validation error")
	}
	if !strings.Contains(err.Error(), "requires exactly one REPORT_JSON comment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExecutionReportRejectsBranchMismatch(t *testing.T) {
	intent := state.BootstrapIntent{TaskRef: "Task 7.2d"}
	result := agent.Result{
		Actions: []agent.Action{
			{
				Type: agent.ActionComment,
				Body: `REPORT_JSON:{"task_ref":"Task 7.2d","summary":"Implemented report gate","branch":"agent/20260308-120000-other","head":"def456"}`,
			},
			{
				Type:    agent.ActionDone,
				Summary: "done",
				Changes: true,
			},
		},
		Terminal: agent.Action{
			Type:    agent.ActionDone,
			Summary: "done",
			Changes: true,
		},
	}

	_, _, err := validateExecutionReport(result, intent, "agent/20260308-120000-task", "abc123", "def456")
	if err == nil {
		t.Fatalf("expected execution report validation error")
	}
	if !strings.Contains(err.Error(), "does not match expected branch") {
		t.Fatalf("unexpected error: %v", err)
	}
}
