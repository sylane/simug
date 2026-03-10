package app

import (
	"bytes"
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestEmitVerbosePrompt(t *testing.T) {
	var buf bytes.Buffer
	o := orchestrator{
		verboseConsole: true,
		console:        &buf,
	}

	o.emitVerbosePrompt(2, 3, "line one\nline two", "session-123")

	output := buf.String()
	if !strings.Contains(output, "simug->codex [attempt 2/3, resume session session-123]") {
		t.Fatalf("unexpected prompt header: %s", output)
	}
	if !strings.Contains(output, "line one\nline two\n") {
		t.Fatalf("missing prompt body: %s", output)
	}
	if !strings.Contains(output, "simug->codex [end prompt]") {
		t.Fatalf("missing prompt footer: %s", output)
	}
}

func TestEmitVerboseAgentLineRoutesKinds(t *testing.T) {
	var buf bytes.Buffer
	o := orchestrator{
		verboseConsole: true,
		console:        &buf,
	}

	o.emitVerboseAgentLine(agent.StreamLine{Kind: agent.StreamKindManager, Line: "SIMUG_MANAGER: hello"})
	o.emitVerboseAgentLine(agent.StreamLine{Kind: agent.StreamKindProtocol, Line: `SIMUG: {"action":"done","changes":false}`})
	o.emitVerboseAgentLine(agent.StreamLine{Kind: agent.StreamKindDiagnostic, Line: "thinking..."})

	output := buf.String()
	if !strings.Contains(output, "codex[manager] hello\n") {
		t.Fatalf("missing manager output: %s", output)
	}
	if !strings.Contains(output, "codex[protocol] SIMUG: {\"action\":\"done\",\"changes\":false}\n") {
		t.Fatalf("missing protocol output: %s", output)
	}
	if !strings.Contains(output, "codex[raw] thinking...\n") {
		t.Fatalf("missing diagnostic output: %s", output)
	}
}
