package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplainLastFailureFromRepoNoFailedTick(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, ".simug")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}

	if err := writeEvents(filepath.Join(dataDir, "events.log"), []map[string]any{
		{
			"kind":    "tick_end",
			"message": "tick completed",
			"fields":  map[string]any{"run_id": "run-a", "tick_seq": 1},
		},
	}); err != nil {
		t.Fatalf("write events: %v", err)
	}

	_, err := explainLastFailureFromRepo(tmp)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "no failed tick found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExplainLastFailureFromRepoIncludesArchiveAndSuggestion(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, ".simug")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}

	archiveMetaPath := filepath.Join(dataDir, "archive", "agent", "run-a", "tick-000001", "attempt-01", "metadata.json")
	if err := os.MkdirAll(filepath.Dir(archiveMetaPath), 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}
	meta := map[string]any{
		"expected_branch":         "agent/20260307-123000-next-task",
		"agent_error":             "",
		"validation_error":        "checkout mismatch for PR #42",
		"protocol_action_count":   3,
		"protocol_terminal_count": 1,
		"protocol_terminal_types": []string{"done"},
		"protocol_actions_excerpt": []string{
			"comment:note",
			"done:changes=true:implemented",
		},
		"protocol_parser_hint": "agent protocol requires exactly one terminal action",
		"rollout_refs":         []string{"/home/sebastien/.codex/sessions/abc/rollout-2026-03-08.jsonl"},
		"session_refs":         []string{"/home/sebastien/.codex/sessions/abc"},
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(archiveMetaPath, append(metaJSON, '\n'), 0o644); err != nil {
		t.Fatalf("write archive metadata: %v", err)
	}

	if err := writeEvents(filepath.Join(dataDir, "events.log"), []map[string]any{
		{
			"kind":    "invariant_decision",
			"message": "checkout synchronization failed",
			"fields": map[string]any{
				"run_id":   "run-a",
				"tick_seq": 1,
				"pass":     false,
				"error":    "checkout mismatch for PR #42",
			},
		},
		{
			"kind":    "agent_archive",
			"message": "archived codex attempt artifacts",
			"fields": map[string]any{
				"run_id":        "run-a",
				"tick_seq":      1,
				"metadata_path": archiveMetaPath,
			},
		},
		{
			"kind":    "tick_end",
			"message": "tick failed",
			"fields": map[string]any{
				"run_id":   "run-a",
				"tick_seq": 1,
				"error":    "checkout mismatch for PR #42",
			},
		},
	}); err != nil {
		t.Fatalf("write events: %v", err)
	}

	out, err := explainLastFailureFromRepo(tmp)
	if err != nil {
		t.Fatalf("explainLastFailureFromRepo returned error: %v", err)
	}

	required := []string{
		"run_id: run-a",
		"tick_seq: 1",
		"violated_invariant: checkout mismatch for PR #42",
		"expected_branch: agent/20260307-123000-next-task",
		"protocol_action_count: 3",
		"protocol_terminal_types: done",
		"rollout_refs: /home/sebastien/.codex/sessions/abc/rollout-2026-03-08.jsonl",
		"suggested_next_action: checkout",
	}
	for _, needle := range required {
		if !strings.Contains(out, needle) {
			t.Fatalf("missing %q in output:\n%s", needle, out)
		}
	}
}

func writeEvents(path string, events []map[string]any) error {
	var lines []string
	for _, event := range events {
		event["ts"] = "2026-03-07T00:00:00Z"
		line, err := json.Marshal(event)
		if err != nil {
			return err
		}
		lines = append(lines, string(line))
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
