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

	"simug/internal/agent"
	"simug/internal/git"
	"simug/internal/github"
	"simug/internal/state"
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

type sequencedCommandRunner struct {
	responses map[string][]string
	errors    map[string][]error
	counts    map[string]int
}

func (m *sequencedCommandRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	key := commandKey(name, args...)
	if m.counts == nil {
		m.counts = make(map[string]int)
	}
	idx := m.counts[key]
	m.counts[key] = idx + 1

	if errs, ok := m.errors[key]; ok {
		if idx < len(errs) && errs[idx] != nil {
			return "", errs[idx]
		}
	}
	if outs, ok := m.responses[key]; ok {
		if idx < len(outs) {
			return outs[idx], nil
		}
		return "", fmt.Errorf("unexpected extra command invocation: %s (call %d)", key, idx+1)
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func TestRunFailsWhenMultipleAuthoredOpenPRsExist(t *testing.T) {
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"noop"}`))
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
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"noop"}`))
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
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`))

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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.Mode != "managed_pr" {
		t.Fatalf("mode=%q, want managed_pr", st.Mode)
	}
}

func TestRunOneManagedTickAcceptsIssueUpdateIntent(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(
		`{"action":"issue_update","issue_number":7,"relation":"fixes","comment":"Implemented via this PR"}`,
		`{"action":"issue_update","issue_number":7,"relation":"fixes","comment":"Implemented via this PR"}`,
		`{"action":"done","summary":"ok","changes":false}`,
	))

	tmp := t.TempDir()
	branch := "agent/20260307-120000-alpha-task"
	issueAction := agent.Action{
		Type:        agent.ActionIssueUpdate,
		IssueNumber: 7,
		Relation:    agent.IssueRelationFixes,
		CommentBody: "Implemented via this PR",
	}
	key := issueUpdateIdempotencyKey(42, issueAction)
	linkForComment := state.IssueLink{
		PRNumber:       42,
		IssueNumber:    7,
		Relation:       "fixes",
		CommentBody:    "Implemented via this PR",
		IdempotencyKey: key,
	}
	expectedIssueComment := buildIssueUpdateCommentBody("example/simug", linkForComment, "")
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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"please handle issue linkage","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                    `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
		commandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "issue", "comment", "7", "--body", expectedIssueComment):                  "",
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		IssueLinks []struct {
			PRNumber       int    `json:"pr_number"`
			IssueNumber    int    `json:"issue_number"`
			Relation       string `json:"relation"`
			CommentBody    string `json:"comment_body"`
			IdempotencyKey string `json:"idempotency_key"`
			CommentPosted  bool   `json:"comment_posted"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if len(st.IssueLinks) != 1 {
		t.Fatalf("issue_links len=%d, want 1", len(st.IssueLinks))
	}
	link := st.IssueLinks[0]
	if link.PRNumber != 42 || link.IssueNumber != 7 || link.Relation != "fixes" {
		t.Fatalf("unexpected issue link: %#v", link)
	}
	if strings.TrimSpace(link.CommentBody) == "" || strings.TrimSpace(link.IdempotencyKey) == "" {
		t.Fatalf("expected comment_body and idempotency_key to be populated: %#v", link)
	}
	if !link.CommentPosted {
		t.Fatalf("expected issue link to be marked comment_posted: %#v", link)
	}
}

func TestRunManagedTickMarksIssueUpdatePostedWhenMarkerAlreadyExists(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "active_pr": 42,
  "active_branch": "agent/20260307-120000-alpha-task",
  "mode": "managed_pr",
  "issue_links": [
    {
      "pr_number": 42,
      "issue_number": 7,
      "relation": "fixes",
      "comment_body": "Implemented via this PR",
      "provenance": "run=abc tick=1",
      "idempotency_key": "abc123",
      "recorded_at": "2026-03-08T00:49:00Z",
      "comment_posted": false,
      "finalized": false
    }
  ],
  "updated_at": "2026-03-08T00:49:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	branch := "agent/20260307-120000-alpha-task"
	link := state.IssueLink{
		PRNumber:       42,
		IssueNumber:    7,
		Relation:       "fixes",
		CommentBody:    "Implemented via this PR",
		IdempotencyKey: "abc123",
	}
	marker := issueUpdateMarker(link)
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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"tick","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                    `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
		commandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"): `[[` +
			`{"id":9001,"body":"` + marker + `","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}` +
			`]]`,
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		IssueLinks []struct {
			CommentPosted bool `json:"comment_posted"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if len(st.IssueLinks) != 1 || !st.IssueLinks[0].CommentPosted {
		t.Fatalf("expected issue link marked as comment_posted, got %#v", st.IssueLinks)
	}
}

func TestRunManagedTickSkipsIssueUpdateOutsideAuthorScope(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "active_pr": 42,
  "active_branch": "agent/20260307-120000-alpha-task",
  "mode": "managed_pr",
  "issue_links": [
    {
      "pr_number": 42,
      "issue_number": 7,
      "relation": "impacts",
      "comment_body": "Could impact this issue",
      "provenance": "run=abc tick=1",
      "idempotency_key": "scope-check",
      "recorded_at": "2026-03-08T00:49:00Z",
      "comment_posted": false,
      "finalized": false
    }
  ],
  "updated_at": "2026-03-08T00:49:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"tick","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                    `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"mallory"}}`,
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		IssueLinks []struct {
			CommentPosted bool `json:"comment_posted"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if len(st.IssueLinks) != 1 || st.IssueLinks[0].CommentPosted {
		t.Fatalf("expected issue link to remain unposted for out-of-scope issue, got %#v", st.IssueLinks)
	}
}

