package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"simug/internal/git"
	"simug/internal/runtimepaths"
)

type loggedEvent struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields"`
}

type archivedMetadata struct {
	ExpectedBranch         string   `json:"expected_branch"`
	ProtocolTurnID         string   `json:"protocol_turn_id"`
	ProtocolSessionID      string   `json:"protocol_session_id"`
	AgentError             string   `json:"agent_error"`
	ValidationError        string   `json:"validation_error"`
	ProtocolActionCount    int      `json:"protocol_action_count"`
	ProtocolActionsExcerpt []string `json:"protocol_actions_excerpt"`
	ProtocolTerminalCount  int      `json:"protocol_terminal_count"`
	ProtocolTerminalTypes  []string `json:"protocol_terminal_types"`
	ProtocolParserHint     string   `json:"protocol_parser_hint"`
	RolloutRefs            []string `json:"rollout_refs"`
	SessionRefs            []string `json:"session_refs"`
}

// ExplainLastFailure summarizes the most recent failed tick.
func ExplainLastFailure(ctx context.Context, startDir string) (string, error) {
	repoRoot, err := git.RepoRoot(ctx, startDir)
	if err != nil {
		return "", err
	}
	return explainLastFailureFromRepo(repoRoot)
}

func explainLastFailureFromRepo(repoRoot string) (string, error) {
	dataDir, err := runtimepaths.ResolveDataDir(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolve runtime dir: %w", err)
	}

	eventsPath := filepath.Join(dataDir, "events.log")
	lines, err := readJSONLEvents(eventsPath)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("no events found in %s", eventsPath)
	}

	failure, ok := findLastFailedTick(lines)
	if !ok {
		return "", fmt.Errorf("no failed tick found in %s", eventsPath)
	}

	runID := stringField(failure.Fields, "run_id")
	tickSeq := int64Field(failure.Fields, "tick_seq")
	failureReason := strings.TrimSpace(stringField(failure.Fields, "error"))
	if failureReason == "" {
		failureReason = strings.TrimSpace(failure.Message)
	}

	invariantReason := ""
	for i := len(lines) - 1; i >= 0; i-- {
		e := lines[i]
		if e.Kind != "invariant_decision" {
			continue
		}
		if stringField(e.Fields, "run_id") != runID || int64Field(e.Fields, "tick_seq") != tickSeq {
			continue
		}
		if boolField(e.Fields, "pass") {
			continue
		}
		invariantReason = strings.TrimSpace(stringField(e.Fields, "error"))
		if invariantReason == "" {
			invariantReason = strings.TrimSpace(e.Message)
		}
		break
	}

	var archivePath string
	var archiveMeta archivedMetadata
	for i := len(lines) - 1; i >= 0; i-- {
		e := lines[i]
		if e.Kind != "agent_archive" {
			continue
		}
		if stringField(e.Fields, "run_id") != runID || int64Field(e.Fields, "tick_seq") != tickSeq {
			continue
		}
		archivePath = stringField(e.Fields, "metadata_path")
		if strings.TrimSpace(archivePath) != "" {
			raw, readErr := os.ReadFile(archivePath)
			if readErr == nil {
				_ = json.Unmarshal(raw, &archiveMeta)
			}
		}
		break
	}

	suggestion := suggestedAction(
		failureReason,
		invariantReason,
		archiveMeta.ExpectedBranch,
		archiveMeta.AgentError,
		archiveMeta.ValidationError,
	)

	var b strings.Builder
	b.WriteString("Last failure summary\n")
	b.WriteString(fmt.Sprintf("- run_id: %s\n", nonEmpty(runID, "unknown")))
	b.WriteString(fmt.Sprintf("- tick_seq: %d\n", tickSeq))
	b.WriteString(fmt.Sprintf("- failure: %s\n", nonEmpty(failureReason, "unknown")))
	if invariantReason != "" {
		b.WriteString(fmt.Sprintf("- violated_invariant: %s\n", invariantReason))
	}
	if archivePath != "" {
		b.WriteString(fmt.Sprintf("- archive_metadata: %s\n", archivePath))
	}
	if archiveMeta.ExpectedBranch != "" {
		b.WriteString(fmt.Sprintf("- expected_branch: %s\n", archiveMeta.ExpectedBranch))
	}
	if archiveMeta.ProtocolTurnID != "" {
		b.WriteString(fmt.Sprintf("- protocol_turn_id: %s\n", archiveMeta.ProtocolTurnID))
	}
	if archiveMeta.ProtocolSessionID != "" {
		b.WriteString(fmt.Sprintf("- protocol_session_id: %s\n", archiveMeta.ProtocolSessionID))
	}
	if archiveMeta.AgentError != "" {
		b.WriteString(fmt.Sprintf("- agent_error: %s\n", archiveMeta.AgentError))
	}
	if archiveMeta.ValidationError != "" {
		b.WriteString(fmt.Sprintf("- validation_error: %s\n", archiveMeta.ValidationError))
	}
	if archiveMeta.ProtocolActionCount > 0 {
		b.WriteString(fmt.Sprintf("- protocol_action_count: %d\n", archiveMeta.ProtocolActionCount))
	}
	if archiveMeta.ProtocolTerminalCount > 0 {
		b.WriteString(fmt.Sprintf("- protocol_terminal_count: %d\n", archiveMeta.ProtocolTerminalCount))
	}
	if len(archiveMeta.ProtocolTerminalTypes) > 0 {
		b.WriteString(fmt.Sprintf("- protocol_terminal_types: %s\n", strings.Join(archiveMeta.ProtocolTerminalTypes, ",")))
	}
	if len(archiveMeta.ProtocolActionsExcerpt) > 0 {
		b.WriteString(fmt.Sprintf("- protocol_actions_excerpt: %s\n", strings.Join(archiveMeta.ProtocolActionsExcerpt, " | ")))
	}
	if archiveMeta.ProtocolParserHint != "" {
		b.WriteString(fmt.Sprintf("- protocol_parser_hint: %s\n", archiveMeta.ProtocolParserHint))
	}
	if len(archiveMeta.RolloutRefs) > 0 {
		b.WriteString(fmt.Sprintf("- rollout_refs: %s\n", strings.Join(archiveMeta.RolloutRefs, ",")))
	}
	if len(archiveMeta.SessionRefs) > 0 {
		b.WriteString(fmt.Sprintf("- session_refs: %s\n", strings.Join(archiveMeta.SessionRefs, ",")))
	}
	b.WriteString(fmt.Sprintf("- suggested_next_action: %s", suggestion))
	return b.String(), nil
}

