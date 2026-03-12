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
	if !strings.Contains(output, "simug[milestone] codex attempt 2/3 start resume_session=session-123 prompt_lines=2") {
		t.Fatalf("unexpected prompt header: %s", output)
	}
	if strings.Contains(output, "line one") || strings.Contains(output, "line two") {
		t.Fatalf("verbose prompt should not dump prompt body: %s", output)
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

func TestEmitVerboseMilestone(t *testing.T) {
	var buf bytes.Buffer
	o := orchestrator{
		verboseConsole: true,
		console:        &buf,
	}

	o.emitVerboseMilestone("attempt %d archived transcript=%s", 1, "/tmp/transcript.log")

	if got := buf.String(); !strings.Contains(got, "simug[milestone] attempt 1 archived transcript=/tmp/transcript.log\n") {
		t.Fatalf("unexpected milestone output: %s", got)
	}
}
