package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"simug/internal/agent"
	"simug/internal/runtimepaths"
)

type archivedAttemptPaths struct {
	MetadataPath string
	PromptPath   string
	OutputPath   string
}

type archivedAttemptMetadata struct {
	ArchivedAt              string   `json:"archived_at"`
	RunID                   string   `json:"run_id"`
	TickSeq                 int64    `json:"tick_seq"`
	Attempt                 int      `json:"attempt"`
	MaxAttempts             int      `json:"max_attempts"`
	ExpectedBranch          string   `json:"expected_branch"`
	BeforeHead              string   `json:"before_head"`
	AfterHead               string   `json:"after_head"`
	ProtocolTurnID          string   `json:"protocol_turn_id,omitempty"`
	ProtocolSessionID       string   `json:"protocol_session_id,omitempty"`
	TerminalAction          string   `json:"terminal_action"`
	TerminalHasChange       bool     `json:"terminal_has_changes"`
	AgentError              string   `json:"agent_error,omitempty"`
	ValidationError         string   `json:"validation_error,omitempty"`
	ProtocolActionCount     int      `json:"protocol_action_count,omitempty"`
	ProtocolActionTypes     []string `json:"protocol_action_types,omitempty"`
	ProtocolActionsExcerpt  []string `json:"protocol_actions_excerpt,omitempty"`
	ProtocolTerminalCount   int      `json:"protocol_terminal_count,omitempty"`
	ProtocolTerminalTypes   []string `json:"protocol_terminal_types,omitempty"`
	ProtocolManagerMessages int      `json:"protocol_manager_messages,omitempty"`
	ProtocolQuarantined     int      `json:"protocol_quarantined_lines,omitempty"`
	ProtocolRawLineCount    int      `json:"protocol_raw_line_count,omitempty"`
	ProtocolParserHint      string   `json:"protocol_parser_hint,omitempty"`
	RolloutRefs             []string `json:"rollout_refs,omitempty"`
	SessionRefs             []string `json:"session_refs,omitempty"`
}

type attemptArchiveDiagnostics struct {
	ActionCount     int
	ActionTypes     []string
	ActionsExcerpt  []string
	TerminalCount   int
	TerminalTypes   []string
	ManagerMessages int
	Quarantined     int
	RawLineCount    int
	ParserHint      string
	RolloutRefs     []string
	SessionRefs     []string
}

func (o *orchestrator) archiveAgentAttempt(
	attempt int,
	maxAttempts int,
	expectedBranch string,
	beforeHead string,
	afterHead string,
	turn agent.CoordinatorTurn,
	prompt string,
	rawOutput string,
	terminalAction string,
	terminalHasChanges bool,
	agentErrText string,
	validationErrText string,
	diagnostics attemptArchiveDiagnostics,
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
		ArchivedAt:              time.Now().UTC().Format(time.RFC3339Nano),
		RunID:                   o.runID,
		TickSeq:                 o.tickSeq,
		Attempt:                 attempt,
		MaxAttempts:             maxAttempts,
		ExpectedBranch:          expectedBranch,
		BeforeHead:              beforeHead,
		AfterHead:               afterHead,
		ProtocolTurnID:          turn.TurnID,
		ProtocolSessionID:       turn.SessionID,
		TerminalAction:          terminalAction,
		TerminalHasChange:       terminalHasChanges,
		AgentError:              agentErrText,
		ValidationError:         validationErrText,
		ProtocolActionCount:     diagnostics.ActionCount,
		ProtocolActionTypes:     diagnostics.ActionTypes,
		ProtocolActionsExcerpt:  diagnostics.ActionsExcerpt,
		ProtocolTerminalCount:   diagnostics.TerminalCount,
		ProtocolTerminalTypes:   diagnostics.TerminalTypes,
		ProtocolManagerMessages: diagnostics.ManagerMessages,
		ProtocolQuarantined:     diagnostics.Quarantined,
		ProtocolRawLineCount:    diagnostics.RawLineCount,
		ProtocolParserHint:      diagnostics.ParserHint,
		RolloutRefs:             diagnostics.RolloutRefs,
		SessionRefs:             diagnostics.SessionRefs,
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