func TestRunNoOpenPRFinalizesMergedPRIssueLinks(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "active_pr": 42,
  "active_branch": "agent/20260307-120000-alpha-task",
  "mode": "managed_pr",
  "issue_links": [
    {
      "pr_number": 42,
      "issue_number": 7,
      "relation": "fixes",
      "comment_body": "Resolved by merge",
      "provenance": "run=abc tick=1",
      "idempotency_key": "fix-key",
      "recorded_at": "2026-03-08T00:49:00Z",
      "comment_posted": true,
      "finalized": false
    },
    {
      "pr_number": 42,
      "issue_number": 8,
      "relation": "relates",
      "comment_body": "Related context",
      "provenance": "run=abc tick=1",
      "idempotency_key": "rel-key",
      "recorded_at": "2026-03-08T00:49:00Z",
      "comment_posted": true,
      "finalized": false
    }
  ],
  "updated_at": "2026-03-08T00:49:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	fixesLink := state.IssueLink{
		PRNumber:       42,
		IssueNumber:    7,
		Relation:       "fixes",
		CommentBody:    "Resolved by merge",
		IdempotencyKey: "fix-key",
	}
	relatesLink := state.IssueLink{
		PRNumber:       42,
		IssueNumber:    8,
		Relation:       "relates",
		CommentBody:    "Related context",
		IdempotencyKey: "rel-key",
	}
	fixesComment := buildIssueFinalizationCommentBody("example/simug", fixesLink)
	relatesComment := buildIssueFinalizationCommentBody("example/simug", relatesLink)

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"):                                   `{"number":42,"title":"A","state":"MERGED","headRefName":"agent/20260307-120000-alpha-task","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":"2026-03-08T00:50:00Z"}`,
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                                                                                   `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
		commandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"):                                                                 "[]",
		commandKey("gh", "issue", "comment", "7", "--body", fixesComment):                                                                                         "",
		commandKey("gh", "api", "repos/example/simug/issues/7", "--method", "PATCH", "-f", "state=closed"):                                                        "",
		commandKey("gh", "api", "repos/example/simug/issues/8"):                                                                                                   `{"number":8,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
		commandKey("gh", "api", "repos/example/simug/issues/8/comments", "--paginate", "--slurp"):                                                                 "[]",
		commandKey("gh", "issue", "comment", "8", "--body", relatesComment):                                                                                       "",
		commandKey("git", "status", "--porcelain"):                                                                                                                "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                                                                                           "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                                                                                    "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                                                                            "0 0\n",
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"):                                                   `[]`,
		commandKey("git", "rev-parse", "HEAD"):                                                                                                                    "abcdef\n",
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		Mode       string `json:"mode"`
		ActivePR   int    `json:"active_pr"`
		IssueLinks []struct {
			IssueNumber int  `json:"issue_number"`
			Finalized   bool `json:"finalized"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.Mode != "issue_triage" {
		t.Fatalf("mode=%q, want issue_triage", st.Mode)
	}
	if st.ActivePR != 0 {
		t.Fatalf("active_pr=%d, want 0", st.ActivePR)
	}
	if len(st.IssueLinks) != 2 || !st.IssueLinks[0].Finalized || !st.IssueLinks[1].Finalized {
		t.Fatalf("expected merged PR issue links finalized, got %#v", st.IssueLinks)
	}
}

func TestRunNoOpenPRMergeFinalizationMarkerSkipsDuplicateCommentButStillCloses(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "active_pr": 42,
  "active_branch": "agent/20260307-120000-alpha-task",
  "mode": "managed_pr",
  "issue_links": [
    {
      "pr_number": 42,
      "issue_number": 7,
      "relation": "fixes",
      "comment_body": "Resolved by merge",
      "provenance": "run=abc tick=1",
      "idempotency_key": "fix-key",
      "recorded_at": "2026-03-08T00:49:00Z",
      "comment_posted": true,
      "finalized": false
    }
  ],
  "updated_at": "2026-03-08T00:49:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	link := state.IssueLink{
		PRNumber:       42,
		IssueNumber:    7,
		Relation:       "fixes",
		CommentBody:    "Resolved by merge",
		IdempotencyKey: "fix-key",
	}
	marker := issueFinalizationMarker(link)
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"):                                   `{"number":42,"title":"A","state":"MERGED","headRefName":"agent/20260307-120000-alpha-task","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":"2026-03-08T00:50:00Z"}`,
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                                                                                   `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
		commandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"): `[[` +
			`{"id":9001,"body":"` + marker + `","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}` +
			`]]`,
		commandKey("gh", "api", "repos/example/simug/issues/7", "--method", "PATCH", "-f", "state=closed"):      "",
		commandKey("git", "status", "--porcelain"):                                                              "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                                         "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                                  "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                          "0 0\n",
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[]`,
		commandKey("git", "rev-parse", "HEAD"):                                                                  "abcdef\n",
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		IssueLinks []struct {
			Finalized bool `json:"finalized"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if len(st.IssueLinks) != 1 || !st.IssueLinks[0].Finalized {
		t.Fatalf("expected merged PR issue link finalized, got %#v", st.IssueLinks)
	}
}

func TestRunNoOpenPRMergeFinalizationSkipsOutOfScopeIssue(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "active_pr": 42,
  "active_branch": "agent/20260307-120000-alpha-task",
  "mode": "managed_pr",
  "issue_links": [
    {
      "pr_number": 42,
      "issue_number": 7,
      "relation": "fixes",
      "comment_body": "Resolved by merge",
      "provenance": "run=abc tick=1",
      "idempotency_key": "fix-key",
      "recorded_at": "2026-03-08T00:49:00Z",
      "comment_posted": true,
      "finalized": false
    }
  ],
  "updated_at": "2026-03-08T00:49:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"):                                   `{"number":42,"title":"A","state":"MERGED","headRefName":"agent/20260307-120000-alpha-task","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":"2026-03-08T00:50:00Z"}`,
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                                                                                   `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"mallory"}}`,
		commandKey("git", "status", "--porcelain"):                                                                                                                "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                                                                                           "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                                                                                    "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                                                                            "0 0\n",
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"):                                                   `[]`,
		commandKey("git", "rev-parse", "HEAD"):                                                                                                                    "abcdef\n",
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		IssueLinks []struct {
			Finalized bool `json:"finalized"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if len(st.IssueLinks) != 1 || !st.IssueLinks[0].Finalized {
		t.Fatalf("expected out-of-scope issue link finalized without mutation, got %#v", st.IssueLinks)
	}
}

func TestRunManagedTickRejectsMalformedIssueUpdatePayload(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_MAX_REPAIR_ATTEMPTS", "0")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(
		`{"action":"issue_update","issue_number":7,"relation":"bogus","comment":"bad relation"}`,
		`{"action":"done","summary":"ok","changes":false}`,
	))

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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"tick","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := Run(ctx, tmp)
	if err == nil {
		t.Fatalf("expected malformed issue_update validation error, got nil")
	}
	if !strings.Contains(err.Error(), `issue_update action invalid relation "bogus"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOnceIssueLifecyclePersistsAcrossRestartUntilMergeFinalization(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")

	tmp := t.TempDir()
	branch := "agent/20260307-120000-alpha-task"
	issueAction := agent.Action{
		Type:        agent.ActionIssueUpdate,
		IssueNumber: 7,
		Relation:    agent.IssueRelationFixes,
		CommentBody: "Implemented via this PR",
	}
	key := issueUpdateIdempotencyKey(42, issueAction)
	trackedLink := state.IssueLink{
		PRNumber:       42,
		IssueNumber:    7,
		Relation:       "fixes",
		CommentBody:    "Implemented via this PR",
		IdempotencyKey: key,
	}
	updateBody := buildIssueUpdateCommentBody("example/simug", trackedLink, "")
	finalizationBody := buildIssueFinalizationCommentBody("example/simug", trackedLink)

	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(
		`{"action":"issue_update","issue_number":7,"relation":"fixes","comment":"Implemented via this PR"}`,
		`{"action":"done","summary":"ok","changes":false}`,
	))
	firstRunner := mockCommandRunner{responses: map[string]string{
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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"tick","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                    `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
		commandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "issue", "comment", "7", "--body", updateBody):                            "",
	}}

	restoreGit := git.SetCommandRunnerForTest(firstRunner)
	restoreGitHub := github.SetCommandRunnerForTest(firstRunner)
	if err := RunOnce(context.Background(), tmp); err != nil {
		restoreGit()
		restoreGitHub()
		t.Fatalf("RunOnce first lifecycle tick returned error: %v", err)
	}
	restoreGit()
	restoreGitHub()

	stateDataAfterFirst, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file after first tick: %v", err)
	}
	var afterFirst struct {
		Mode       string `json:"mode"`
		ActivePR   int    `json:"active_pr"`
		IssueLinks []struct {
			IssueNumber   int  `json:"issue_number"`
			CommentPosted bool `json:"comment_posted"`
			Finalized     bool `json:"finalized"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateDataAfterFirst, &afterFirst); err != nil {
		t.Fatalf("decode state after first tick: %v", err)
	}
	if afterFirst.Mode != "managed_pr" || afterFirst.ActivePR != 42 {
		t.Fatalf("unexpected first-tick managed state: %#v", afterFirst)
	}
	if len(afterFirst.IssueLinks) != 1 || !afterFirst.IssueLinks[0].CommentPosted || afterFirst.IssueLinks[0].Finalized {
		t.Fatalf("unexpected issue link state after first tick: %#v", afterFirst.IssueLinks)
	}

	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))
	secondRunner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"):                                   `{"number":42,"title":"A","state":"MERGED","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":"2026-03-08T00:50:00Z"}`,
		commandKey("gh", "api", "repos/example/simug/issues/7"):                                                                                                   `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
		commandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"):                                                                 "[]",
		commandKey("gh", "issue", "comment", "7", "--body", finalizationBody):                                                                                     "",
		commandKey("gh", "api", "repos/example/simug/issues/7", "--method", "PATCH", "-f", "state=closed"):                                                        "",
		commandKey("git", "status", "--porcelain"):                                                                                                                "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                                                                                           "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                                                                                    "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                                                                            "0 0\n",
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"):                                                   `[]`,
		commandKey("git", "rev-parse", "HEAD"):                                                                                                                    "abcdef\n",
	}}

	restoreGit = git.SetCommandRunnerForTest(secondRunner)
	restoreGitHub = github.SetCommandRunnerForTest(secondRunner)
	if err := RunOnce(context.Background(), tmp); err != nil {
		restoreGit()
		restoreGitHub()
		t.Fatalf("RunOnce second lifecycle tick returned error: %v", err)
	}
	restoreGit()
	restoreGitHub()

	stateDataAfterSecond, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file after second tick: %v", err)
	}
	var afterSecond struct {
		Mode       string `json:"mode"`
		ActivePR   int    `json:"active_pr"`
		IssueLinks []struct {
			IssueNumber int  `json:"issue_number"`
			Finalized   bool `json:"finalized"`
		} `json:"issue_links"`
	}
	if err := json.Unmarshal(stateDataAfterSecond, &afterSecond); err != nil {
		t.Fatalf("decode state after second tick: %v", err)
	}
	if afterSecond.Mode != "issue_triage" || afterSecond.ActivePR != 0 {
		t.Fatalf("unexpected second-tick merged transition state: %#v", afterSecond)
	}
	if len(afterSecond.IssueLinks) != 1 || afterSecond.IssueLinks[0].IssueNumber != 7 || !afterSecond.IssueLinks[0].Finalized {
		t.Fatalf("unexpected issue link state after second tick: %#v", afterSecond.IssueLinks)
	}
}

