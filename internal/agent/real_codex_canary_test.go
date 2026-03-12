package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type canaryResult struct {
	Name      string `json:"name"`
	Passed    bool   `json:"passed"`
	ErrorText string `json:"error,omitempty"`
}

func TestRealCodexProtocolConformanceCanary(t *testing.T) {
	if os.Getenv("SIMUG_REAL_CODEX") != "1" {
		t.Skip("set SIMUG_REAL_CODEX=1 to run real Codex protocol canary")
	}
	cmd := strings.TrimSpace(os.Getenv("SIMUG_REAL_CODEX_CMD"))
	if cmd == "" {
		t.Fatal("SIMUG_REAL_CODEX_CMD is required when SIMUG_REAL_CODEX=1")
	}

	outRoot := strings.TrimSpace(os.Getenv("SIMUG_CANARY_OUT_DIR"))
	if outRoot == "" {
		outRoot = filepath.Join(".simug", "canary", "real-codex")
	}
	runDir := filepath.Join(outRoot, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("create canary output dir: %v", err)
	}

	scenarios := []struct {
		name        string
		turnID      string
		prompt      string
		expectError bool
	}{
		{
			name:   "managed-pr",
			turnID: "canary-managed-pr",
			prompt: strings.Join([]string{
				"You are a protocol conformance canary.",
				"Respond with one bounded coordinator envelope and no additional text.",
				`Line 1: SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"canary-managed-pr"}`,
				`Line 2: SIMUG: {"envelope":"coordinator","event":"action","turn_id":"canary-managed-pr","payload":{"action":"comment","body":"canary managed-pr comment"}}`,
				`Line 3: SIMUG: {"envelope":"coordinator","event":"action","turn_id":"canary-managed-pr","payload":{"action":"done","summary":"canary managed-pr done","changes":false}}`,
				`Line 4: SIMUG: {"envelope":"coordinator","event":"end","turn_id":"canary-managed-pr"}`,
			}, "\n"),
		},
		{
			name:   "issue-triage",
			turnID: "canary-issue-triage",
			prompt: strings.Join([]string{
				"You are a protocol conformance canary.",
				"Respond with one bounded coordinator envelope and no additional text.",
				`Line 1: SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"canary-issue-triage"}`,
				`Line 2: SIMUG: {"envelope":"coordinator","event":"action","turn_id":"canary-issue-triage","payload":{"action":"issue_report","issue_number":7,"relevant":true,"analysis":"canary triage","needs_task":false}}`,
				`Line 3: SIMUG: {"envelope":"coordinator","event":"action","turn_id":"canary-issue-triage","payload":{"action":"done","summary":"canary triage done","changes":false}}`,
				`Line 4: SIMUG: {"envelope":"coordinator","event":"end","turn_id":"canary-issue-triage"}`,
			}, "\n"),
		},
		{
			name:   "task-bootstrap",
			turnID: "canary-task-bootstrap",
			prompt: strings.Join([]string{
				"You are a protocol conformance canary.",
				"Respond with one bounded coordinator envelope and no additional text.",
				`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"canary-task-bootstrap"}`,
				`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"canary-task-bootstrap","payload":{"action":"idle","reason":"canary bootstrap idle"}}`,
				`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"canary-task-bootstrap"}`,
			}, "\n"),
		},
		{
			name:   "malformed-protocol-output",
			turnID: "canary-malformed",
			prompt: strings.Join([]string{
				"You are a protocol conformance canary.",
				"Respond with a bounded coordinator begin line, then one malformed SIMUG line, then the matching coordinator end line.",
				`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"canary-malformed"}`,
				`SIMUG: {bad-json}`,
				`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"canary-malformed"}`,
			}, "\n"),
			expectError: true,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	for idx, scenario := range scenarios {
		scenarioDir := filepath.Join(runDir, fmt.Sprintf("%02d-%s", idx+1, scenario.name))
		if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
			t.Fatalf("create scenario output dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(scenarioDir, "prompt.txt"), []byte(scenario.prompt), 0o644); err != nil {
			t.Fatalf("write prompt artifact: %v", err)
		}

		runner := Runner{Command: cmd, Turn: CoordinatorTurn{TurnID: scenario.turnID}}
		result, err := runner.Run(ctx, scenario.prompt)
		rawOutput := result.RawOutput
		if err != nil {
			rawOutput = RawOutputFromError(err)
		}
		if err := os.WriteFile(filepath.Join(scenarioDir, "raw_output.txt"), []byte(rawOutput), 0o644); err != nil {
			t.Fatalf("write raw output artifact: %v", err)
		}

		cr := canaryResult{Name: scenario.name}
		if scenario.expectError {
			if err == nil {
				cr.Passed = false
				cr.ErrorText = "expected protocol failure, got success"
			} else {
				cr.Passed = true
				cr.ErrorText = err.Error()
			}
		} else {
			if err != nil {
				cr.Passed = false
				cr.ErrorText = err.Error()
			} else {
				cr.Passed = true
			}
		}

		data, marshalErr := json.MarshalIndent(cr, "", "  ")
		if marshalErr != nil {
			t.Fatalf("marshal canary result: %v", marshalErr)
		}
		if err := os.WriteFile(filepath.Join(scenarioDir, "result.json"), append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write scenario result artifact: %v", err)
		}

		if !cr.Passed {
			t.Fatalf("scenario %q failed: %s (artifacts: %s)", scenario.name, cr.ErrorText, scenarioDir)
		}
	}
}
