package app

import (
	"context"
	"io"
	"os/exec"
	"time"
)

const (
	defaultAgentCommandExec     = "codex exec"
	defaultAgentCommandFallback = "codex"
)

var (
	agentCommandLookPath = exec.LookPath
	agentCommandProbe    = probeAgentCommand
)

func defaultAgentCommand() string {
	if _, err := agentCommandLookPath("codex"); err != nil {
		return defaultAgentCommandExec
	}
	if agentCommandSupported("codex", "exec", "--help") {
		return defaultAgentCommandExec
	}
	if agentCommandSupported("codex", "--help") {
		return defaultAgentCommandFallback
	}
	return defaultAgentCommandExec
}

func agentCommandSupported(name string, args ...string) bool {
	return agentCommandProbe(name, args...) == nil
}

func probeAgentCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
