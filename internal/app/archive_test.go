package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simug/internal/agent"
)

func TestArchiveAgentAttemptWritesTranscript(t *testing.T) {
	tmp := t.TempDir()
	o := &orchestrator{
		repoRoot: tmp,
		runID:    "run-a",
		tickSeq:  7,
	}

	paths, err := o.archiveAgentAttempt(
		1,
		3,
		"agent/20260312-173000-task",
		"abc123",
		"def456",
		agent.CoordinatorTurn{TurnID: "turn-1", SessionID: "session-1"},
		"prompt line\n",
		"raw output\n",
		"2026-03-12T17:38:09Z simug[prompt] prompt line\n",
		"done",
		true,
		"",
		"",
		attemptArchiveDiagnostics{},
	)
	if err != nil {
		t.Fatalf("archiveAgentAttempt returned error: %v", err)
	}

	transcriptData, err := os.ReadFile(paths.TranscriptPath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if !strings.Contains(string(transcriptData), "simug[prompt] prompt line") {
		t.Fatalf("unexpected transcript content: %s", string(transcriptData))
	}

	metaData, err := os.ReadFile(paths.MetadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta archivedAttemptMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.TranscriptPath != paths.TranscriptPath {
		t.Fatalf("metadata transcript path = %q, want %q", meta.TranscriptPath, paths.TranscriptPath)
	}
	if filepath.Base(paths.TranscriptPath) != "transcript.log" {
		t.Fatalf("unexpected transcript filename: %s", paths.TranscriptPath)
	}
}

func TestArchiveAgentAttemptUsesPlaceholdersForEmptyArtifacts(t *testing.T) {
	tmp := t.TempDir()
	o := &orchestrator{
		repoRoot: tmp,
		runID:    "run-a",
		tickSeq:  7,
	}

	paths, err := o.archiveAgentAttempt(
		1,
		3,
		"agent/20260312-173000-task",
		"abc123",
		"abc123",
		agent.CoordinatorTurn{TurnID: "turn-1"},
		"prompt line\n",
		"",
		"",
		"",
		false,
		"protocol failure",
		"",
		attemptArchiveDiagnostics{},
	)
	if err != nil {
		t.Fatalf("archiveAgentAttempt returned error: %v", err)
	}

	outputData, err := os.ReadFile(paths.OutputPath)
	if err != nil {
		t.Fatalf("read raw output: %v", err)
	}
	if strings.TrimSpace(string(outputData)) != "simug archived empty raw agent output" {
		t.Fatalf("unexpected raw output placeholder: %q", string(outputData))
	}

	transcriptData, err := os.ReadFile(paths.TranscriptPath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if strings.TrimSpace(string(transcriptData)) != "simug archived empty classified transcript" {
		t.Fatalf("unexpected transcript placeholder: %q", string(transcriptData))
	}
}
