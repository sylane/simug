package app

import (
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestAttemptTranscriptTextIncludesPromptAndAgentLines(t *testing.T) {
	transcript := newAttemptTranscript()
	transcript.RecordMilestone("codex attempt 1/3 start")
	transcript.RecordPrompt("line one\nline two\n")
	transcript.RecordAgentLine(agent.StreamLine{Kind: agent.StreamKindManager, Line: "SIMUG_MANAGER: hello"})
	transcript.RecordAgentLine(agent.StreamLine{Kind: agent.StreamKindProtocol, Line: `SIMUG: {"action":"done","changes":false}`})

	text := transcript.Text()
	required := []string{
		" simug[milestone] codex attempt 1/3 start",
		" simug[prompt] line one",
		" simug[prompt] line two",
		" codex[manager] SIMUG_MANAGER: hello",
		` codex[protocol] SIMUG: {"action":"done","changes":false}`,
	}
	for _, needle := range required {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %q in transcript:\n%s", needle, text)
		}
	}
}

func TestSplitTranscriptLinesDropsTrailingBlankOnly(t *testing.T) {
	lines := splitTranscriptLines("one\n\ntwo\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d, want 3", len(lines))
	}
	if lines[1] != "" {
		t.Fatalf("expected inner blank line, got %q", lines[1])
	}
}
