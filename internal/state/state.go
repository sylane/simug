package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"simug/internal/runtimepaths"
)

type Mode string

const (
	ModeManagedPR     Mode = "managed_pr"
	ModeIssueTriage   Mode = "issue_triage"
	ModeTaskBootstrap Mode = "task_bootstrap"
)

type State struct {
	Repo                string           `json:"repo"`
	ActivePR            int              `json:"active_pr"`
	ActiveBranch        string           `json:"active_branch"`
	Mode                Mode             `json:"mode"`
	ActiveIssue         int              `json:"active_issue"`
	PendingTaskID       string           `json:"pending_task_id"`
	BootstrapIntent     *BootstrapIntent `json:"bootstrap_intent,omitempty"`
	IssueLinks          []IssueLink      `json:"issue_links,omitempty"`
	InFlightAttempt     *Attempt         `json:"in_flight_attempt,omitempty"`
	LastRecovery        *Recovery        `json:"last_recovery,omitempty"`
	LastCommentID       int64            `json:"last_comment_id"` // Legacy cursor, retained for migration safety.
	LastIssueCommentID  int64            `json:"last_issue_comment_id"`
	LastReviewCommentID int64            `json:"last_review_comment_id"`
	LastReviewID        int64            `json:"last_review_id"`
	CursorUncertain     bool             `json:"cursor_uncertain"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type AttemptPhase string

const (
	AttemptPhaseStarted     AttemptPhase = "started"
	AttemptPhaseAgentExited AttemptPhase = "agent_exited"
	AttemptPhaseValidated   AttemptPhase = "validated"
	AttemptPhaseFailed      AttemptPhase = "failed"
	AttemptPhaseRecovered   AttemptPhase = "recovered"
)

type Attempt struct {
	RunID          string       `json:"run_id"`
	TickSeq        int64        `json:"tick_seq"`
	AttemptIndex   int          `json:"attempt_index"`
	MaxAttempts    int          `json:"max_attempts"`
	ExpectedBranch string       `json:"expected_branch"`
	Mode           Mode         `json:"mode"`
	Phase          AttemptPhase `json:"phase"`
	PromptHash     string       `json:"prompt_hash"`
	BeforeHead     string       `json:"before_head"`
	AfterHead      string       `json:"after_head,omitempty"`
	TerminalAction string       `json:"terminal_action,omitempty"`
	AgentError     string       `json:"agent_error,omitempty"`
	ValidationErr  string       `json:"validation_error,omitempty"`
	StartedAt      time.Time    `json:"started_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

type RecoveryAction string

const (
	RecoveryResume RecoveryAction = "resume"
	RecoveryReplay RecoveryAction = "replay"
	RecoveryRepair RecoveryAction = "repair"
	RecoveryAbort  RecoveryAction = "abort"
)

type Recovery struct {
	Action     RecoveryAction `json:"action"`
	Reason     string         `json:"reason"`
	AttemptRun string         `json:"attempt_run"`
	AttemptSeq int64          `json:"attempt_tick_seq"`
	RecordedAt time.Time      `json:"recorded_at"`
}

type BootstrapIntent struct {
	TaskRef    string    `json:"task_ref"`
	Summary    string    `json:"summary"`
	BranchSlug string    `json:"branch_slug"`
	BranchName string    `json:"branch_name"`
	PRTitle    string    `json:"pr_title"`
	PRBody     string    `json:"pr_body"`
	Checks     []string  `json:"checks,omitempty"`
	ApprovedAt time.Time `json:"approved_at"`
}

type IssueLink struct {
	PRNumber       int       `json:"pr_number"`
	IssueNumber    int       `json:"issue_number"`
	Relation       string    `json:"relation"`
	CommentBody    string    `json:"comment_body"`
	Provenance     string    `json:"provenance"`
	IdempotencyKey string    `json:"idempotency_key"`
	RecordedAt     time.Time `json:"recorded_at"`
	CommentPosted  bool      `json:"comment_posted"`
	Finalized      bool      `json:"finalized"`
}

func Load(repoRoot string) (*State, error) {
	dir, err := runtimepaths.ResolveDataDir(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime dir: %w", err)
	}
	path := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			st := &State{}
			st.Normalize()
			return st, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode state file: %w", err)
	}
	st.Normalize()
	return &st, nil
}

func (s *State) Save(repoRoot string) error {
	s.Normalize()

	dir, err := runtimepaths.EnsureDataDir(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve runtime dir: %w", err)
	}

	path := filepath.Join(dir, "state.json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

func (s *State) Normalize() {
	if s == nil {
		return
	}
	switch s.Mode {
	case "", ModeManagedPR, ModeIssueTriage, ModeTaskBootstrap:
	default:
		s.Mode = ""
	}

	if s.ActivePR != 0 {
		s.Mode = ModeManagedPR
	}
	if s.Mode == "" {
		s.Mode = ModeIssueTriage
	}

	if s.Mode == ModeManagedPR {
		s.ActiveIssue = 0
		s.PendingTaskID = ""
		s.BootstrapIntent = nil
	}

	if s.Mode == ModeIssueTriage && s.ActivePR == 0 {
		s.BootstrapIntent = nil
	}

	if s.BootstrapIntent != nil {
		intent := s.BootstrapIntent
		if strings.TrimSpace(intent.TaskRef) == "" ||
			strings.TrimSpace(intent.Summary) == "" ||
			strings.TrimSpace(intent.BranchSlug) == "" ||
			strings.TrimSpace(intent.BranchName) == "" ||
			strings.TrimSpace(intent.PRTitle) == "" ||
			strings.TrimSpace(intent.PRBody) == "" {
			s.BootstrapIntent = nil
		}
	}

	if len(s.IssueLinks) > 0 {
		filtered := s.IssueLinks[:0]
		for _, link := range s.IssueLinks {
			if link.IssueNumber <= 0 || strings.TrimSpace(link.IdempotencyKey) == "" {
				continue
			}
			filtered = append(filtered, link)
		}
		s.IssueLinks = filtered
	}

	if s.InFlightAttempt != nil {
		a := s.InFlightAttempt
		if a.AttemptIndex <= 0 || a.MaxAttempts <= 0 || strings.TrimSpace(a.ExpectedBranch) == "" || strings.TrimSpace(a.PromptHash) == "" {
			s.InFlightAttempt = nil
			return
		}
		switch a.Phase {
		case AttemptPhaseStarted, AttemptPhaseAgentExited, AttemptPhaseValidated, AttemptPhaseFailed, AttemptPhaseRecovered:
		default:
			s.InFlightAttempt = nil
		}
	}

	if s.LastRecovery != nil {
		switch s.LastRecovery.Action {
		case RecoveryResume, RecoveryReplay, RecoveryRepair, RecoveryAbort:
		default:
			s.LastRecovery = nil
		}
	}
}
