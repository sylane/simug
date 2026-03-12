package app

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"simug/internal/agent"
	"simug/internal/git"
	"simug/internal/state"
)

type dirtyThenCleanRunner struct {
	repoRoot       string
	expectedBranch string
	head           string
	statusCalls    int
}

func (r *dirtyThenCleanRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	key := commandKey(name, args...)
	switch key {
	case commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):
		return r.expectedBranch + "\n", nil
	case commandKey("git", "rev-parse", "HEAD"):
		return r.head + "\n", nil
	case commandKey("git", "status", "--porcelain"):
		r.statusCalls++
		if r.statusCalls == 1 {
			return " M file.txt\n", nil
		}
		return "\n", nil
	default:
		return "", fmt.Errorf("unexpected command: %s", key)
	}
}

func TestPromptTuningHarnessRecoversFromProtocolFailure(t *testing.T) {
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
		runner:  agent.Runner{Command: `input="$(cat)"; turn="$SIMUG_PROTOCOL_TURN_ID"; if printf '%s' "$input" | grep -q "Violation:"; then ` + envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`) + `; else printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"; printf 'SIMUG: {bad-json}\n'; printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"; fi`},
		runID:   "run-harness",
		tickSeq: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _, err := o.runAgentWithValidation(ctx, "initial prompt", expectedBranch, beforeHead, false, false, nil, "", false, nil)
	if err != nil {
		t.Fatalf("runAgentWithValidation returned error: %v", err)
	}
	if result.Terminal.Type != agent.ActionDone {
		t.Fatalf("terminal=%q, want %q", result.Terminal.Type, agent.ActionDone)
	}

	paths, globErr := filepath.Glob(filepath.Join(tmp, ".simug", "archive", "agent", "run-harness", "tick-000001", "attempt-*"))
	if globErr != nil {
		t.Fatalf("glob attempt archives: %v", globErr)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 archived attempts, got %d (%v)", len(paths), paths)
	}
}

func TestPromptTuningHarnessRecoversFromValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	expectedBranch := "agent/20260307-120000-next-task"
	beforeHead := "abcdef"

	seqRunner := &dirtyThenCleanRunner{
		repoRoot:       tmp,
		expectedBranch: expectedBranch,
		head:           beforeHead,
	}
	restoreGit := git.SetCommandRunnerForTest(seqRunner)
	defer restoreGit()

	o := orchestrator{
		repoRoot: tmp,
		state:    &state.State{Mode: state.ModeManagedPR},
		cfg: config{
			MainBranch:        "main",
			BranchPattern:     regexp.MustCompile("^" + regexp.QuoteMeta(expectedBranch) + "$"),
			MaxRepairAttempts: 1,
		},
		runner:  agent.Runner{Command: envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`)},
		runID:   "run-harness",
		tickSeq: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _, err := o.runAgentWithValidation(ctx, "initial prompt", expectedBranch, beforeHead, false, false, nil, "", false, nil)
	if err != nil {
		t.Fatalf("runAgentWithValidation returned error: %v", err)
	}
	if result.Terminal.Type != agent.ActionDone {
		t.Fatalf("terminal=%q, want %q", result.Terminal.Type, agent.ActionDone)
	}
	if seqRunner.statusCalls != 2 {
		t.Fatalf("expected two status checks, got %d", seqRunner.statusCalls)
	}
}

func TestPromptTuningHarnessFailsAfterBoundedProtocolRetries(t *testing.T) {
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
		runner:  agent.Runner{Command: `turn="$SIMUG_PROTOCOL_TURN_ID"; printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"; printf 'SIMUG: {bad-json}\n'; printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"`},
		runID:   "run-harness",
		tickSeq: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := o.runAgentWithValidation(ctx, "initial prompt", expectedBranch, beforeHead, false, false, nil, "", false, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "failed after 2 attempts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAgentWithValidationFailsClosedWhenBootstrapProtocolFailureAdvancedHead(t *testing.T) {
	tmp := t.TempDir()
	expectedBranch := "agent/20260307-120000-next-task"
	beforeHead := "abcdef"
	afterHead := "fedcba"

	restoreGit := git.SetCommandRunnerForTest(mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): expectedBranch + "\n",
		commandKey("git", "status", "--porcelain"):             "\n",
		commandKey("git", "rev-parse", "HEAD"):                 afterHead + "\n",
	}})
	defer restoreGit()

	o := orchestrator{
		repoRoot: tmp,
		state:    &state.State{Mode: state.ModeTaskBootstrap},
		cfg: config{
			MainBranch:        "main",
			BranchPattern:     regexp.MustCompile("^" + regexp.QuoteMeta(expectedBranch) + "$"),
			MaxRepairAttempts: 1,
		},
		runner:  agent.Runner{Command: `turn="$SIMUG_PROTOCOL_TURN_ID"; printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"; printf 'SIMUG: {bad-json}\n'; printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"`},
		runID:   "run-harness",
		tickSeq: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, gotAfterHead, err := o.runAgentWithValidation(ctx, "initial prompt", expectedBranch, beforeHead, true, true, nil, "", true, nil)
	if err == nil {
		t.Fatalf("expected fail-closed error")
	}
	if gotAfterHead != "" {
		t.Fatalf("afterHead=%q, want empty on fail-closed abort", gotAfterHead)
	}
	if !strings.Contains(err.Error(), "refusing automatic repair after execution/protocol failure") {
		t.Fatalf("unexpected error: %v", err)
	}

	paths, globErr := filepath.Glob(filepath.Join(tmp, ".simug", "archive", "agent", "run-harness", "tick-000001", "attempt-*"))
	if globErr != nil {
		t.Fatalf("glob attempt archives: %v", globErr)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 archived attempt, got %d (%v)", len(paths), paths)
	}
}