func TestRunOnceMergedPRChecksOutMainPullsAndDeletesMergedLocalBranch(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	branch := "agent/20260307-120000-alpha-task"
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "managed_pr",
  "active_pr": 42,
  "active_branch": "` + branch + `",
  "updated_at": "2026-03-08T01:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := &sequencedCommandRunner{
		responses: map[string][]string{
			commandKey("git", "rev-parse", "--show-toplevel"): {tmp + "\n"},
			commandKey("git", "remote", "get-url", "origin"):  {"https://github.com/example/simug.git\n"},
			commandKey("gh", "api", "user", "--jq", ".login"): {"alice\n"},
			commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): {`[]`},
			commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"):                                   {`{"number":42,"title":"A","state":"MERGED","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":"2026-03-08T00:50:00Z"}`},
			commandKey("git", "status", "--porcelain"):                                                              {"\n", "\n"},
			commandKey("git", "fetch", "--prune", "origin"):                                                         {"", ""},
			commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                                  {branch + "\n", "main\n"},
			commandKey("git", "merge-base", "--is-ancestor", "HEAD", "origin/main"):                                 {""},
			commandKey("git", "checkout", "main"):                                                                   {""},
			commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                          {"0 1\n"},
			commandKey("git", "pull", "--ff-only", "origin", "main"):                                                {""},
			commandKey("git", "branch", "-d", branch):                                                               {""},
			commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): {`[]`},
			commandKey("git", "rev-parse", "HEAD"): {`abcdef
`, `abcdef
`},
		},
	}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	if runner.counts[commandKey("git", "checkout", "main")] != 1 {
		t.Fatalf("expected checkout main once, got %d", runner.counts[commandKey("git", "checkout", "main")])
	}
	if runner.counts[commandKey("git", "pull", "--ff-only", "origin", "main")] != 1 {
		t.Fatalf("expected pull --ff-only once, got %d", runner.counts[commandKey("git", "pull", "--ff-only", "origin", "main")])
	}
	if runner.counts[commandKey("git", "branch", "-d", branch)] != 1 {
		t.Fatalf("expected merged branch deletion once, got %d", runner.counts[commandKey("git", "branch", "-d", branch)])
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		Mode         string `json:"mode"`
		ActivePR     int    `json:"active_pr"`
		ActiveBranch string `json:"active_branch"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.ActivePR != 0 || st.ActiveBranch != "" {
		t.Fatalf("expected cleared managed PR state, got %#v", st)
	}
	if st.Mode != "issue_triage" {
		t.Fatalf("mode=%q, want issue_triage", st.Mode)
	}
}

func TestRunOnceMergedRebasedPRChecksOutMainPullsAndDeletesManagedLocalBranch(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	branch := "agent/20260307-120000-alpha-task"
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "managed_pr",
  "active_pr": 42,
  "active_branch": "` + branch + `",
  "updated_at": "2026-03-08T01:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := &sequencedCommandRunner{
		responses: map[string][]string{
			commandKey("git", "rev-parse", "--show-toplevel"): {tmp + "\n"},
			commandKey("git", "remote", "get-url", "origin"):  {"https://github.com/example/simug.git\n"},
			commandKey("gh", "api", "user", "--jq", ".login"): {"alice\n"},
			commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): {`[]`},
			commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"):                                   {`{"number":42,"title":"A","state":"MERGED","headRefName":"` + branch + `","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":"2026-03-08T00:50:00Z"}`},
			commandKey("git", "status", "--porcelain"):                                                              {"\n", "\n"},
			commandKey("git", "fetch", "--prune", "origin"):                                                         {"", ""},
			commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                                  {branch + "\n", "main\n"},
			commandKey("git", "checkout", "main"):                                                                   {""},
			commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                          {"0 1\n"},
			commandKey("git", "pull", "--ff-only", "origin", "main"):                                                {""},
			commandKey("git", "branch", "-d", branch):                                                               {""},
			commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): {`[]`},
			commandKey("git", "rev-parse", "HEAD"): {`abcdef
`, `abcdef
`},
		},
	}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	if runner.counts[commandKey("git", "checkout", "main")] != 1 {
		t.Fatalf("expected checkout main once, got %d", runner.counts[commandKey("git", "checkout", "main")])
	}
	if runner.counts[commandKey("git", "pull", "--ff-only", "origin", "main")] != 1 {
		t.Fatalf("expected pull --ff-only once, got %d", runner.counts[commandKey("git", "pull", "--ff-only", "origin", "main")])
	}
	if runner.counts[commandKey("git", "branch", "-d", branch)] != 1 {
		t.Fatalf("expected merged branch deletion once, got %d", runner.counts[commandKey("git", "branch", "-d", branch)])
	}
	if runner.counts[commandKey("git", "merge-base", "--is-ancestor", "HEAD", "origin/main")] != 0 {
		t.Fatalf("expected merge-base ancestry check to be skipped for GitHub-confirmed merged branch, got %d", runner.counts[commandKey("git", "merge-base", "--is-ancestor", "HEAD", "origin/main")])
	}
}

func TestRunWritesHighFidelityTraceEvents(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`))

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

