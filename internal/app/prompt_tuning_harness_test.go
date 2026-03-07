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
		cfg: config{
			MainBranch:        "main",
			BranchPattern:     regexp.MustCompile("^" + regexp.QuoteMeta(expectedBranch) + "$"),
			MaxRepairAttempts: 1,
		},
		runner:  agent.Runner{Command: `input="$(cat)"; if printf '%s' "$input" | grep -q "Violation:"; then printf 'SIMUG: {"action":"done","summary":"ok","changes":false}\n'; else printf 'SIMUG: {bad-json}\n'; fi`},
		runID:   "run-harness",
		tickSeq: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _, err := o.runAgentWithValidation(ctx, "initial prompt", expectedBranch, beforeHead, false, false, nil)
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
		cfg: config{
			MainBranch:        "main",
			BranchPattern:     regexp.MustCompile("^" + regexp.QuoteMeta(expectedBranch) + "$"),
			MaxRepairAttempts: 1,
		},
		runner:  agent.Runner{Command: `printf 'SIMUG: {"action":"done","summary":"ok","changes":false}\n'`},
		runID:   "run-harness",
		tickSeq: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, _, err := o.runAgentWithValidation(ctx, "initial prompt", expectedBranch, beforeHead, false, false, nil)
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
		cfg: config{
			MainBranch:        "main",
			BranchPattern:     regexp.MustCompile("^" + regexp.QuoteMeta(expectedBranch) + "$"),
			MaxRepairAttempts: 1,
		},
		runner:  agent.Runner{Command: `printf 'SIMUG: {bad-json}\n'`},
		runID:   "run-harness",
		tickSeq: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := o.runAgentWithValidation(ctx, "initial prompt", expectedBranch, beforeHead, false, false, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "failed after 2 attempts") {
		t.Fatalf("unexpected error: %v", err)
	}
}
