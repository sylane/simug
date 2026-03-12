package app

import (
	"fmt"
	"io"
	"strings"

	"simug/internal/agent"
)

func (o *orchestrator) emitVerbosePrompt(attempt, maxAttempts int, prompt, sessionID string) {
	if o == nil || !o.verboseConsole {
		return
	}

	w := o.consoleWriter()
	fmt.Fprintf(w, "simug[milestone] codex attempt %d/%d start", attempt, maxAttempts)
	if strings.TrimSpace(sessionID) != "" {
		fmt.Fprintf(w, " resume_session=%s", strings.TrimSpace(sessionID))
	}
	lineCount := len(splitTranscriptLines(prompt))
	if lineCount > 0 {
		fmt.Fprintf(w, " prompt_lines=%d", lineCount)
	}
	fmt.Fprintln(w)
}

func (o *orchestrator) emitVerboseAgentLine(line agent.StreamLine) {
	if o == nil || !o.verboseConsole {
		return
	}

	label := "codex[raw]"
	body := line.Line
	switch line.Kind {
	case agent.StreamKindManager:
		label = "codex[manager]"
		if trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line.Line), "SIMUG_MANAGER:")); trimmed != "" {
			body = trimmed
		}
	case agent.StreamKindProtocol:
		label = "codex[protocol]"
	}

	fmt.Fprintf(o.consoleWriter(), "%s %s\n", label, body)
}

func (o *orchestrator) emitVerboseMilestone(format string, args ...any) {
	if o == nil || !o.verboseConsole {
		return
	}
	fmt.Fprintf(o.consoleWriter(), "simug[milestone] %s\n", fmt.Sprintf(format, args...))
}

func (o *orchestrator) consoleWriter() io.Writer {
	if o != nil && o.console != nil {
		return o.console
	}
	return io.Discard
}