func TestRunArchivesCodexPromptAndOutput(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`))

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

	var archiveFields map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry struct {
			Kind   string         `json:"kind"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode event log entry: %v", err)
		}
		if entry.Kind == "agent_archive" {
			archiveFields = entry.Fields
		}
	}
	if archiveFields == nil {
		t.Fatalf("expected agent_archive event")
	}

	promptPath, _ := archiveFields["prompt_path"].(string)
	outputPath, _ := archiveFields["output_path"].(string)
	metadataPath, _ := archiveFields["metadata_path"].(string)
	transcriptPath, _ := archiveFields["transcript_path"].(string)
	if promptPath == "" || outputPath == "" || metadataPath == "" || transcriptPath == "" {
		t.Fatalf("archive event missing paths: %#v", archiveFields)
	}

	promptData, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt archive: %v", err)
	}
	if !strings.Contains(string(promptData), "You are Codex running under simug orchestration.") {
		t.Fatalf("unexpected prompt archive content: %s", string(promptData))
	}

	outputData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output archive: %v", err)
	}
	if !strings.Contains(string(outputData), `"payload":{"action":"done","summary":"ok","changes":false}`) {
		t.Fatalf("unexpected output archive content: %s", string(outputData))
	}

	transcriptData, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("read transcript archive: %v", err)
	}
	if !strings.Contains(string(transcriptData), " simug[prompt] You are Codex running under simug orchestration.") {
		t.Fatalf("unexpected transcript archive content: %s", string(transcriptData))
	}
	if !strings.Contains(string(transcriptData), ` codex[protocol] SIMUG: {"envelope":"coordinator","event":"action"`) {
		t.Fatalf("transcript missing protocol classification: %s", string(transcriptData))
	}

	var meta struct {
		RunID                  string   `json:"run_id"`
		TickSeq                int64    `json:"tick_seq"`
		Attempt                int      `json:"attempt"`
		ExpectedBranch         string   `json:"expected_branch"`
		ProtocolActionCount    int      `json:"protocol_action_count"`
		ProtocolTerminalCount  int      `json:"protocol_terminal_count"`
		ProtocolActionsExcerpt []string `json:"protocol_actions_excerpt"`
		ProtocolActiveTurnID   string   `json:"protocol_active_turn_id"`
		ProtocolActiveLines    []string `json:"protocol_active_lines"`
	}
	metaData, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata archive: %v", err)
	}
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("decode metadata archive: %v", err)
	}
	if meta.RunID == "" || meta.TickSeq <= 0 || meta.Attempt != 1 {
		t.Fatalf("unexpected archive metadata: %#v", meta)
	}
	if meta.ExpectedBranch != branch {
		t.Fatalf("metadata expected branch=%q, want %q", meta.ExpectedBranch, branch)
	}
	if meta.ProtocolActionCount == 0 || meta.ProtocolTerminalCount == 0 {
		t.Fatalf("metadata protocol diagnostics missing counts: %#v", meta)
	}
	if len(meta.ProtocolActionsExcerpt) == 0 {
		t.Fatalf("metadata protocol diagnostics missing excerpt: %#v", meta)
	}
	if meta.ProtocolActiveTurnID == "" || len(meta.ProtocolActiveLines) == 0 {
		t.Fatalf("metadata active-turn evidence missing: %#v", meta)
	}
}

func TestRunRoutesManagerPrefixAndQuarantinesUnprefixedOutput(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedManagerAndPayloadCommand("hello manager", `{"action":"done","summary":"ok","changes":false}`)+`; printf 'free text line\n'`)

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

	foundManager := false
	foundQuarantine := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry struct {
			Kind   string         `json:"kind"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode event log entry: %v", err)
		}
		if entry.Kind == "manager_message" {
			foundManager = true
			if body, _ := entry.Fields["body"].(string); body != "hello manager" {
				t.Fatalf("unexpected manager message body: %#v", entry.Fields)
			}
		}
		if entry.Kind == "agent_quarantine" {
			foundQuarantine = true
			if count, ok := entry.Fields["count"]; !ok || count.(float64) < 1 {
				t.Fatalf("unexpected quarantine entry: %#v", entry.Fields)
			}
		}
	}
	if !foundManager {
		t.Fatalf("expected manager_message event")
	}
	if !foundQuarantine {
		t.Fatalf("expected agent_quarantine event")
	}
}

func TestRunPersistsInFlightAttemptJournalOnAgentFailure(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_MAX_REPAIR_ATTEMPTS", "0")
	t.Setenv("SIMUG_AGENT_CMD", `turn="$SIMUG_PROTOCOL_TURN_ID"; printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"; printf 'SIMUG: {not-json}\n'; printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"`)

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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"tick","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	err := RunOnce(context.Background(), tmp)
	if err == nil {
		t.Fatalf("expected run failure")
	}

	stateData, readErr := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if readErr != nil {
		t.Fatalf("read state file: %v", readErr)
	}
	var st struct {
		InFlightAttempt struct {
			AttemptIndex   int    `json:"attempt_index"`
			MaxAttempts    int    `json:"max_attempts"`
			ExpectedBranch string `json:"expected_branch"`
			Mode           string `json:"mode"`
			Phase          string `json:"phase"`
			PromptHash     string `json:"prompt_hash"`
			BeforeHead     string `json:"before_head"`
			AgentError     string `json:"agent_error"`
		} `json:"in_flight_attempt"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.InFlightAttempt.AttemptIndex != 1 || st.InFlightAttempt.MaxAttempts != 1 {
		t.Fatalf("unexpected attempt indexes: %#v", st.InFlightAttempt)
	}
	if st.InFlightAttempt.ExpectedBranch != branch || st.InFlightAttempt.Mode != "managed_pr" {
		t.Fatalf("unexpected branch/mode in journal: %#v", st.InFlightAttempt)
	}
	if st.InFlightAttempt.Phase != "failed" {
		t.Fatalf("phase=%q, want failed", st.InFlightAttempt.Phase)
	}
	if strings.TrimSpace(st.InFlightAttempt.PromptHash) == "" || strings.TrimSpace(st.InFlightAttempt.BeforeHead) == "" {
		t.Fatalf("expected prompt hash and before head in journal: %#v", st.InFlightAttempt)
	}
	if strings.TrimSpace(st.InFlightAttempt.AgentError) == "" {
		t.Fatalf("expected agent_error to be captured in journal: %#v", st.InFlightAttempt)
	}
}

