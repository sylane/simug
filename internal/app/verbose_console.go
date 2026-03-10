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
	fmt.Fprintf(w, "simug->codex [attempt %d/%d", attempt, maxAttempts)
	if strings.TrimSpace(sessionID) != "" {
		fmt.Fprintf(w, ", resume session %s", strings.TrimSpace(sessionID))
	}
	fmt.Fprintln(w, "]")
	fmt.Fprintln(w, prompt)
	if !strings.HasSuffix(prompt, "\n") {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "simug->codex [end prompt]")
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

func (o *orchestrator) consoleWriter() io.Writer {
	if o != nil && o.console != nil {
		return o.console
	}
	return io.Discard
}
