package app

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"simug/internal/agent"
)

type attemptTranscript struct {
	mu      sync.Mutex
	entries []attemptTranscriptEntry
}

type attemptTranscriptEntry struct {
	Timestamp string
	Actor     string
	Kind      string
	Line      string
}

func newAttemptTranscript() *attemptTranscript {
	return &attemptTranscript{}
}

func (t *attemptTranscript) RecordMilestone(line string) {
	t.record("simug", "milestone", line)
}

func (t *attemptTranscript) RecordPrompt(prompt string) {
	for _, line := range splitTranscriptLines(prompt) {
		t.record("simug", "prompt", line)
	}
}

func (t *attemptTranscript) RecordAgentLine(line agent.StreamLine) {
	kind := "diagnostic"
	switch line.Kind {
	case agent.StreamKindManager:
		kind = "manager"
	case agent.StreamKindProtocol:
		kind = "protocol"
	}
	t.record("codex", kind, line.Line)
}

func (t *attemptTranscript) Text() string {
	if t == nil {
		return ""
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	var b strings.Builder
	for _, entry := range t.entries {
		b.WriteString(formatTranscriptEntry(entry))
		b.WriteByte('\n')
	}
	return b.String()
}

func (t *attemptTranscript) record(actor, kind, line string) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries = append(t.entries, attemptTranscriptEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Actor:     strings.TrimSpace(actor),
		Kind:      strings.TrimSpace(kind),
		Line:      line,
	})
}

func formatTranscriptEntry(entry attemptTranscriptEntry) string {
	return fmt.Sprintf("%s %s[%s] %s", entry.Timestamp, entry.Actor, entry.Kind, entry.Line)
}

func splitTranscriptLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}