func TestRunArchivesExactProtocolEvidenceOnDuplicatedTerminalFailure(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_MAX_REPAIR_ATTEMPTS", "0")
	t.Setenv("SIMUG_AGENT_CMD", strings.Join([]string{
		`printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"stale-turn"}\n'`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"stale-turn","payload":{"action":"done","summary":"stale","changes":false}}\n'`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"stale-turn"}\n'`,
		`turn="$SIMUG_PROTOCOL_TURN_ID"`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"%s"}\n' "$turn"`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"%s","payload":{"action":"done","summary":"ok","changes":false}}\n' "$turn"`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"%s","payload":{"action":"idle","reason":"duplicate"}}\n' "$turn"`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"%s"}\n' "$turn"`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"begin","turn_id":"echo-turn"}\n'`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"action","turn_id":"echo-turn","payload":{"action":"done","summary":"echo","changes":false}}\n'`,
		`printf 'SIMUG: {"envelope":"coordinator","event":"end","turn_id":"echo-turn"}\n'`,
	}, "; "))

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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"tick","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	err := RunOnce(context.Background(), tmp)
	if err == nil {
		t.Fatalf("expected run failure")
	}
	if !strings.Contains(err.Error(), "exactly one terminal action") {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(tmp, ".simug", "events.log"))
	if readErr != nil {
		t.Fatalf("read events log: %v", readErr)
	}

	var archiveFields map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry struct {
			Kind   string         `json:"kind"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode event log entry: %v", err)
		}
		if entry.Kind == "agent_archive" {
			archiveFields = entry.Fields
		}
	}
	if archiveFields == nil {
		t.Fatalf("expected agent_archive event")
	}

	outputPath, _ := archiveFields["output_path"].(string)
	transcriptPath, _ := archiveFields["transcript_path"].(string)
	metadataPath, _ := archiveFields["metadata_path"].(string)
	if outputPath == "" || transcriptPath == "" || metadataPath == "" {
		t.Fatalf("archive event missing paths: %#v", archiveFields)
	}

	outputData, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read output archive: %v", readErr)
	}
	if strings.TrimSpace(string(outputData)) == "" {
		t.Fatalf("expected non-empty raw output archive")
	}

	transcriptData, readErr := os.ReadFile(transcriptPath)
	if readErr != nil {
		t.Fatalf("read transcript archive: %v", readErr)
	}
	if !strings.Contains(string(transcriptData), ` codex[protocol] SIMUG: {"envelope":"coordinator","event":"action"`) {
		t.Fatalf("expected transcript protocol classification, got: %s", string(transcriptData))
	}

	var meta struct {
		ProtocolTerminalCount int    `json:"protocol_terminal_count"`
		ProtocolParserHint    string `json:"protocol_parser_hint"`
	}
	metaData, readErr := os.ReadFile(metadataPath)
	if readErr != nil {
		t.Fatalf("read metadata archive: %v", readErr)
	}
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("decode metadata archive: %v", err)
	}
	if !strings.Contains(meta.ProtocolParserHint, "exactly one terminal action") {
		t.Fatalf("unexpected parser hint: %q", meta.ProtocolParserHint)
	}
}

func TestRunClearsInFlightAttemptJournalAfterSuccessfulAttempt(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"done","summary":"ok","changes":false}`))

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
		commandKey("gh", "api", "repos/example/simug/issues/42/comments", "--paginate", "--slurp"): `[[{"id":1001,"body":"tick","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}]]`,
		commandKey("gh", "api", "repos/example/simug/pulls/42/comments", "--paginate", "--slurp"):  "[]",
		commandKey("gh", "api", "repos/example/simug/pulls/42/reviews", "--paginate", "--slurp"):   "[]",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		InFlightAttempt any `json:"in_flight_attempt"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.InFlightAttempt != nil {
		t.Fatalf("expected in_flight_attempt to be cleared after success, got %#v", st.InFlightAttempt)
	}
}

func TestRunNoOpenPRIdlePersistsIssueTriageMode(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"):                                                   `[]`,
		commandKey("git", "status", "--porcelain"):                                     "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                         "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"): "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                         "abcdef\n",
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		Mode        string `json:"mode"`
		ActivePR    int    `json:"active_pr"`
		ActiveIssue int    `json:"active_issue"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.Mode != "issue_triage" {
		t.Fatalf("mode=%q, want issue_triage", st.Mode)
	}
	if st.ActivePR != 0 {
		t.Fatalf("active_pr=%d, want 0", st.ActivePR)
	}
	if st.ActiveIssue != 0 {
		t.Fatalf("active_issue=%d, want 0", st.ActiveIssue)
	}
}

func TestRunOnceNoOpenPRIdleCompletesSingleTick(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"):                                                   `[]`,
		commandKey("git", "status", "--porcelain"):                                     "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                         "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"): "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                         "abcdef\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.Mode != "issue_triage" {
		t.Fatalf("mode=%q, want issue_triage", st.Mode)
	}
}

func TestRunOnceRecoversInFlightAttemptWithReplayAction(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "issue_triage",
  "in_flight_attempt": {
    "run_id": "old-run",
    "tick_seq": 4,
    "attempt_index": 1,
    "max_attempts": 2,
    "expected_branch": "main",
    "mode": "issue_triage",
    "phase": "started",
    "prompt_hash": "abc123",
    "before_head": "deadbeef",
    "started_at": "2026-03-08T01:00:00Z",
    "updated_at": "2026-03-08T01:00:00Z"
  },
  "updated_at": "2026-03-08T01:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"):      tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):       "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"):      "alice\n",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): "main\n",
		commandKey("git", "status", "--porcelain"):             "\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("git", "fetch", "--prune", "origin"):                                                         "",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                          "0 0\n",
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[]`,
		commandKey("git", "rev-parse", "HEAD"):                                                                  "abcdef\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		CursorUncertain bool `json:"cursor_uncertain"`
		LastRecovery    struct {
			Action string `json:"action"`
		} `json:"last_recovery"`
		InFlightAttempt any `json:"in_flight_attempt"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.LastRecovery.Action != "replay" {
		t.Fatalf("last_recovery.action=%q, want replay", st.LastRecovery.Action)
	}
	if !st.CursorUncertain {
		t.Fatalf("expected cursor_uncertain=true after replay recovery")
	}
	if st.InFlightAttempt != nil {
		t.Fatalf("expected in_flight_attempt cleared after replay recovery, got %#v", st.InFlightAttempt)
	}
}

