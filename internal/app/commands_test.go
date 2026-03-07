package app

import (
	"reflect"
	"testing"
)

func TestParseAgentCommandsAuthorizationAndVerbFiltering(t *testing.T) {
	allowedUsers := map[string]struct{}{"alice": {}}
	allowedVerbs := map[string]struct{}{"do": {}, "retry": {}}

	body := "/agent do ship-it\n/agent unknown thing\nhello\n/agent retry\n"
	commands, ignored := parseAgentCommands(body, "alice", allowedUsers, allowedVerbs)

	wantCommands := []string{"/agent do ship-it", "/agent retry"}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands mismatch\n got: %#v\nwant: %#v", commands, wantCommands)
	}
	if len(ignored) != 1 {
		t.Fatalf("expected one ignored command, got %#v", ignored)
	}
}

func TestParseAgentCommandsRejectsUnauthorizedAuthor(t *testing.T) {
	allowedUsers := map[string]struct{}{"alice": {}}
	allowedVerbs := map[string]struct{}{"do": {}}

	commands, ignored := parseAgentCommands("/agent do test\n", "mallory", allowedUsers, allowedVerbs)
	if len(commands) != 0 {
		t.Fatalf("expected no accepted commands, got %#v", commands)
	}
	if len(ignored) != 1 {
		t.Fatalf("expected one ignored command, got %#v", ignored)
	}
}
