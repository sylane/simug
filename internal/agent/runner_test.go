package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseActionsParsesProtocolLines(t *testing.T) {
	actions, err := parseActions("SIMUG: {\"action\":\"comment\",\"body\":\"hi\"}\n")
	if err != nil {
		t.Fatalf("parseActions returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionComment {
		t.Fatalf("expected action type %q, got %q", ActionComment, actions[0].Type)
	}
}

func TestParseActionsRejectsUnknownProtocolPrefix(t *testing.T) {
	_, err := parseActions("OTHER: {\"action\":\"comment\",\"body\":\"nope\"}\n")
	if err == nil {
		t.Fatalf("expected error for unknown protocol prefix")
	}
}

func TestParseRoutedOutputRoutesManagerAndQuarantinesUnprefixed(t *testing.T) {
	out, err := parseRoutedOutput(strings.Join([]string{
		"SIMUG_MANAGER: Heads up",
		"random narrative line",
		`SIMUG: {"action":"done","summary":"ok","changes":false}`,
	}, "\n"), CoordinatorTurn{})
	if err != nil {
		t.Fatalf("parseRoutedOutput returned error: %v", err)
	}
	if len(out.Actions) != 1 || out.Actions[0].Type != ActionDone {
		t.Fatalf("unexpected actions: %#v", out.Actions)
	}
	if len(out.ManagerMessages) != 1 || out.ManagerMessages[0] != "Heads up" {
		t.Fatalf("unexpected manager messages: %#v", out.ManagerMessages)
	}
	if len(out.QuarantinedLines) != 1 || out.QuarantinedLines[0] != "random narrative line" {
		t.Fatalf("unexpected quarantined lines: %#v", out.QuarantinedLines)
	}
}

func TestParseRoutedOutputRejectsManagerPrefixAbuseSpacing(t *testing.T) {
	out, err := parseRoutedOutput(strings.Join([]string{
		"SIMUG_MANAGER : spoof with spacing",
		`SIMUG: {"action":"done","summary":"ok","changes":false}`,
	}, "\n"), CoordinatorTurn{})
	if err != nil {
		t.Fatalf("parseRoutedOutput returned error: %v", err)
	}
	if len(out.ManagerMessages) != 0 {
		t.Fatalf("expected no manager messages, got %#v", out.ManagerMessages)
	}
	if len(out.QuarantinedLines) != 1 || out.QuarantinedLines[0] != "SIMUG_MANAGER : spoof with spacing" {
		t.Fatalf("unexpected quarantined lines: %#v", out.QuarantinedLines)
	}
}

func TestParseRoutedOutputIgnoresPromptTemplateProtocolLines(t *testing.T) {
	out, err := parseRoutedOutput(strings.Join([]string{
		`SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"optional","pr_body":"optional"}`,
		`SIMUG: {"action":"done","summary":"real result","changes":false}`,
	}, "\n"), CoordinatorTurn{})
	if err != nil {
		t.Fatalf("parseRoutedOutput returned error: %v", err)
	}
	out.Actions = removePromptTemplateEchoSequences(out.Actions)
	if len(out.Actions) != 1 {
		t.Fatalf("expected 1 parsed action after filtering template line, got %d", len(out.Actions))
	}
	if out.Actions[0].Summary != "real result" {
		t.Fatalf("unexpected action summary: %#v", out.Actions[0])
	}
}

func TestParseRoutedOutputUsesOnlyMatchingActiveTurnEnvelope(t *testing.T) {
	turn := CoordinatorTurn{TurnID: "turn-123"}
	out, err := parseRoutedOutput(strings.Join([]string{
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"stale-turn","payload":{"action":"done","summary":"stale","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"comment","body":"hi"}}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"done","summary":"ok","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"idle","reason":"echo"}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
	}, "\n"), turn)
	if err != nil {
		t.Fatalf("parseRoutedOutput returned error: %v", err)
	}
	if len(out.Actions) != 2 {
		t.Fatalf("actions=%d, want 2", len(out.Actions))
	}
	if out.Actions[1].Type != ActionDone || out.Actions[1].Summary != "ok" {
		t.Fatalf("unexpected actions: %#v", out.Actions)
	}
}

func TestCollectProtocolForensicsSeparatesActiveAndIgnoredTurnLines(t *testing.T) {
	turn := CoordinatorTurn{TurnID: "turn-123"}
	raw := strings.Join([]string{
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"stale-turn","payload":{"action":"done","summary":"stale","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"stale-turn"}`,
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"done","summary":"ok","changes":false}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{"action":"idle","reason":"echo"}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
	}, "\n")

	forensics := CollectProtocolForensics(raw, turn)
	if got := len(forensics.RawProtocolLines); got != 9 {
		t.Fatalf("raw protocol lines=%d, want 9", got)
	}
	if got := len(forensics.ActiveProtocolLines); got != 3 {
		t.Fatalf("active protocol lines=%d, want 3", got)
	}
	if got := len(forensics.IgnoredProtocolLines); got != 6 {
		t.Fatalf("ignored protocol lines=%d, want 6", got)
	}
	if forensics.AcceptedTurn.TurnID != turn.TurnID {
		t.Fatalf("accepted turn=%q, want %q", forensics.AcceptedTurn.TurnID, turn.TurnID)
	}
	if !strings.Contains(forensics.ActiveProtocolLines[1], `"summary":"ok"`) {
		t.Fatalf("unexpected active lines: %#v", forensics.ActiveProtocolLines)
	}
	if !strings.Contains(forensics.IgnoredProtocolLines[0], `"turn_id":"stale-turn"`) {
		t.Fatalf("unexpected ignored lines: %#v", forensics.IgnoredProtocolLines)
	}
}

func TestParseRoutedOutputRejectsMalformedActiveTurnAction(t *testing.T) {
	turn := CoordinatorTurn{TurnID: "turn-123"}
	_, err := parseRoutedOutput(strings.Join([]string{
		`SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"turn-123"}`,
		`SIMUG: {"envelope":"coordinator","event":"action","turn_id":"turn-123","payload":{bad-json}}`,
		`SIMUG: {"envelope":"coordinator","event":"end","turn_id":"turn-123"}`,
	}, "\n"), turn)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "parse active coordinator line") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseActionJSONDone(t *testing.T) {
	a, err := parseActionJSON(`{"action":"done","summary":"ok","changes":true,"pr_title":"Title","pr_body":"Body"}`)
	if err != nil {
		t.Fatalf("parseActionJSON returned error: %v", err)
	}
	if a.Type != ActionDone {
		t.Fatalf("got type %q, want %q", a.Type, ActionDone)
	}
	if !a.Changes {
		t.Fatalf("expected changes=true")
	}
	if a.PRTitle != "Title" || a.PRBody != "Body" {
		t.Fatalf("unexpected metadata: title=%q body=%q", a.PRTitle, a.PRBody)
	}
}

func TestParseActionJSONReplyRequiresCommentID(t *testing.T) {
	_, err := parseActionJSON(`{"action":"reply","body":"hello"}`)
	if err == nil {
		t.Fatalf("expected error for missing comment_id")
	}
}

func TestParseActionJSONIssueReport(t *testing.T) {
	a, err := parseActionJSON(`{"action":"issue_report","issue_number":42,"relevant":true,"analysis":"Looks valid","needs_task":true,"task_title":"Add guard","task_body":"Implement checks"}`)
	if err != nil {
		t.Fatalf("parseActionJSON returned error: %v", err)
	}
	if a.Type != ActionIssueReport {
		t.Fatalf("got type %q, want %q", a.Type, ActionIssueReport)
	}
	if a.IssueNumber != 42 || !a.Relevant || !a.NeedsTask {
		t.Fatalf("unexpected issue_report fields: %#v", a)
	}
	if a.TaskTitle != "Add guard" || a.TaskBody != "Implement checks" {
		t.Fatalf("unexpected issue task metadata: %#v", a)
	}
}

func TestParseActionJSONIssueReportRequiresIssueNumber(t *testing.T) {
	_, err := parseActionJSON(`{"action":"issue_report","relevant":true,"analysis":"ok","needs_task":false}`)
	if err == nil {
		t.Fatalf("expected error for missing issue_number")
	}
}

func TestParseActionJSONIssueUpdate(t *testing.T) {
	a, err := parseActionJSON(`{"action":"issue_update","issue_number":42,"relation":"fixes","comment":"Implemented with tests"}`)
	if err != nil {
		t.Fatalf("parseActionJSON returned error: %v", err)
	}
	if a.Type != ActionIssueUpdate {
		t.Fatalf("got type %q, want %q", a.Type, ActionIssueUpdate)
	}
	if a.IssueNumber != 42 || a.Relation != IssueRelationFixes || a.CommentBody != "Implemented with tests" {
		t.Fatalf("unexpected issue_update fields: %#v", a)
	}
}

func TestParseActionJSONIssueUpdateRejectsInvalidRelation(t *testing.T) {
	_, err := parseActionJSON(`{"action":"issue_update","issue_number":42,"relation":"unknown","comment":"x"}`)
	if err == nil {
		t.Fatalf("expected error for invalid relation")
	}
}

func TestRawOutputFromErrorReturnsRunnerOutput(t *testing.T) {
	r := Runner{Command: `printf 'oops\n'`}
	_, err := r.Run(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error")
	}

	raw := RawOutputFromError(err)
	if strings.TrimSpace(raw) != "oops" {
		t.Fatalf("raw output = %q, want %q", strings.TrimSpace(raw), "oops")
	}
}

func TestRawOutputFromErrorReturnsEmptyForNonRunError(t *testing.T) {
	if got := RawOutputFromError(errors.New("boom")); got != "" {
		t.Fatalf("expected empty output for non-run error, got %q", got)
	}
}

func TestRunnerRunCapturesManagerAndQuarantinedLines(t *testing.T) {
	r := Runner{Command: `printf 'SIMUG_MANAGER: hi manager\nfree text\nSIMUG: {"action":"done","summary":"ok","changes":false}\n'`}
	result, err := r.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(result.ManagerMessages) != 1 || result.ManagerMessages[0] != "hi manager" {
		t.Fatalf("unexpected manager messages: %#v", result.ManagerMessages)
	}
	if len(result.QuarantinedLines) != 1 || result.QuarantinedLines[0] != "free text" {
		t.Fatalf("unexpected quarantined lines: %#v", result.QuarantinedLines)
	}
	if result.Terminal.Type != ActionDone {
		t.Fatalf("terminal=%q, want %q", result.Terminal.Type, ActionDone)
	}
}

func TestRunnerRunStreamsLinesDuringExecution(t *testing.T) {
	var got []StreamLine
	r := Runner{
		Command: `printf 'SIMUG_MANAGER: hi manager\nfree text\nSIMUG: {"action":"done","summary":"ok","changes":false}\n'`,
		OnLine: func(line StreamLine) {
			got = append(got, line)
		},
	}

	result, err := r.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if result.Terminal.Type != ActionDone {
		t.Fatalf("terminal=%q, want %q", result.Terminal.Type, ActionDone)
	}
	if len(got) != 3 {
		t.Fatalf("streamed lines=%d, want 3", len(got))
	}
	if got[0].Kind != StreamKindManager || got[0].Line != "SIMUG_MANAGER: hi manager" {
		t.Fatalf("unexpected first streamed line: %#v", got[0])
	}
	if got[1].Kind != StreamKindDiagnostic || got[1].Line != "free text" {
		t.Fatalf("unexpected second streamed line: %#v", got[1])
	}
	if got[2].Kind != StreamKindProtocol || !strings.Contains(got[2].Line, `"action":"done"`) {
		t.Fatalf("unexpected third streamed line: %#v", got[2])
	}
}

func TestClassifyStreamLine(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantKind StreamKind
		wantLine string
		wantOK   bool
	}{
		{name: "protocol", raw: "SIMUG: {\"action\":\"done\"}\n", wantKind: StreamKindProtocol, wantLine: "SIMUG: {\"action\":\"done\"}", wantOK: true},
		{name: "manager", raw: "SIMUG_MANAGER: hi\n", wantKind: StreamKindManager, wantLine: "SIMUG_MANAGER: hi", wantOK: true},
		{name: "diagnostic", raw: "thinking...\n", wantKind: StreamKindDiagnostic, wantLine: "thinking...", wantOK: true},
		{name: "blank", raw: "\n", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := classifyStreamLine([]byte(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Kind != tc.wantKind || got.Line != tc.wantLine {
				t.Fatalf("got=%#v, want kind=%q line=%q", got, tc.wantKind, tc.wantLine)
			}
		})
	}
}

func TestStreamBufferFlushEmitsPartialLine(t *testing.T) {
	var got bytes.Buffer
	buffer := newStreamBuffer(func(line StreamLine) {
		got.WriteString(line.Line)
	})
	if _, err := buffer.Write([]byte("SIMUG_MANAGER: partial")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	buffer.Flush()
	if got.String() != "SIMUG_MANAGER: partial" {
		t.Fatalf("flushed=%q, want %q", got.String(), "SIMUG_MANAGER: partial")
	}
}

func TestRunnerRunCollapsesDuplicatedTerminalSequenceFromTranscript(t *testing.T) {
	r := Runner{Command: `printf '%s\n' \
'OpenAI Codex v0.111.0' \
'codex' \
'SIMUG: {"action":"comment","body":"same"}' \
'SIMUG: {"action":"done","summary":"ok","changes":false}' \
'tokens used' \
'2,385' \
'SIMUG: {"action":"comment","body":"same"}' \
'SIMUG: {"action":"done","summary":"ok","changes":false}'`}

	result, err := r.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("actions=%d, want 2 after collapse", len(result.Actions))
	}
	if result.Terminal.Type != ActionDone {
		t.Fatalf("terminal=%q, want %q", result.Terminal.Type, ActionDone)
	}
}

func TestRunnerRunKeepsDistinctMultipleTerminalResponsesAsError(t *testing.T) {
	r := Runner{Command: `printf '%s\n' \
'SIMUG: {"action":"comment","body":"first"}' \
'SIMUG: {"action":"done","summary":"ok","changes":false}' \
'SIMUG: {"action":"comment","body":"second"}' \
'SIMUG: {"action":"done","summary":"ok","changes":false}'`}

	_, err := r.Run(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for distinct multiple terminal responses")
	}
	if !strings.Contains(err.Error(), "exactly one terminal action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunnerRunAcceptsValidProtocolDespiteNonZeroExit(t *testing.T) {
	r := Runner{Command: `printf 'SIMUG: {"action":"done","summary":"ok","changes":false}\n'; exit 1`}
	result, err := r.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Terminal.Type != ActionDone {
		t.Fatalf("terminal=%q, want %q", result.Terminal.Type, ActionDone)
	}
}

func TestRunnerRunIgnoresEchoedTemplateProtocolBlock(t *testing.T) {
	r := Runner{Command: `printf '%s\n' \
'SIMUG: {"action":"comment","body":"..."}' \
'SIMUG: {"action":"done","summary":"...","changes":true,"pr_title":"optional","pr_body":"optional"}' \
'SIMUG: {"action":"idle","reason":"..."}' \
'SIMUG: {"action":"comment","body":"final update"}' \
'SIMUG: {"action":"done","summary":"real done","changes":false}'`}

	result, err := r.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("actions=%d, want 2 after filtering echoed template block", len(result.Actions))
	}
	if result.Terminal.Type != ActionDone || result.Terminal.Summary != "real done" {
		t.Fatalf("unexpected terminal: %#v", result.Terminal)
	}
}
