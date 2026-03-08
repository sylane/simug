package app

import (
	"errors"
	"testing"
)

func TestLoadConfigReadsSimugEnvVars(t *testing.T) {
	t.Setenv("SIMUG_AGENT_CMD", "codex --profile simug")
	t.Setenv("SIMUG_POLL_SECONDS", "15")
	t.Setenv("SIMUG_MAIN_BRANCH", "trunk")
	t.Setenv("SIMUG_BRANCH_PREFIX", "bot")
	t.Setenv("SIMUG_MAX_REPAIR_ATTEMPTS", "3")
	t.Setenv("SIMUG_ALLOWED_COMMAND_USERS", "alice,bob")
	t.Setenv("SIMUG_ALLOWED_COMMAND_VERBS", "do,status")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}
	if cfg.AgentCommand != "codex --profile simug" {
		t.Fatalf("AgentCommand=%q, want codex --profile simug", cfg.AgentCommand)
	}
	if cfg.PollInterval.Seconds() != 15 {
		t.Fatalf("PollInterval=%s, want 15s", cfg.PollInterval)
	}
	if cfg.MainBranch != "trunk" {
		t.Fatalf("MainBranch=%q, want trunk", cfg.MainBranch)
	}
	if cfg.BranchPrefix != "bot/" {
		t.Fatalf("BranchPrefix=%q, want bot/", cfg.BranchPrefix)
	}
	if cfg.MaxRepairAttempts != 3 {
		t.Fatalf("MaxRepairAttempts=%d, want 3", cfg.MaxRepairAttempts)
	}
	if _, ok := cfg.AllowedUsers["alice"]; !ok {
		t.Fatalf("expected alice in allowed users")
	}
	if _, ok := cfg.AllowedVerbs["status"]; !ok {
		t.Fatalf("expected status in allowed verbs")
	}
}

func TestLoadConfigUsesAutoDetectedAgentCommandWhenUnset(t *testing.T) {
	t.Setenv("SIMUG_AGENT_CMD", "")
	restore := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "/usr/bin/codex", nil },
		func(name string, args ...string) error { return nil },
	)
	defer restore()

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}
	if cfg.AgentCommand != "codex exec" {
		t.Fatalf("AgentCommand=%q, want codex exec", cfg.AgentCommand)
	}
}

func TestLoadConfigDoesNotProbeWhenSimugAgentCmdExplicit(t *testing.T) {
	t.Setenv("SIMUG_AGENT_CMD", "custom-agent --mode strict")
	restore := setAgentCommandProbersForTest(
		func(name string) (string, error) {
			t.Fatalf("lookPath should not run when SIMUG_AGENT_CMD is set")
			return "", nil
		},
		func(name string, args ...string) error {
			t.Fatalf("probe should not run when SIMUG_AGENT_CMD is set")
			return nil
		},
	)
	defer restore()

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}
	if cfg.AgentCommand != "custom-agent --mode strict" {
		t.Fatalf("AgentCommand=%q, want custom-agent --mode strict", cfg.AgentCommand)
	}
}

func TestLoadConfigFallsBackWhenCodexNotFound(t *testing.T) {
	t.Setenv("SIMUG_AGENT_CMD", "")
	restore := setAgentCommandProbersForTest(
		func(name string) (string, error) { return "", errors.New("not found") },
		func(name string, args ...string) error {
			t.Fatalf("probe should not run when codex is missing")
			return nil
		},
	)
	defer restore()

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}
	if cfg.AgentCommand != "codex exec" {
		t.Fatalf("AgentCommand=%q, want codex exec", cfg.AgentCommand)
	}
}
