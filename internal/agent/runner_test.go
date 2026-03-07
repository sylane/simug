package agent

import (
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
