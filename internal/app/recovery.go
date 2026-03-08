package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"simug/internal/git"
	"simug/internal/state"
)

func (o *orchestrator) recoverInterruptedAttempt(ctx context.Context) error {
	if o == nil || o.state == nil || o.state.InFlightAttempt == nil {
		return nil
	}

	attempt := *o.state.InFlightAttempt
	action, reason := o.decideRecoveryAction(ctx, attempt)
	now := time.Now().UTC()
	o.state.LastRecovery = &state.Recovery{
		Action:     action,
		Reason:     reason,
		AttemptRun: attempt.RunID,
		AttemptSeq: attempt.TickSeq,
		RecordedAt: now,
	}
	o.state.UpdatedAt = now

	o.logEvent("recovery_transition", "evaluated restart recovery action from persisted in-flight attempt", map[string]any{
		"action":          string(action),
		"reason":          reason,
		"attempt_run":     attempt.RunID,
		"attempt_tick":    attempt.TickSeq,
		"attempt_index":   attempt.AttemptIndex,
		"expected_branch": attempt.ExpectedBranch,
		"phase":           string(attempt.Phase),
	})

	switch action {
	case state.RecoveryAbort:
		// Preserve in-flight attempt details for diagnosis.
	case state.RecoveryReplay:
		o.state.CursorUncertain = true
		o.state.InFlightAttempt = nil
	case state.RecoveryRepair:
		o.state.CursorUncertain = true
		o.state.InFlightAttempt = nil
	case state.RecoveryResume:
		o.state.InFlightAttempt = nil
	default:
		return fmt.Errorf("unsupported recovery action %q", action)
	}

	if err := o.state.Save(o.repoRoot); err != nil {
		return fmt.Errorf("persist recovery action %q: %w", action, err)
	}
	if action == state.RecoveryAbort {
		return fmt.Errorf("restart recovery abort: %s", reason)
	}
	return nil
}

func (o *orchestrator) decideRecoveryAction(ctx context.Context, attempt state.Attempt) (state.RecoveryAction, string) {
	branch, err := git.CurrentBranch(ctx, o.repoRoot)
	if err != nil {
		return state.RecoveryAbort, fmt.Sprintf("cannot read current branch during recovery: %v", err)
	}
	clean, status, err := git.IsClean(ctx, o.repoRoot)
	if err != nil {
		return state.RecoveryAbort, fmt.Sprintf("cannot read working tree status during recovery: %v", err)
	}
	if !clean {
		return state.RecoveryAbort, fmt.Sprintf("working tree is dirty during recovery: %s", strings.TrimSpace(status))
	}

	expected := strings.TrimSpace(attempt.ExpectedBranch)
	if expected == "" {
		return state.RecoveryRepair, "missing expected branch in in-flight attempt journal"
	}
	if branch != expected {
		return state.RecoveryRepair, fmt.Sprintf("current branch %q differs from expected %q", branch, expected)
	}

	switch attempt.Phase {
	case state.AttemptPhaseValidated:
		if strings.TrimSpace(attempt.AgentError) == "" && strings.TrimSpace(attempt.ValidationErr) == "" {
			return state.RecoveryResume, "validated attempt without recorded errors"
		}
		return state.RecoveryReplay, "validated attempt has recorded error context"
	case state.AttemptPhaseFailed:
		return state.RecoveryReplay, "failed attempt should replay with bounded repair"
	case state.AttemptPhaseStarted, state.AttemptPhaseAgentExited:
		return state.RecoveryReplay, "attempt interrupted before successful validation"
	case state.AttemptPhaseRecovered:
		return state.RecoveryResume, "attempt already marked recovered"
	default:
		return state.RecoveryRepair, fmt.Sprintf("unknown attempt phase %q", attempt.Phase)
	}
}
