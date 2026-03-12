package app

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"simug/internal/agent"
)

var agentCommandPreflightProbe = runAgentCommandPreflight

func preflightAgentCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return fmt.Errorf("agent command is empty (set SIMUG_AGENT_CMD)")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || fields[0] != "codex" {
		return nil
	}

	if _, err := agentCommandLookPath("codex"); err != nil {
		return fmt.Errorf("codex CLI not found in PATH for %q: %w (install Codex CLI or set SIMUG_AGENT_CMD)", trimmed, err)
	}

	out, err := agentCommandPreflightProbe(trimmed)
	assessment := agent.CodexRuntimeAssessment(trimmed, out)
	if assessment.Hint != "" && err != nil {
		return fmt.Errorf("codex preflight failed for %q: %s", trimmed, assessment.Hint)
	}
	if err != nil {
		return fmt.Errorf("codex preflight failed for %q: %w: %s", trimmed, err, limitString(out, 600))
	}
	return nil
}

func runAgentCommandPreflight(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command+" --help")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
