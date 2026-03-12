package app

import (
	"fmt"
	"strings"

	"simug/internal/agent"
)

func newCoordinatorTurn(runID string, tickSeq int64, attempt int, sessionID string) agent.CoordinatorTurn {
	return agent.CoordinatorTurn{
		TurnID:    fmt.Sprintf("%s-tick-%06d-attempt-%02d", strings.TrimSpace(runID), tickSeq, attempt),
		SessionID: strings.TrimSpace(sessionID),
	}
}

func appendCoordinatorTurnPrompt(prompt string, turn agent.CoordinatorTurn) string {
	if strings.TrimSpace(turn.TurnID) == "" {
		return prompt
	}

	var b strings.Builder
	b.WriteString(prompt)
	if !strings.HasSuffix(prompt, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\nActive coordinator turn identity:\n")
	b.WriteString(fmt.Sprintf("- turn_id: %s\n", strings.TrimSpace(turn.TurnID)))
	if strings.TrimSpace(turn.SessionID) == "" {
		b.WriteString("- session_id: none\n")
		b.WriteString("- Omit session_id from SIMUG coordinator envelopes for this turn.\n")
	} else {
		b.WriteString(fmt.Sprintf("- session_id: %s\n", strings.TrimSpace(turn.SessionID)))
		b.WriteString("- Include this exact session_id in every SIMUG coordinator begin/action/end envelope for this turn.\n")
	}
	b.WriteString("- Emit exactly one coordinator begin envelope, then coordinator action envelopes, then one matching coordinator end envelope.\n")
	b.WriteString("- The coordinator ignores SIMUG lines outside this exact active turn envelope.\n")
	return b.String()
}
