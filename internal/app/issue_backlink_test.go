package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"simug/internal/git"
	"simug/internal/github"
	"simug/internal/state"
)

type githubOnlyMockRunner struct {
	responses map[string]string
	errors    map[string]error
}

func (m githubOnlyMockRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if err, ok := m.errors[key]; ok {
		return "", err
	}
	if out, ok := m.responses[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func githubCommandKey(name string, args ...string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func TestBuildIssuePRBacklinkCommentBody(t *testing.T) {
	body := buildIssuePRBacklinkCommentBody("example/simug", 7, "5.4a", 123)
	required := []string{
		issuePRBacklinkMarker(7, "5.4a", 123),
		"### simug implementation PR link",
		"- Issue: #7",
		"- Task: Task 5.4a",
		"- PR: #123 (https://github.com/example/simug/pull/123)",
	}
	for _, needle := range required {
		if !strings.Contains(body, needle) {
			t.Fatalf("missing %q in backlink body:\n%s", needle, body)
		}
	}
}

func TestMaybePostIssueDerivedPRBacklinkPostsComment(t *testing.T) {
	o := orchestrator{
		repoRoot: "/tmp",
		repo:     git.Repo{Owner: "example", Name: "simug"},
		user:     "alice",
		state: &state.State{
			ActiveIssue:   7,
			PendingTaskID: "5.4a",
		},
	}

	body := buildIssuePRBacklinkCommentBody("example/simug", 7, "5.4a", 123)
	runner := githubOnlyMockRunner{responses: map[string]string{
		githubCommandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"): `[]`,
		githubCommandKey("gh", "issue", "comment", "7", "--body", body):                                 "",
	}}
	restore := github.SetCommandRunnerForTest(runner)
	defer restore()

	if err := o.maybePostIssueDerivedPRBacklink(context.Background(), 123); err != nil {
		t.Fatalf("maybePostIssueDerivedPRBacklink returned error: %v", err)
	}
}

func TestMaybePostIssueDerivedPRBacklinkSkipsDuplicateMarker(t *testing.T) {
	o := orchestrator{
		repoRoot: "/tmp",
		repo:     git.Repo{Owner: "example", Name: "simug"},
		user:     "alice",
		state: &state.State{
			ActiveIssue:   7,
			PendingTaskID: "5.4a",
		},
	}

	marker := issuePRBacklinkMarker(7, "5.4a", 123)
	runner := githubOnlyMockRunner{responses: map[string]string{
		githubCommandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"): `[[` +
			`{"id":1001,"body":"` + marker + `","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}` +
			`]]`,
	}}
	restore := github.SetCommandRunnerForTest(runner)
	defer restore()

	if err := o.maybePostIssueDerivedPRBacklink(context.Background(), 123); err != nil {
		t.Fatalf("maybePostIssueDerivedPRBacklink returned error: %v", err)
	}
}

func TestMaybePostIssueDerivedPRBacklinkIgnoresMarkerFromOtherAuthor(t *testing.T) {
	o := orchestrator{
		repoRoot: "/tmp",
		repo:     git.Repo{Owner: "example", Name: "simug"},
		user:     "alice",
		state: &state.State{
			ActiveIssue:   7,
			PendingTaskID: "5.4a",
		},
	}

	marker := issuePRBacklinkMarker(7, "5.4a", 123)
	body := buildIssuePRBacklinkCommentBody("example/simug", 7, "5.4a", 123)
	runner := githubOnlyMockRunner{responses: map[string]string{
		githubCommandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"): `[[` +
			`{"id":1001,"body":"` + marker + `","created_at":"2026-03-07T12:00:00Z","user":{"login":"mallory"}}` +
			`]]`,
		githubCommandKey("gh", "issue", "comment", "7", "--body", body): "",
	}}
	restore := github.SetCommandRunnerForTest(runner)
	defer restore()

	if err := o.maybePostIssueDerivedPRBacklink(context.Background(), 123); err != nil {
		t.Fatalf("maybePostIssueDerivedPRBacklink returned error: %v", err)
	}
}
