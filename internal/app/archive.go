package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"simug/internal/runtimepaths"
)

type archivedAttemptPaths struct {
	MetadataPath string
	PromptPath   string
	OutputPath   string
}

type archivedAttemptMetadata struct {
	ArchivedAt        string `json:"archived_at"`
	RunID             string `json:"run_id"`
	TickSeq           int64  `json:"tick_seq"`
	Attempt           int    `json:"attempt"`
	MaxAttempts       int    `json:"max_attempts"`
	ExpectedBranch    string `json:"expected_branch"`
	BeforeHead        string `json:"before_head"`
	AfterHead         string `json:"after_head"`
	TerminalAction    string `json:"terminal_action"`
	TerminalHasChange bool   `json:"terminal_has_changes"`
	AgentError        string `json:"agent_error,omitempty"`
	ValidationError   string `json:"validation_error,omitempty"`
}

func (o *orchestrator) archiveAgentAttempt(
	attempt int,
	maxAttempts int,
	expectedBranch string,
	beforeHead string,
	afterHead string,
	prompt string,
	rawOutput string,
	terminalAction string,
	terminalHasChanges bool,
	agentErrText string,
	validationErrText string,
) (archivedAttemptPaths, error) {
	if o == nil {
		return archivedAttemptPaths{}, fmt.Errorf("nil orchestrator")
	}

	dataDir, err := runtimepaths.EnsureDataDir(o.repoRoot)
	if err != nil {
		return archivedAttemptPaths{}, fmt.Errorf("resolve runtime dir for archive: %w", err)
	}

	archiveDir := filepath.Join(
		dataDir,
		"archive",
		"agent",
		o.runID,
		fmt.Sprintf("tick-%06d", o.tickSeq),
		fmt.Sprintf("attempt-%02d", attempt),
	)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return archivedAttemptPaths{}, fmt.Errorf("create archive dir %s: %w", archiveDir, err)
	}

	paths := archivedAttemptPaths{
		MetadataPath: filepath.Join(archiveDir, "metadata.json"),
		PromptPath:   filepath.Join(archiveDir, "prompt.txt"),
		OutputPath:   filepath.Join(archiveDir, "raw_output.txt"),
	}

	meta := archivedAttemptMetadata{
		ArchivedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		RunID:             o.runID,
		TickSeq:           o.tickSeq,
		Attempt:           attempt,
		MaxAttempts:       maxAttempts,
		ExpectedBranch:    expectedBranch,
		BeforeHead:        beforeHead,
		AfterHead:         afterHead,
		TerminalAction:    terminalAction,
		TerminalHasChange: terminalHasChanges,
		AgentError:        agentErrText,
		ValidationError:   validationErrText,
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return archivedAttemptPaths{}, fmt.Errorf("encode archive metadata: %w", err)
	}

	if err := os.WriteFile(paths.PromptPath, []byte(prompt), 0o644); err != nil {
		return archivedAttemptPaths{}, fmt.Errorf("write prompt archive: %w", err)
	}
	if err := os.WriteFile(paths.OutputPath, []byte(rawOutput), 0o644); err != nil {
		return archivedAttemptPaths{}, fmt.Errorf("write output archive: %w", err)
	}
	if err := os.WriteFile(paths.MetadataPath, append(metaJSON, '\n'), 0o644); err != nil {
		return archivedAttemptPaths{}, fmt.Errorf("write metadata archive: %w", err)
	}

	return paths, nil
}
