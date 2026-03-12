package app

import "strings"

func singleQuoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func envelopedAgentCommand(payloads ...string) string {
	commands := []string{`turn="$SIMUG_PROTOCOL_TURN_ID"`}
	for _, payload := range payloads {
		trimmed := strings.TrimSpace(payload)
		if trimmed == "" {
			continue
		}
		commands = append(commands,
			`printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"`,
			`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"%s","payload":%s}\n' "$turn" `+singleQuoteShell(trimmed),
			`printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"`,
		)
		break
	}
	if len(payloads) > 1 {
		commands = []string{`turn="$SIMUG_PROTOCOL_TURN_ID"`, `printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"`}
		for _, payload := range payloads {
			trimmed := strings.TrimSpace(payload)
			if trimmed == "" {
				continue
			}
			commands = append(commands,
				`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"%s","payload":%s}\n' "$turn" `+singleQuoteShell(trimmed),
			)
		}
		commands = append(commands, `printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"`)
	}
	return strings.Join(commands, "; ")
}

func envelopedManagerAndPayloadCommand(managerMessage string, payloads ...string) string {
	commands := []string{}
	if strings.TrimSpace(managerMessage) != "" {
		commands = append(commands, `printf '%s\n' `+singleQuoteShell("SIMUG_MANAGER: "+strings.TrimSpace(managerMessage)))
	}
	if len(payloads) > 0 {
		commands = append(commands, envelopedAgentCommand(payloads...))
	}
	return strings.Join(commands, "; ")
}
