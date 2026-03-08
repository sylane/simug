package app

import "testing"

func TestBuildSessionResumeCommandForCodexExec(t *testing.T) {
	command, err := buildSessionResumeCommand("codex exec", "123e4567-e89b-12d3-a456-426614174000")
	if err != nil {
		t.Fatalf("buildSessionResumeCommand returned error: %v", err)
	}
	want := "codex exec resume 123e4567-e89b-12d3-a456-426614174000 -"
	if command != want {
		t.Fatalf("command=%q, want %q", command, want)
	}
}

func TestBuildSessionResumeCommandRejectsUnsupportedBaseCommand(t *testing.T) {
	_, err := buildSessionResumeCommand(`printf 'SIMUG: {"action":"idle","reason":"noop"}\n'`, "123e4567-e89b-12d3-a456-426614174000")
	if err == nil {
		t.Fatalf("expected unsupported command error")
	}
}

func TestExtractCodexSessionIDFromRawOutput(t *testing.T) {
	raw := "/home/sebastien/.codex/sessions/123e4567-e89b-12d3-a456-426614174000/rollout-2026-03-08.jsonl\n"
	sessionID := extractCodexSessionIDFromRawOutput(raw)
	if sessionID != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("session_id=%q, want 123e4567-e89b-12d3-a456-426614174000", sessionID)
	}
}
