package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"simug/internal/state"
)

func (o *orchestrator) recordInFlightAttemptStart(attemptIndex, maxAttempts int, expectedBranch, beforeHead, prompt string) error {
	if o == nil || o.state == nil {
		return fmt.Errorf("record attempt start: nil orchestrator state")
	}
	now := time.Now().UTC()
	o.state.InFlightAttempt = &state.Attempt{
		RunID:          o.runID,
		TickSeq:        o.tickSeq,
		AttemptIndex:   attemptIndex,
		MaxAttempts:    maxAttempts,
		ExpectedBranch: expectedBranch,
		Mode:           o.state.Mode,
		Phase:          state.AttemptPhaseStarted,
		PromptHash:     stableHash(prompt),
		BeforeHead:     beforeHead,
		StartedAt:      now,
		UpdatedAt:      now,
	}
	o.state.UpdatedAt = now
	o.logEvent("attempt_journal", "persisting in-flight attempt start", map[string]any{
		"attempt":         attemptIndex,
		"max_attempts":    maxAttempts,
		"expected_branch": expectedBranch,
		"mode":            string(o.state.Mode),
		"prompt_hash":     o.state.InFlightAttempt.PromptHash,
	})
	if err := o.state.Save(o.repoRoot); err != nil {
		return fmt.Errorf("persist in-flight attempt start: %w", err)
	}
	return nil
}

func (o *orchestrator) recordInFlightAttemptResult(attemptIndex int, afterHead, terminalAction, agentErr, validationErr string) error {
	if o == nil || o.state == nil {
		return fmt.Errorf("record attempt result: nil orchestrator state")
	}
	if o.state.InFlightAttempt == nil || o.state.InFlightAttempt.AttemptIndex != attemptIndex {
		return fmt.Errorf("record attempt result: no matching in-flight attempt for index %d", attemptIndex)
	}
	now := time.Now().UTC()
	entry := o.state.InFlightAttempt
	entry.AfterHead = afterHead
	entry.TerminalAction = terminalAction
	entry.AgentError = agentErr
	entry.ValidationErr = validationErr
	entry.UpdatedAt = now
	if agentErr != "" || validationErr != "" {
		entry.Phase = state.AttemptPhaseFailed
	} else {
		entry.Phase = state.AttemptPhaseValidated
	}
	o.state.UpdatedAt = now
	o.logEvent("attempt_journal", "persisting in-flight attempt result", map[string]any{
		"attempt":          attemptIndex,
		"phase":            string(entry.Phase),
		"terminal_action":  terminalAction,
		"after_head":       afterHead,
		"agent_error":      errorTextf(agentErr),
		"validation_error": errorTextf(validationErr),
	})
	if err := o.state.Save(o.repoRoot); err != nil {
		return fmt.Errorf("persist in-flight attempt result: %w", err)
	}
	return nil
}

func (o *orchestrator) clearInFlightAttemptJournal() error {
	if o == nil || o.state == nil || o.state.InFlightAttempt == nil {
		return nil
	}
	o.logEvent("attempt_journal", "clearing in-flight attempt journal after successful validation", map[string]any{
		"attempt": o.state.InFlightAttempt.AttemptIndex,
	})
	o.state.InFlightAttempt = nil
	o.state.UpdatedAt = time.Now().UTC()
	if err := o.state.Save(o.repoRoot); err != nil {
		return fmt.Errorf("clear in-flight attempt journal: %w", err)
	}
	return nil
}

func stableHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func errorTextf(s string) any {
	if s == "" {
		return nil
	}
	return s
}
