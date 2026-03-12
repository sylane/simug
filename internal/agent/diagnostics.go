package agent

import "strings"

type CodexRuntimeSeverity string

const (
	CodexRuntimeSeverityNone    CodexRuntimeSeverity = ""
	CodexRuntimeSeverityWarning CodexRuntimeSeverity = "warning"
	CodexRuntimeSeverityFatal   CodexRuntimeSeverity = "fatal"
)

type CodexRuntimeDiagnostic struct {
	Severity CodexRuntimeSeverity
	Hint     string
}

// CodexRuntimeHint classifies common Codex runtime failures into actionable hints.
func CodexRuntimeHint(command, raw string) string {
	return CodexRuntimeAssessment(command, raw).Hint
}

// CodexRuntimeAssessment classifies common Codex runtime failures into actionable hints and severity.
func CodexRuntimeAssessment(command, raw string) CodexRuntimeDiagnostic {
	command = strings.TrimSpace(command)
	if command == "" {
		return CodexRuntimeDiagnostic{}
	}
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "codex" {
		return CodexRuntimeDiagnostic{}
	}

	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "permission denied") &&
		(strings.Contains(lower, ".codex/tmp/arg0") ||
			strings.Contains(lower, "failed to clean up stale arg0 temp dirs") ||
			strings.Contains(lower, "failed to renew cache ttl") ||
			strings.Contains(lower, "could not update path")):
		return CodexRuntimeDiagnostic{
			Severity: CodexRuntimeSeverityWarning,
			Hint:     "codex runtime paths appear unwritable; fix permissions under ~/.codex (especially ~/.codex/tmp/arg0) instead of overriding to a fresh CODEX_HOME/CODEX_SQLITE_HOME unless you also preserve Codex auth/config",
		}
	case strings.Contains(lower, "401 unauthorized"),
		strings.Contains(lower, "invalid_api_key"),
		strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "run `codex login`"):
		return CodexRuntimeDiagnostic{
			Severity: CodexRuntimeSeverityFatal,
			Hint:     "codex authentication appears invalid or missing; run `codex login` in this environment or configure API credentials",
		}
	case strings.Contains(lower, "command not found"):
		return CodexRuntimeDiagnostic{
			Severity: CodexRuntimeSeverityFatal,
			Hint:     "codex command not found; install Codex CLI or set SIMUG_AGENT_CMD to a valid command",
		}
	default:
		return CodexRuntimeDiagnostic{}
	}
}
