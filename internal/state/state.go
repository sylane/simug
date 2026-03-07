package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"simug/internal/runtimepaths"
)

type State struct {
	Repo                string    `json:"repo"`
	ActivePR            int       `json:"active_pr"`
	ActiveBranch        string    `json:"active_branch"`
	LastCommentID       int64     `json:"last_comment_id"` // Legacy cursor, retained for migration safety.
	LastIssueCommentID  int64     `json:"last_issue_comment_id"`
	LastReviewCommentID int64     `json:"last_review_comment_id"`
	LastReviewID        int64     `json:"last_review_id"`
	CursorUncertain     bool      `json:"cursor_uncertain"`
	UpdatedAt           time.Time `json:"updated_at"`
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
			return &State{}, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode state file: %w", err)
	}
	return &st, nil
}

func (s *State) Save(repoRoot string) error {
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
