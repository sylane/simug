package app

import (
	"errors"
	"testing"
)

func TestDefaultAgentCommandPrefersCodexExec(t *testing.T) {
	restore := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "/usr/bin/codex", nil },
		func(name string, args ...string) error {
			if name == "codex" && len(args) == 2 && args[0] == "exec" && args[1] == "--help" {
				return nil
			}
			return errors.New("unexpected command probe")
		},
	)
	defer restore()

	got := defaultAgentCommand()
	if got != "codex exec" {
		t.Fatalf("defaultAgentCommand() = %q, want %q", got, "codex exec")
	}
}

func TestDefaultAgentCommandFallsBackToLegacyCodex(t *testing.T) {
	restore := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "/usr/bin/codex", nil },
		func(name string, args ...string) error {
			if name != "codex" {
				return errors.New("unexpected binary")
			}
			if len(args) == 2 && args[0] == "exec" && args[1] == "--help" {
				return errors.New("exec not supported")
			}
			if len(args) == 1 && args[0] == "--help" {
				return nil
			}
			return errors.New("unexpected command probe")
		},
	)
	defer restore()

	got := defaultAgentCommand()
	if got != "codex" {
		t.Fatalf("defaultAgentCommand() = %q, want %q", got, "codex")
	}
}

func TestDefaultAgentCommandKeepsExecWhenCodexMissing(t *testing.T) {
	restore := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "", errors.New("not found") },
		func(name string, args ...string) error {
			t.Fatalf("probe should not run when codex is not in PATH")
			return nil
		},
	)
	defer restore()

	got := defaultAgentCommand()
	if got != "codex exec" {
		t.Fatalf("defaultAgentCommand() = %q, want %q", got, "codex exec")
	}
}

func setAgentCommandProbersForTest(
	lookPath func(string) (string, error),
	probe func(string, ...string) error,
) func() {
	previousLookPath := agentCommandLookPath
	previousProbe := agentCommandProbe
	agentCommandLookPath = lookPath
	agentCommandProbe = probe
	return func() {
		agentCommandLookPath = previousLookPath
		agentCommandProbe = previousProbe
	}
}