func readJSONLEvents(path string) ([]loggedEvent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("event log not found: %s", path)
		}
		return nil, fmt.Errorf("read event log: %w", err)
	}

	var out []loggedEvent
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e loggedEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func findLastFailedTick(events []loggedEvent) (loggedEvent, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != "tick_end" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(e.Message), "tick failed") {
			return e, true
		}
		if strings.TrimSpace(stringField(e.Fields, "error")) != "" {
			return e, true
		}
	}
	return loggedEvent{}, false
}

func suggestedAction(failureReason, invariantReason, expectedBranch, agentError, validationError string) string {
	all := strings.ToLower(strings.Join([]string{failureReason, invariantReason, agentError, validationError}, "\n"))

	switch {
	case strings.Contains(all, "multiple open prs"):
		return "close or merge extra authored open PRs so only one managed lane remains, then rerun simug"
	case strings.Contains(all, "dirty working tree"):
		return "clean or commit local changes, then rerun simug"
	case strings.Contains(all, "checkout mismatch"), strings.Contains(all, "expected current branch"):
		if expectedBranch != "" {
			return fmt.Sprintf("checkout %q, sync it with origin, ensure clean tree, then rerun simug", expectedBranch)
		}
		return "re-align local checkout with managed PR head and origin, then rerun simug"
	case strings.Contains(all, "agent protocol"), strings.Contains(all, "missing protocol lines"):
		return "inspect archived transcript.log, raw_output.txt, and prompt.txt for the failed attempt, then tighten prompt/protocol handling before rerun"
	case strings.Contains(all, "failed validation"):
		return "inspect invariant_decision and archive metadata for the failed attempt, repair repository consistency, then rerun simug"
	default:
		return "inspect .simug/events.log and latest .simug/archive/agent attempt artifacts, apply the indicated fix, then rerun simug"
	}
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func int64Field(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	default:
		return 0
	}
}

func boolField(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
