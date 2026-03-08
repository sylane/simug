package agent

import "testing"

func TestCodexRuntimeHintPermissionDenied(t *testing.T) {
	raw := `WARNING: failed to clean up stale arg0 temp dirs: Permission denied (os error 13) at path "/home/me/.codex/tmp/arg0/x"`
	hint := CodexRuntimeHint("codex exec", raw)
	if hint == "" {
		t.Fatalf("expected permission hint")
	}
}

func TestCodexRuntimeHintAuthFailure(t *testing.T) {
	hint := CodexRuntimeHint("codex exec", "request failed: 401 Unauthorized")
	if hint == "" {
		t.Fatalf("expected auth hint")
	}
}

func TestCodexRuntimeHintIgnoresNonCodexCommand(t *testing.T) {
	hint := CodexRuntimeHint("printf", "command not found")
	if hint != "" {
		t.Fatalf("expected empty hint, got %q", hint)
	}
}