func TestRunOnceRecoversInFlightAttemptWithResumeAction(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "issue_triage",
  "in_flight_attempt": {
    "run_id": "old-run",
    "tick_seq": 4,
    "attempt_index": 1,
    "max_attempts": 2,
    "expected_branch": "main",
    "mode": "issue_triage",
    "phase": "validated",
    "prompt_hash": "abc123",
    "before_head": "deadbeef",
    "after_head": "deadbeef",
    "started_at": "2026-03-08T01:00:00Z",
    "updated_at": "2026-03-08T01:00:00Z"
  },
  "updated_at": "2026-03-08T01:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"):      tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):       "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"):      "alice\n",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): "main\n",
		commandKey("git", "status", "--porcelain"):             "\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("git", "fetch", "--prune", "origin"):                                                         "",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                          "0 0\n",
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[]`,
		commandKey("git", "rev-parse", "HEAD"):                                                                  "abcdef\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		CursorUncertain bool `json:"cursor_uncertain"`
		LastRecovery    struct {
			Action string `json:"action"`
		} `json:"last_recovery"`
		InFlightAttempt any `json:"in_flight_attempt"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.LastRecovery.Action != "resume" {
		t.Fatalf("last_recovery.action=%q, want resume", st.LastRecovery.Action)
	}
	if st.CursorUncertain {
		t.Fatalf("expected cursor_uncertain=false for resume recovery")
	}
	if st.InFlightAttempt != nil {
		t.Fatalf("expected in_flight_attempt cleared after resume recovery, got %#v", st.InFlightAttempt)
	}
}

func TestRunOnceRecoversInFlightAttemptWithRepairAction(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "issue_triage",
  "in_flight_attempt": {
    "run_id": "old-run",
    "tick_seq": 4,
    "attempt_index": 1,
    "max_attempts": 2,
    "expected_branch": "agent/20260307-120000-next-task",
    "mode": "managed_pr",
    "phase": "validated",
    "prompt_hash": "abc123",
    "before_head": "deadbeef",
    "after_head": "deadbeef",
    "started_at": "2026-03-08T01:00:00Z",
    "updated_at": "2026-03-08T01:00:00Z"
  },
  "updated_at": "2026-03-08T01:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"):      tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):       "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"):      "alice\n",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): "main\n",
		commandKey("git", "status", "--porcelain"):             "\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("git", "fetch", "--prune", "origin"):                                                         "",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):                          "0 0\n",
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[]`,
		commandKey("git", "rev-parse", "HEAD"):                                                                  "abcdef\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		CursorUncertain bool `json:"cursor_uncertain"`
		LastRecovery    struct {
			Action string `json:"action"`
		} `json:"last_recovery"`
		InFlightAttempt any `json:"in_flight_attempt"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.LastRecovery.Action != "repair" {
		t.Fatalf("last_recovery.action=%q, want repair", st.LastRecovery.Action)
	}
	if !st.CursorUncertain {
		t.Fatalf("expected cursor_uncertain=true after repair recovery")
	}
	if st.InFlightAttempt != nil {
		t.Fatalf("expected in_flight_attempt cleared after repair recovery, got %#v", st.InFlightAttempt)
	}
}

func TestRunRecoveryAbortOnDirtyTree(t *testing.T) {
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"noop"}`))
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "issue_triage",
  "in_flight_attempt": {
    "run_id": "old-run",
    "tick_seq": 4,
    "attempt_index": 1,
    "max_attempts": 2,
    "expected_branch": "main",
    "mode": "issue_triage",
    "phase": "started",
    "prompt_hash": "abc123",
    "before_head": "deadbeef",
    "started_at": "2026-03-08T01:00:00Z",
    "updated_at": "2026-03-08T01:00:00Z"
  },
  "updated_at": "2026-03-08T01:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"):      tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):       "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"):      "alice\n",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): "main\n",
		commandKey("git", "status", "--porcelain"):             " M file.txt\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	err := Run(context.Background(), tmp)
	if err == nil {
		t.Fatalf("expected recovery abort error")
	}
	if !strings.Contains(err.Error(), "restart recovery abort") {
		t.Fatalf("unexpected error: %v", err)
	}

	stateData, readErr := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if readErr != nil {
		t.Fatalf("read state file: %v", readErr)
	}
	var st struct {
		LastRecovery struct {
			Action string `json:"action"`
		} `json:"last_recovery"`
		InFlightAttempt any `json:"in_flight_attempt"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.LastRecovery.Action != "abort" {
		t.Fatalf("last_recovery.action=%q, want abort", st.LastRecovery.Action)
	}
	if st.InFlightAttempt == nil {
		t.Fatalf("expected in_flight_attempt retained after abort")
	}
}

func TestRunRecoveryAbortWhenFailedBootstrapAttemptAdvancedHead(t *testing.T) {
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"noop"}`))
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "task_bootstrap",
  "bootstrap_intent": {
    "task_ref": "Task 7.3c",
    "summary": "guard bootstrap commits",
    "branch_slug": "bootstrap-single-commit-guard",
    "branch_name": "agent/20260310-120000-bootstrap-single-commit-guard",
    "pr_title": "fix: guard bootstrap commits",
    "pr_body": "Prevents stacked bootstrap commits",
    "approved_at": "2026-03-08T01:00:00Z"
  },
  "in_flight_attempt": {
    "run_id": "old-run",
    "tick_seq": 4,
    "attempt_index": 1,
    "max_attempts": 2,
    "expected_branch": "agent/20260310-120000-bootstrap-single-commit-guard",
    "mode": "task_bootstrap",
    "phase": "failed",
    "prompt_hash": "abc123",
    "before_head": "deadbeef",
    "after_head": "feedface",
    "validation_error": "execution report validation requires exactly one REPORT_JSON comment, got 3",
    "started_at": "2026-03-08T01:00:00Z",
    "updated_at": "2026-03-08T01:00:00Z"
  },
  "updated_at": "2026-03-08T01:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"):      tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):       "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"):      "alice\n",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"): "agent/20260310-120000-bootstrap-single-commit-guard\n",
		commandKey("git", "status", "--porcelain"):             "\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	err := Run(context.Background(), tmp)
	if err == nil {
		t.Fatalf("expected recovery abort error")
	}
	if !strings.Contains(err.Error(), "restart recovery abort") || !strings.Contains(err.Error(), "advanced HEAD") {
		t.Fatalf("unexpected error: %v", err)
	}

	stateData, readErr := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if readErr != nil {
		t.Fatalf("read state file: %v", readErr)
	}
	var st struct {
		LastRecovery struct {
			Action string `json:"action"`
			Reason string `json:"reason"`
		} `json:"last_recovery"`
		InFlightAttempt any `json:"in_flight_attempt"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.LastRecovery.Action != "abort" {
		t.Fatalf("last_recovery.action=%q, want abort", st.LastRecovery.Action)
	}
	if !strings.Contains(st.LastRecovery.Reason, "advanced HEAD") {
		t.Fatalf("unexpected last_recovery.reason: %q", st.LastRecovery.Reason)
	}
	if st.InFlightAttempt == nil {
		t.Fatalf("expected in_flight_attempt retained after abort")
	}
}

