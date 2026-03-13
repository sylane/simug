package app

import (
	"fmt"
	"strings"
)

type promptContractSection struct {
	Header            string
	Lines             []string
	TrailingBlankLine bool
}

func (s promptContractSection) String() string {
	var b strings.Builder
	if strings.TrimSpace(s.Header) != "" {
		b.WriteString(s.Header)
		if !strings.HasSuffix(s.Header, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, line := range s.Lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if s.TrailingBlankLine {
		b.WriteByte('\n')
	}
	return b.String()
}

func coordinatorEnvelopeRuleLines(managerInstruction string, extraLines ...string) []string {
	lines := []string{
		"Emit machine actions only inside one bounded SIMUG coordinator envelope.",
		"Emit exactly one coordinator begin envelope and one matching coordinator end envelope for the active turn.",
		"Each coordinator action envelope must use event=action and carry the action JSON in payload.",
		"When the coordinator provides a non-empty session_id for the active turn, include that same session_id in every coordinator envelope.",
		managerInstruction,
		"Coordinator ignores SIMUG lines outside the active turn envelope.",
	}
	return append(lines, extraLines...)
}

func managedPRCoordinatorRules() promptContractSection {
	return promptContractSection{
		Lines: coordinatorEnvelopeRuleLines(
			"Emit manager-facing human messages only with prefix SIMUG_MANAGER:.",
			"Unprefixed narrative text is quarantined and ignored by the coordinator.",
			"Terminal protocol action must be exactly one of done or idle.",
		),
		TrailingBlankLine: true,
	}
}

func managedPRCoordinatorSchema() promptContractSection {
	return promptContractSection{
		Header: "Coordinator envelope schema for this managed PR turn:",
		Lines: []string{
			"- SIMUG_MANAGER: <human-friendly manager message>",
			"- begin envelope: coordinator event=begin for the active turn_id (and session_id when provided)",
			"- action envelope payload.action may be comment(body), reply(comment_id, body), issue_update(issue_number, relation, comment), done(summary, changes, optional pr_title, optional pr_body), or idle(reason)",
			"- end envelope: coordinator event=end matching the same active turn identity",
		},
		TrailingBlankLine: true,
	}
}

func issueTriageCoordinatorRules() promptContractSection {
	return promptContractSection{
		Lines: coordinatorEnvelopeRuleLines(
			"Emit manager-facing human messages only with prefix SIMUG_MANAGER:.",
			"Unprefixed narrative text is quarantined and ignored by the coordinator.",
			"Emit exactly one issue_report action before terminal action.",
			"Terminal protocol action must be exactly one of done or idle.",
		),
		TrailingBlankLine: true,
	}
}

func issueTriageProtocolExamples() promptContractSection {
	return promptContractSection{
		Header: "Protocol JSON lines:",
		Lines: []string{
			"SIMUG_MANAGER: <human-friendly manager message>",
			`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"<ACTIVE_TURN_ID>"}`,
			`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"issue_report","issue_number":123,"relevant":true,"analysis":"...","needs_task":true,"task_title":"...","task_body":"..."}}`,
			`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"done","summary":"issue triaged","changes":false}}`,
			`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"idle","reason":"..."}}`,
			`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"<ACTIVE_TURN_ID>"}`,
		},
		TrailingBlankLine: true,
	}
}

func bootstrapIntentCoordinatorRules() promptContractSection {
	return promptContractSection{
		Lines: coordinatorEnvelopeRuleLines(
			"Use SIMUG_MANAGER: for manager-facing human text; unprefixed text is quarantined.",
		),
		TrailingBlankLine: false,
	}
}

func bootstrapIntentProtocolExamples() promptContractSection {
	return promptContractSection{
		Header: "Protocol JSON lines:",
		Lines: []string{
			"SIMUG_MANAGER: <human-friendly manager message>",
			`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"<ACTIVE_TURN_ID>"}`,
			`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"comment","body":"INTENT_JSON:{\"task_ref\":\"Task 7.2a\",\"summary\":\"...\",\"branch_slug\":\"intent-handshake\",\"pr_title\":\"...\",\"pr_body\":\"...\",\"checks\":[\"GOCACHE=/tmp/go-build go test ./...\"]}"}}`,
			`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"done","summary":"intent prepared","changes":false}}`,
			`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"<ACTIVE_TURN_ID>","payload":{"action":"idle","reason":"no task available"}}`,
			`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"<ACTIVE_TURN_ID>"}`,
		},
		TrailingBlankLine: false,
	}
}

func bootstrapExecutionCoordinatorRules() promptContractSection {
	return promptContractSection{
		Lines: coordinatorEnvelopeRuleLines(
			"Use SIMUG_MANAGER: for manager-facing human text; unprefixed text is quarantined.",
			"Keep working tree clean before finishing.",
		),
		TrailingBlankLine: false,
	}
}

func bootstrapExecutionCoordinatorSchema() promptContractSection {
	return promptContractSection{
		Header: "Coordinator envelope schema for this execution turn:",
		Lines: []string{
			"- SIMUG_MANAGER: <human-friendly manager message>",
			"- begin envelope: coordinator event=begin for the active turn_id (and session_id when provided)",
			"- action envelope payload.action may be comment(body), issue_update(issue_number, relation, comment), done(summary, changes, optional pr_title, optional pr_body), or idle(reason)",
			"- when payload.action is comment and terminal action is done, exactly one comment body must start with REPORT_JSON: and include task_ref, summary, branch, and head from this run",
			"- end envelope: coordinator event=end matching the same active turn identity",
		},
		TrailingBlankLine: false,
	}
}

func repairCoordinatorRules(expectedBranch, mainBranch string) promptContractSection {
	return promptContractSection{
		Lines: []string{
			"- emit machine actions only inside one bounded SIMUG coordinator envelope",
			"- emit exactly one coordinator begin envelope and one matching coordinator end envelope for the active turn",
			"- each coordinator action envelope must use event=action and carry the action JSON in payload",
			"- when the coordinator provides a non-empty session_id for the active turn, include that same session_id in every coordinator envelope",
			"- use SIMUG_MANAGER: for manager-facing messages; unprefixed text is quarantined",
			"- coordinator ignores SIMUG lines outside the active turn envelope",
			fmt.Sprintf("- branch must be %q (or %q if terminal action is idle)", expectedBranch, mainBranch),
			"- keep the working tree clean before finishing",
		},
		TrailingBlankLine: false,
	}
}

func repairCoordinatorSchema() promptContractSection {
	return promptContractSection{
		Header: "Coordinator envelope schema for this repair turn:",
		Lines: []string{
			"- SIMUG_MANAGER: <human-friendly manager message>",
			"- begin envelope: coordinator event=begin for the active turn_id (and session_id when provided)",
			"- action envelope payload.action may be comment(body), reply(comment_id, body), issue_update(issue_number, relation, comment), done(summary, changes), or idle(reason)",
			"- end envelope: coordinator event=end matching the same active turn identity",
		},
		TrailingBlankLine: false,
	}
}
