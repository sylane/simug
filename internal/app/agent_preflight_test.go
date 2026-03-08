package app

import (
	"errors"
	"strings"
	"testing"
)

func TestPreflightAgentCommandSkipsNonCodexCommands(t *testing.T) {
	restoreLookPath := setAgentCommandProbersForTest(
		func(name string) (string, error) {
			t.Fatalf("lookPath should not run for non-codex commands")
			return "", nil
		},
		func(name string, args ...string) error {
			t.Fatalf("command probe should not run for non-codex commands")
			return nil
		},
	)
	defer restoreLookPath()

	restoreProbe := setAgentCommandPreflightProbeForTest(func(command string) (string, error) {
		t.Fatalf("preflight probe should not run for non-codex commands")
		return "", nil
	})
	defer restoreProbe()

	if err := preflightAgentCommand(`printf 'ok\n'`); err != nil {
		t.Fatalf("preflightAgentCommand returned error: %v", err)
	}
}

func TestPreflightAgentCommandFailsWhenCodexMissing(t *testing.T) {
	restoreLookPath := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "", errors.New("not found") },
		func(name string, args ...string) error { return nil },
	)
	defer restoreLookPath()

	restoreProbe := setAgentCommandPreflightProbeForTest(func(command string) (string, error) {
		t.Fatalf("preflight probe should not run when codex is missing")
		return "", nil
	})
	defer restoreProbe()

	err := preflightAgentCommand("codex exec")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "codex CLI not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightAgentCommandClassifiesPermissionFailures(t *testing.T) {
	restoreLookPath := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "/usr/bin/codex", nil },
		func(name string, args ...string) error { return nil },
	)
	defer restoreLookPath()

	restoreProbe := setAgentCommandPreflightProbeForTest(func(command string) (string, error) {
		return "WARNING: failed to clean up stale arg0 temp dirs: Permission denied (os error 13) at path \"/home/me/.codex/tmp/arg0/codex-arg0x\"", nil
	})
	defer restoreProbe()

	err := preflightAgentCommand("codex exec")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "runtime paths appear unwritable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightAgentCommandClassifiesAuthFailures(t *testing.T) {
	restoreLookPath := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "/usr/bin/codex", nil },
		func(name string, args ...string) error { return nil },
	)
	defer restoreLookPath()

	restoreProbe := setAgentCommandPreflightProbeForTest(func(command string) (string, error) {
		return "ERROR: 401 Unauthorized", errors.New("exit status 1")
	})
	defer restoreProbe()

	err := preflightAgentCommand("codex exec")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "authentication appears invalid or missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightAgentCommandReturnsGenericFailureDetails(t *testing.T) {
	restoreLookPath := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "/usr/bin/codex", nil },
		func(name string, args ...string) error { return nil },
	)
	defer restoreLookPath()

	restoreProbe := setAgentCommandPreflightProbeForTest(func(command string) (string, error) {
		return "unclassified failure text", errors.New("exit status 1")
	})
	defer restoreProbe()

	err := preflightAgentCommand("codex exec")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "codex preflight failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func setAgentCommandPreflightProbeForTest(fn func(string) (string, error)) func() {
	previous := agentCommandPreflightProbe
	agentCommandPreflightProbe = fn
	return func() {
		agentCommandPreflightProbe = previous
	}
}