func TestRunNoOpenPRFailsOnFetchOriginError(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	runner := mockCommandRunner{
		responses: map[string]string{
			commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
			commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
			commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
			commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
			commandKey("git", "status", "--porcelain"): "\n",
		},
		errors: map[string]error{
			commandKey("git", "fetch", "--prune", "origin"): fmt.Errorf("network down"),
		},
	}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	err := RunOnce(context.Background(), tmp)
	if err == nil {
		t.Fatalf("expected fetch failure")
	}
	if !strings.Contains(err.Error(), "fetch origin") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMergeFinalizationFailsWhenCloseIssueFails(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`))

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "active_pr": 42,
  "active_branch": "agent/20260307-120000-alpha-task",
  "mode": "managed_pr",
  "issue_links": [
    {
      "pr_number": 42,
      "issue_number": 7,
      "relation": "fixes",
      "comment_body": "Resolved by merge",
      "provenance": "run=abc tick=1",
      "idempotency_key": "fix-key",
      "recorded_at": "2026-03-08T00:49:00Z",
      "comment_posted": true,
      "finalized": false
    }
  ],
  "updated_at": "2026-03-08T00:49:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	link := state.IssueLink{
		PRNumber:       42,
		IssueNumber:    7,
		Relation:       "fixes",
		CommentBody:    "Resolved by merge",
		IdempotencyKey: "fix-key",
	}
	finalBody := buildIssueFinalizationCommentBody("example/simug", link)
	runner := mockCommandRunner{
		responses: map[string]string{
			commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
			commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
			commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
			commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
			commandKey("gh", "pr", "view", "42", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"):                                   `{"number":42,"title":"A","state":"MERGED","headRefName":"agent/20260307-120000-alpha-task","headRefOid":"abcdef","baseRefName":"main","author":{"login":"alice"},"mergedAt":"2026-03-08T00:50:00Z"}`,
			commandKey("gh", "api", "repos/example/simug/issues/7"):                                                                                                   `{"number":7,"title":"tracked","body":"x","state":"OPEN","user":{"login":"alice"}}`,
			commandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"):                                                                 "[]",
			commandKey("gh", "issue", "comment", "7", "--body", finalBody):                                                                                            "",
		},
		errors: map[string]error{
			commandKey("gh", "api", "repos/example/simug/issues/7", "--method", "PATCH", "-f", "state=closed"): fmt.Errorf("permission denied"),
		},
	}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	err := RunOnce(context.Background(), tmp)
	if err == nil {
		t.Fatalf("expected close issue failure")
	}
	if !strings.Contains(err.Error(), "close issue #7") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunNoOpenPRSkipsIssueTriageWhenPendingTaskExists(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", `input="$(cat)"; if printf '%s' "$input" | grep -q "Legacy pending task hint: prioritize Task 5.4a."; then `+envelopedAgentCommand(
		`{"action":"comment","body":"INTENT_JSON:{\"task_ref\":\"Task 5.4a\",\"summary\":\"bootstrap pending task\",\"branch_slug\":\"task-5-4a\",\"pr_title\":\"feat: task 5.4a\",\"pr_body\":\"Implements Task 5.4a\",\"checks\":[\"GOCACHE=/tmp/go-build go test ./...\"]}"}`,
		`{"action":"done","summary":"intent prepared","changes":false}`,
	)+`; else `+envelopedAgentCommand(`{"action":"idle","reason":"missing pending task target"}`)+`; fi`)

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".simug"), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	stateJSON := `{
  "repo": "example/simug",
  "mode": "issue_triage",
  "pending_task_id": "5.4a",
  "updated_at": "2026-03-07T00:00:00Z"
}
`
	if err := os.WriteFile(filepath.Join(tmp, ".simug", "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("git", "status", "--porcelain"):                                     "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                         "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"): "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                         "abcdef\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		Mode            string `json:"mode"`
		PendingTaskID   string `json:"pending_task_id"`
		BootstrapIntent *struct {
			TaskRef    string `json:"task_ref"`
			BranchSlug string `json:"branch_slug"`
			BranchName string `json:"branch_name"`
		} `json:"bootstrap_intent"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.Mode != "task_bootstrap" {
		t.Fatalf("mode=%q, want task_bootstrap", st.Mode)
	}
	if st.PendingTaskID != "5.4a" {
		t.Fatalf("pending_task_id=%q, want 5.4a", st.PendingTaskID)
	}
	if st.BootstrapIntent == nil {
		t.Fatalf("bootstrap_intent=nil, want persisted intent")
	}
	if st.BootstrapIntent.TaskRef != "Task 5.4a" {
		t.Fatalf("task_ref=%q, want Task 5.4a", st.BootstrapIntent.TaskRef)
	}
	if st.BootstrapIntent.BranchSlug != "task-5-4a" {
		t.Fatalf("branch_slug=%q, want task-5-4a", st.BootstrapIntent.BranchSlug)
	}
	if !strings.HasPrefix(st.BootstrapIntent.BranchName, "agent/") {
		t.Fatalf("branch_name=%q, want agent/*", st.BootstrapIntent.BranchName)
	}
}

func TestRunNoOpenPRSelectsOldestAuthoredIssueDeterministically(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", `input="$(cat)"; if printf '%s' "$input" | grep -q "Selected issue: #"; then `+envelopedAgentCommand(
		`{"action":"issue_report","issue_number":4,"relevant":true,"analysis":"needs task","needs_task":true,"task_title":"x","task_body":"y"}`,
		`{"action":"done","summary":"triaged","changes":false}`,
	)+`; elif printf '%s' "$input" | grep -q "Issue-derived intake context is active: issue #4."; then `+envelopedAgentCommand(
		`{"action":"comment","body":"INTENT_JSON:{\"task_ref\":\"Task 7.2a\",\"summary\":\"bootstrap after triage\",\"branch_slug\":\"bootstrap-after-triage\",\"pr_title\":\"feat: bootstrap after triage\",\"pr_body\":\"Implements planned task\",\"checks\":[\"GOCACHE=/tmp/go-build go test ./...\"]}"}`,
		`{"action":"done","summary":"intent prepared","changes":false}`,
	)+`; else `+envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`)+`; fi`)

	tmp := t.TempDir()
	report := agent.Action{
		Type:        agent.ActionIssueReport,
		IssueNumber: 4,
		Relevant:    true,
		Analysis:    "needs task",
		NeedsTask:   true,
		TaskTitle:   "x",
		TaskBody:    "y",
	}
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[[` +
			`{"number":11,"title":"later","state":"OPEN","user":{"login":"alice"}},` +
			`{"number":4,"title":"older","state":"OPEN","user":{"login":"alice"}},` +
			`{"number":7,"title":"middle","state":"OPEN","user":{"login":"alice"}}` +
			`]]`,
		commandKey("gh", "api", "repos/example/simug/issues/4/comments", "--paginate", "--slurp"): "[]",
		commandKey("gh", "issue", "comment", "4", "--body", buildIssueTriageCommentBody(report)):  "",
		commandKey("git", "status", "--porcelain"):                                                "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                           "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                    "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):            "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                                    "abcdef\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	if err := RunOnce(context.Background(), tmp); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		Mode            string `json:"mode"`
		ActiveIssue     int    `json:"active_issue"`
		PendingTaskID   string `json:"pending_task_id"`
		BootstrapIntent *struct {
			TaskRef string `json:"task_ref"`
		} `json:"bootstrap_intent"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.Mode != "task_bootstrap" {
		t.Fatalf("mode=%q, want task_bootstrap", st.Mode)
	}
	if st.ActiveIssue != 4 {
		t.Fatalf("active_issue=%d, want 4", st.ActiveIssue)
	}
	if st.PendingTaskID != "7.2a" {
		t.Fatalf("pending_task_id=%q, want 7.2a", st.PendingTaskID)
	}
	if st.BootstrapIntent == nil {
		t.Fatalf("bootstrap_intent=nil, want persisted intent")
	}
}

func TestRunNoOpenPRNeedsTaskDoesNotRequirePlanningFile(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_MAX_REPAIR_ATTEMPTS", "0")
	t.Setenv("SIMUG_AGENT_CMD", `input="$(cat)"; if printf '%s' "$input" | grep -q "Selected issue: #"; then `+envelopedAgentCommand(
		`{"action":"issue_report","issue_number":4,"relevant":true,"analysis":"needs task","needs_task":true,"task_title":"x","task_body":"y"}`,
		`{"action":"done","summary":"triaged","changes":false}`,
	)+`; else `+envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`)+`; fi`)

	tmp := t.TempDir()
	report := agent.Action{
		Type:        agent.ActionIssueReport,
		IssueNumber: 4,
		Relevant:    true,
		Analysis:    "needs task",
		NeedsTask:   true,
		TaskTitle:   "x",
		TaskBody:    "y",
	}
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[[` +
			`{"number":4,"title":"older","state":"OPEN","user":{"login":"alice"}}` +
			`]]`,
		commandKey("gh", "api", "repos/example/simug/issues/4/comments", "--paginate", "--slurp"): "[]",
		commandKey("gh", "issue", "comment", "4", "--body", buildIssueTriageCommentBody(report)):  "",
		commandKey("git", "status", "--porcelain"):                                                "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                           "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                    "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):            "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                                    "abcdef\n",
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

	stateData, err := os.ReadFile(filepath.Join(tmp, ".simug", "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st struct {
		PendingTaskID string `json:"pending_task_id"`
	}
	if err := json.Unmarshal(stateData, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	if st.PendingTaskID != "" {
		t.Fatalf("pending_task_id=%q, want empty", st.PendingTaskID)
	}
}

func TestRunNoOpenPRIssueTriageRejectsMissingIssueReport(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_MAX_REPAIR_ATTEMPTS", "0")
	t.Setenv("SIMUG_AGENT_CMD", envelopedAgentCommand(`{"action":"done","summary":"triaged","changes":false}`))

	tmp := t.TempDir()
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[[` +
			`{"number":4,"title":"older","state":"OPEN","user":{"login":"alice"}}` +
			`]]`,
		commandKey("git", "status", "--porcelain"):                                     "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                         "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"): "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                         "abcdef\n",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()
	restoreGitHub := github.SetCommandRunnerForTest(runner)
	defer restoreGitHub()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := Run(ctx, tmp)
	if err == nil {
		t.Fatalf("expected issue triage validation error, got nil")
	}
	if !strings.Contains(err.Error(), "exactly one issue_report") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunNoOpenPRIssueTriageSkipsDuplicateMarkerComment(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", `input="$(cat)"; if printf '%s' "$input" | grep -q "Selected issue: #"; then `+envelopedAgentCommand(
		`{"action":"issue_report","issue_number":4,"relevant":false,"analysis":"already handled","needs_task":false}`,
		`{"action":"done","summary":"triaged","changes":false}`,
	)+`; else `+envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`)+`; fi`)

	tmp := t.TempDir()
	report := agent.Action{
		Type:        agent.ActionIssueReport,
		IssueNumber: 4,
		Relevant:    false,
		Analysis:    "already handled",
		NeedsTask:   false,
	}
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[[` +
			`{"number":4,"title":"older","state":"OPEN","user":{"login":"alice"}}` +
			`]]`,
		commandKey("gh", "api", "repos/example/simug/issues/4/comments", "--paginate", "--slurp"): `[[` +
			`{"id":1001,"body":"` + issueTriageMarker(report) + `","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}` +
			`]]`,
		commandKey("git", "status", "--porcelain"):                                     "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                         "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"): "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                         "abcdef\n",
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

