package app

import (
	"errors"
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestBuildAttemptArchiveDiagnosticsFromParsedResult(t *testing.T) {
	turn := agent.CoordinatorTurn{TurnID: "turn-123"}
	raw := strings.Join([]string{
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"stale-turn","payload":{"action":"done","summary":"stale","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"comment","body":"note"}}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"done","summary":"ok","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
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

	diagnostics := buildAttemptArchiveDiagnostics(raw, turn, &result, nil, errors.New("validation failure"))
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
	if diagnostics.ActiveTurnID != "turn-123" {
		t.Fatalf("active_turn_id=%q, want %q", diagnostics.ActiveTurnID, "turn-123")
	}
	if diagnostics.ActiveLineCount != 4 {
		t.Fatalf("active_line_count=%d, want 4", diagnostics.ActiveLineCount)
	}
	if diagnostics.IgnoredLineCount != 3 {
		t.Fatalf("ignored_line_count=%d, want 3", diagnostics.IgnoredLineCount)
	}
	if len(diagnostics.ActiveLines) != 4 || !strings.Contains(diagnostics.ActiveLines[1], `"body":"note"`) {
		t.Fatalf("unexpected active lines: %#v", diagnostics.ActiveLines)
	}
	if len(diagnostics.IgnoredLines) != 3 || !strings.Contains(diagnostics.IgnoredLines[0], `"turn_id":"stale-turn"`) {
		t.Fatalf("unexpected ignored lines: %#v", diagnostics.IgnoredLines)
	}
}

func TestBuildAttemptArchiveDiagnosticsFromRawErrorOutput(t *testing.T) {
	turn := agent.CoordinatorTurn{TurnID: "turn-123"}
	raw := strings.Join([]string{
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"stale-turn","payload":{"action":"done","summary":"stale","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"done","summary":"ok","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"idle","reason":"no task available"}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"done","summary":"echo","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
		"/home/sebastien/.codex/sessions/abc123/rollout-2026-03-08.jsonl",
	}, "\n")

	diagnostics := buildAttemptArchiveDiagnostics(raw, turn, nil, errors.New("agent protocol parse failed"), nil)
	if diagnostics.ActionCount != 2 {
		t.Fatalf("action_count=%d, want 2 active action lines", diagnostics.ActionCount)
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
	if diagnostics.ActiveTurnID != "turn-123" || diagnostics.ActiveLineCount != 4 {
		t.Fatalf("unexpected active turn evidence: %#v", diagnostics)
	}
	if diagnostics.IgnoredLineCount != 6 {
		t.Fatalf("ignored_line_count=%d, want 6", diagnostics.IgnoredLineCount)
	}
	if len(diagnostics.ActiveLines) != 4 || !strings.Contains(diagnostics.ActiveLines[2], `"action":"idle"`) {
		t.Fatalf("unexpected active lines: %#v", diagnostics.ActiveLines)
	}
	if len(diagnostics.IgnoredLines) != 6 || !containsString(diagnostics.IgnoredLines, `"summary":"echo"`) {
		t.Fatalf("unexpected ignored lines: %#v", diagnostics.IgnoredLines)
	}
}

func containsString(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}
