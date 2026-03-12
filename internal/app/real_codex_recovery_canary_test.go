package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"simug/internal/agent"
	"simug/internal/git"
	"simug/internal/state"
)

type recoveryCanaryResult struct {
	Name      string `json:"name"`
	Passed    bool   `json:"passed"`
	ErrorText string `json:"error,omitempty"`
}

func TestRealCodexRepairInteractionCanary(t *testing.T) {
	if os.Getenv("SIMUG_REAL_CODEX") != "1" {
		t.Skip("set SIMUG_REAL_CODEX=1 to run real Codex recovery canary")
	}
	cmd := strings.TrimSpace(os.Getenv("SIMUG_REAL_CODEX_CMD"))
	if cmd == "" {
		t.Fatal("SIMUG_REAL_CODEX_CMD is required when SIMUG_REAL_CODEX=1")
	}

	tmp := t.TempDir()
	expectedBranch := "agent/20260307-120000-next-task"
	beforeHead := "abcdef"
	restoreGit := git.SetCommandRunnerForTest(mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): expectedBranch + "\n",
		commandKey("git", "status", "--porcelain"):             "\n",
		commandKey("git", "rev-parse", "HEAD"):                 beforeHead + "\n",
	}})
	defer restoreGit()

	o := orchestrator{
		repoRoot: tmp,
		state:    &state.State{Mode: state.ModeManagedPR},
		cfg: config{
			MainBranch:        "main",
			BranchPattern:     regexp.MustCompile("^" + regexp.QuoteMeta(expectedBranch) + "$"),
			MaxRepairAttempts: 1,
		},
		runner:  agent.Runner{Command: cmd},
		runID:   "real-codex-recovery",
		tickSeq: 1,
	}

	prompt := strings.Join([]string{
		"You are a real Codex repair canary.",
		"If this prompt contains 'Violation:' output exactly one bounded coordinator envelope ending in a done action with summary 'repair canary ok' and changes=false.",
		"Otherwise output exactly one malformed SIMUG line and no valid coordinator envelope.",
		`MALFORMED: {bad-json}`,
		"No extra text.",
	}, "\n")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, _, err := o.runAgentWithValidation(ctx, prompt, expectedBranch, beforeHead, false, false, nil, "", false, nil)
	cr := recoveryCanaryResult{Name: "repair-interaction"}
	if err != nil {
		cr.Passed = false
		cr.ErrorText = err.Error()
	} else if result.Terminal.Type != agent.ActionDone {
		cr.Passed = false
		cr.ErrorText = fmt.Sprintf("terminal=%q, want done", result.Terminal.Type)
	} else {
		cr.Passed = true
	}
	writeRecoveryCanaryResult(t, cr)
	if !cr.Passed {
		t.Fatalf("real Codex repair interaction canary failed: %s", cr.ErrorText)
	}
}

func TestRealCodexRestartRecoveryBoundaryCanary(t *testing.T) {
	if os.Getenv("SIMUG_REAL_CODEX") != "1" {
		t.Skip("set SIMUG_REAL_CODEX=1 to run real Codex recovery canary")
	}

	tmp := t.TempDir()
	restoreGit := git.SetCommandRunnerForTest(mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): "main\n",
		commandKey("git", "status", "--porcelain"):             "\n",
	}})
	defer restoreGit()

	o := orchestrator{
		repoRoot: tmp,
		state: &state.State{
			Mode: state.ModeIssueTriage,
			InFlightAttempt: &state.Attempt{
				RunID:          "run-a",
				TickSeq:        7,
				AttemptIndex:   1,
				MaxAttempts:    2,
				ExpectedBranch: "main",
				Mode:           state.ModeIssueTriage,
				Phase:          state.AttemptPhaseValidated,
				PromptHash:     "abc123",
				BeforeHead:     "deadbeef",
				AfterHead:      "deadbeef",
				StartedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
			},
		},
	}

	err := o.recoverInterruptedAttempt(context.Background())
	cr := recoveryCanaryResult{Name: "restart-boundary"}
	if err != nil {
		cr.Passed = false
		cr.ErrorText = err.Error()
	} else if o.state.LastRecovery == nil || o.state.LastRecovery.Action != state.RecoveryResume {
		cr.Passed = false
		cr.ErrorText = "expected last_recovery.action=resume"
	} else {
		cr.Passed = true
	}
	writeRecoveryCanaryResult(t, cr)
	if !cr.Passed {
		t.Fatalf("real Codex restart recovery boundary canary failed: %s", cr.ErrorText)
	}
}

func writeRecoveryCanaryResult(t *testing.T, result recoveryCanaryResult) {
	t.Helper()
	outRoot := strings.TrimSpace(os.Getenv("SIMUG_CANARY_OUT_DIR"))
	if outRoot == "" {
		outRoot = filepath.Join(".simug", "canary", "real-codex")
	}
	runDir := filepath.Join(outRoot, "recovery")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("create recovery canary output dir: %v", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal recovery canary result: %v", err)
	}
	path := filepath.Join(runDir, result.Name+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write recovery canary result: %v", err)
	}
}
