package app

import (
	"errors"
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestBuildAttemptArchiveDiagnosticsFromParsedResult(t *testing.T) {
	raw := strings.Join([]string{
		`SIMUG: {"action":"comment","body":"note"}`,
		`SIMUG: {"action":"done","summary":"ok","changes":false}`,
		"/home/sebastien/.codex/sessions/abc123/rollout-2026-03-08.jsonl",
	}, "\n")
	result := agent.Result{
		Actions: []agent.Action{
			{Type: agent.ActionComment, Body: "note"},
			{Type: agent.ActionDone, Summary: "ok", Changes: false},
		},
		Terminal:         agent.Action{Type: agent.ActionDone, Summary: "ok", Changes: false},
		ManagerMessages:  []string{"manager note"},
		QuarantinedLines: []string{"free text"},
	}

	diagnostics := buildAttemptArchiveDiagnostics(raw, &result, nil, errors.New("validation failure"))
	if diagnostics.ActionCount != 2 {
		t.Fatalf("action_count=%d, want 2", diagnostics.ActionCount)
	}
	if diagnostics.TerminalCount != 1 || len(diagnostics.TerminalTypes) != 1 || diagnostics.TerminalTypes[0] != "done" {
		t.Fatalf("unexpected terminal diagnostics: %#v", diagnostics)
	}
	if diagnostics.ManagerMessages != 1 || diagnostics.Quarantined != 1 {
		t.Fatalf("unexpected manager/quarantined counts: %#v", diagnostics)
	}
	if diagnostics.ParserHint == "" || !strings.Contains(diagnostics.ParserHint, "validation failure") {
		t.Fatalf("parser_hint=%q, want validation failure context", diagnostics.ParserHint)
	}
	if len(diagnostics.RolloutRefs) == 0 || len(diagnostics.SessionRefs) == 0 {
		t.Fatalf("expected rollout/session refs in diagnostics: %#v", diagnostics)
	}
}

func TestBuildAttemptArchiveDiagnosticsFromRawErrorOutput(t *testing.T) {
	raw := strings.Join([]string{
		`SIMUG: {"action":"done","summary":"ok","changes":false}`,
		`SIMUG: {"action":"idle","reason":"no task available"}`,
		"/home/sebastien/.codex/sessions/abc123/rollout-2026-03-08.jsonl",
	}, "\n")

	diagnostics := buildAttemptArchiveDiagnostics(raw, nil, errors.New("agent protocol parse failed"), nil)
	if diagnostics.ActionCount != 2 {
		t.Fatalf("action_count=%d, want 2 raw protocol lines", diagnostics.ActionCount)
	}
	if diagnostics.TerminalCount != 2 {
		t.Fatalf("terminal_count=%d, want 2", diagnostics.TerminalCount)
	}
	if diagnostics.ParserHint == "" || !strings.Contains(diagnostics.ParserHint, "agent protocol parse failed") {
		t.Fatalf("unexpected parser_hint: %q", diagnostics.ParserHint)
	}
	if len(diagnostics.ActionsExcerpt) == 0 {
		t.Fatalf("expected raw protocol excerpt in diagnostics")
	}
}
