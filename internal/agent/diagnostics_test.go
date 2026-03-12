package agent

import "testing"

func TestCodexRuntimeHintPermissionDenied(t *testing.T) {
	raw := `WARNING: failed to clean up stale arg0 temp dirs: Permission denied (os error 13) at path "/home/me/.codex/tmp/arg0/x"`
	assessment := CodexRuntimeAssessment("codex exec", raw)
	if assessment.Hint == "" {
		t.Fatalf("expected permission hint")
	}
	if assessment.Severity != CodexRuntimeSeverityWarning {
		t.Fatalf("severity=%q, want warning", assessment.Severity)
	}
}

func TestCodexRuntimeHintAuthFailure(t *testing.T) {
	assessment := CodexRuntimeAssessment("codex exec", "request failed: 401 Unauthorized")
	if assessment.Hint == "" {
		t.Fatalf("expected auth hint")
	}
	if assessment.Severity != CodexRuntimeSeverityFatal {
		t.Fatalf("severity=%q, want fatal", assessment.Severity)
	}
}

func TestCodexRuntimeHintIgnoresNonCodexCommand(t *testing.T) {
	assessment := CodexRuntimeAssessment("printf", "command not found")
	if assessment.Hint != "" {
		t.Fatalf("expected empty hint, got %q", assessment.Hint)
	}
}
