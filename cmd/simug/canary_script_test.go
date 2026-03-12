package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealCodexCanaryScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "canary-real-codex-protocol.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read canary script: %v", err)
	}
	content := string(data)

	required := []string{
		"SIMUG_REAL_CODEX=1",
		"SIMUG_REAL_CODEX_CMD",
		"SIMUG_CANARY_OUT_DIR",
		"default_agent_cmd",
		"preflight_agent_cmd",
		"codex exec",
		"go test ./internal/agent -run TestRealCodexProtocolConformanceCanary",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in canary script", needle)
		}
	}
}

func TestRealCodexRecoveryCanaryScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "canary-real-codex-recovery.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recovery canary script: %v", err)
	}
	content := string(data)

	required := []string{
		"SIMUG_REAL_CODEX=1",
		"SIMUG_REAL_CODEX_CMD",
		"SIMUG_CANARY_OUT_DIR",
		"default_agent_cmd",
		"preflight_agent_cmd",
		"codex exec",
		"go test ./internal/app -run 'TestRealCodex(RepairInteractionCanary|RestartRecoveryBoundaryCanary)$'",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in recovery canary script", needle)
		}
	}
}

func TestRealCodexGateScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "canary-real-codex-gate.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gate script: %v", err)
	}
	content := string(data)

	required := []string{
		"canary-real-codex-protocol.sh",
		"canary-real-codex-recovery.sh",
		"default_agent_cmd",
		"preflight_agent_cmd",
		"phase: preflight",
		"phase: protocol_canary",
		"phase: recovery_canary",
		"codex exec",
		"summary.json",
		"--retain-days",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in gate script", needle)
		}
	}
}

func TestSandboxDryRunScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "sandbox-dry-run.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sandbox dry-run script: %v", err)
	}
	content := string(data)

	required := []string{
		"--repo",
		"--issue-pr",
		"--planning-pr",
		"gh pr view",
		".simug/canary/sandbox",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in sandbox dry-run script", needle)
		}
	}
}

func TestSelfHostCanaryScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "self-host-canary.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read self-host canary script: %v", err)
	}
	content := string(data)

	required := []string{
		"self-host-loop.sh",
		"phase1.log",
		"phase2.log",
		"summary.json",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in self-host canary script", needle)
		}
	}
}

func TestChaosStopRestartScriptContract(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "chaos-stop-restart.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read chaos script: %v", err)
	}
	content := string(data)

	required := []string{
		"SIGTERM",
		"SIGKILL",
		"run --once",
		".simug/chaos",
		"summary.json",
	}
	for _, needle := range required {
		if !strings.Contains(content, needle) {
			t.Fatalf("missing %q in chaos script", needle)
		}
	}
}
