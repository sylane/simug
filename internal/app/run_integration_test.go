package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"simug/internal/git"
	"simug/internal/github"
)

type mockCommandRunner struct {
	responses map[string]string
	errors    map[string]error
}

func (m mockCommandRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	key := commandKey(name, args...)
	if err, ok := m.errors[key]; ok {
		return "", err
	}
	if out, ok := m.responses[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func commandKey(name string, args ...string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func TestRunFailsWhenMultipleAuthoredOpenPRsExist(t *testing.T) {
	tmp := t.TempDir()
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[
  {"number":1,"title":"A","state":"OPEN","headRefName":"agent/20260307-120000-alpha-task","headRefOid":"111","baseRefName":"main","author":{"login":"alice"},"mergedAt":null},
  {"number":2,"title":"B","state":"OPEN","headRefName":"agent/20260307-120500-beta-task","headRefOid":"222","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}
]`,
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Run(ctx, tmp)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "expected at most one managed PR") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFailsOnCheckoutMismatchForManagedPR(t *testing.T) {
	tmp := t.TempDir()
	branch := "agent/20260307-120000-alpha-task"
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[
  {"number":42,"title":"A","state":"OPEN","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}
]`,
		commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `{"number":42,"title":"A","state":"OPEN","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}`,
		commandKey("git", "fetch", "--prune", "origin"):        "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): "main\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Run(ctx, tmp)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "checkout mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAcquireLockRecoversStaleLock(t *testing.T) {
	tmp := t.TempDir()
	lockDir := filepath.Join(tmp, ".simug")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	// Very high pid is expected to be absent on normal development hosts.
	if err := os.WriteFile(filepath.Join(lockDir, "lock"), []byte("pid=999999\n"), 0o644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	unlock, err := acquireLock(tmp)
	if err != nil {
		t.Fatalf("acquireLock should recover stale lock, got error: %v", err)
	}
	defer unlock()
}

func TestRunOneManagedTickSuccessWithMockedCommands(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", `printf 'SIMUG: {"action":"done","summary":"ok","changes":false}\n'`)

	tmp := t.TempDir()
	branch := "agent/20260307-120000-alpha-task"
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[
  {"number":42,"title":"A","state":"OPEN","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}
]`,
		commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `{"number":42,"title":"A","state":"OPEN","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}`,
		commandKey("git", "fetch", "--prune", "origin"):                                            "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                     branch + "\n",
		commandKey("git", "status", "--porcelain"):                                                 "\n",
		commandKey("git", "rev-parse", "HEAD"):                                                     "abcdef\n",
		commandKey("git", "rev-parse", "origin/"+branch):                                           "abcdef\n",
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"/agent do status","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := Run(ctx, tmp); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRunWritesHighFidelityTraceEvents(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", `printf 'SIMUG: {"action":"done","summary":"ok","changes":false}\n'`)

	tmp := t.TempDir()
	branch := "agent/20260307-120000-alpha-task"
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[
  {"number":42,"title":"A","state":"OPEN","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}
]`,
		commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `{"number":42,"title":"A","state":"OPEN","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}`,
		commandKey("git", "fetch", "--prune", "origin"):                                            "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                     branch + "\n",
		commandKey("git", "status", "--porcelain"):                                                 "\n",
		commandKey("git", "rev-parse", "HEAD"):                                                     "abcdef\n",
		commandKey("git", "rev-parse", "origin/"+branch):                                           "abcdef\n",
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"hello","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := Run(ctx, tmp); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".simug", "events.log"))
	if err != nil {
		t.Fatalf("read events log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatalf("expected non-empty events log")
	}

	foundCommandTrace := false
	foundInvariantDecision := false
	for _, line := range lines {
		var entry struct {
			Kind   string         `json:"kind"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode event log entry: %v", err)
		}

		if entry.Kind == "command_trace" {
			foundCommandTrace = true
			required := []string{"run_id", "tick_seq", "command_seq", "component", "name", "args", "duration_ms", "exit_code", "stdout_tail", "stderr_tail"}
			for _, key := range required {
				if _, ok := entry.Fields[key]; !ok {
					t.Fatalf("command trace missing field %q: %#v", key, entry.Fields)
				}
			}
		}

		if entry.Kind == "invariant_decision" {
			foundInvariantDecision = true
			if _, ok := entry.Fields["run_id"]; !ok {
				t.Fatalf("invariant decision missing run_id: %#v", entry.Fields)
			}
			if _, ok := entry.Fields["tick_seq"]; !ok {
				t.Fatalf("invariant decision missing tick_seq: %#v", entry.Fields)
			}
		}
	}

	if !foundCommandTrace {
		t.Fatalf("expected at least one command_trace event")
	}
	if !foundInvariantDecision {
		t.Fatalf("expected at least one invariant_decision event")
	}
}