func TestRunNoOpenPRIssueTriageIgnoresMarkerFromOtherAuthor(t *testing.T) {
	t.Setenv("SIMUG_POLL_SECONDS", "3600")
	t.Setenv("SIMUG_AGENT_CMD", `input="$(cat)"; if printf '%s' "$input" | grep -q "Selected issue: #"; then `+envelopedAgentCommand(
		`{"action":"issue_report","issue_number":4,"relevant":false,"analysis":"still comment","needs_task":false}`,
		`{"action":"done","summary":"triaged","changes":false}`,
	)+`; else `+envelopedAgentCommand(`{"action":"idle","reason":"no task available"}`)+`; fi`)

	tmp := t.TempDir()
	report := agent.Action{
		Type:        agent.ActionIssueReport,
		IssueNumber: 4,
		Relevant:    false,
		Analysis:    "still comment",
		NeedsTask:   false,
	}
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "rev-parse", "--show-toplevel"): tmp + "\n",
		commandKey("git", "remote", "get-url", "origin"):  "https://github.com/example/simug.git\n",
		commandKey("gh", "api", "user", "--jq", ".login"): "alice\n",
		commandKey("gh", "pr", "list", "--state", "open", "--author", "alice", "--json", "number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt"): `[]`,
		commandKey("gh", "api", "repos/example/simug/issues?state=open&creator=alice", "--paginate", "--slurp"): `[[` +
			`{"number":4,"title":"older","state":"OPEN","user":{"login":"alice"}}` +
			`]]`,
		commandKey("gh", "api", "repos/example/simug/issues/4/comments", "--paginate", "--slurp"): `[[` +
			`{"id":1001,"body":"` + issueTriageMarker(report) + `","created_at":"2026-03-07T12:00:00Z","user":{"login":"mallory"}}` +
			`]]`,
		commandKey("gh", "issue", "comment", "4", "--body", buildIssueTriageCommentBody(report)): "",
		commandKey("git", "status", "--porcelain"):                                               "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                          "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                                   "main\n",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"):           "0 0\n",
		commandKey("git", "rev-parse", "HEAD"):                                                   "abcdef\n",
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
