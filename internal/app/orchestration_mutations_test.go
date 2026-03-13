package app

import (
	"context"
	"testing"

	"simug/internal/git"
	"simug/internal/github"
	"simug/internal/state"
)

func TestEnsureIssueCommentMutationSkipsDuplicateMarkerFromSameUser(t *testing.T) {
	o := orchestrator{
		repoRoot: "/tmp",
		repo:     git.Repo{Owner: "example", Name: "simug"},
		user:     "alice",
		state:    &state.State{},
	}

	runner := githubOnlyMockRunner{responses: map[string]string{
		githubCommandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"): `[[` +
			`{"id":1001,"body":"prefix <!-- marker --> suffix","created_at":"2026-03-07T12:00:00Z","user":{"login":"alice"}}` +
			`]]`,
	}}
	restore := github.SetCommandRunnerForTest(runner)
	defer restore()

	exists, err := o.ensureIssueCommentMutation(context.Background(), issueCommentMutationSpec{
		IssueNumber:      7,
		Marker:           "<!-- marker -->",
		Body:             "ignored",
		EventKind:        "test",
		DuplicateMessage: "duplicate",
		PostMessage:      "post",
		ListError:        "list comments",
		PostError:        "post comment",
	})
	if err != nil {
		t.Fatalf("ensureIssueCommentMutation returned error: %v", err)
	}
	if !exists {
		t.Fatalf("expected duplicate marker to be detected")
	}
}

func TestEnsureIssueCommentMutationPostsWhenMarkerOnlyFromOtherUser(t *testing.T) {
	o := orchestrator{
		repoRoot: "/tmp",
		repo:     git.Repo{Owner: "example", Name: "simug"},
		user:     "alice",
		state:    &state.State{},
	}

	runner := githubOnlyMockRunner{responses: map[string]string{
		githubCommandKey("gh", "api", "repos/example/simug/issues/7/comments", "--paginate", "--slurp"): `[[` +
			`{"id":1001,"body":"<!-- marker -->","created_at":"2026-03-07T12:00:00Z","user":{"login":"mallory"}}` +
			`]]`,
		githubCommandKey("gh", "issue", "comment", "7", "--body", "body"): "",
	}}
	restore := github.SetCommandRunnerForTest(runner)
	defer restore()

	exists, err := o.ensureIssueCommentMutation(context.Background(), issueCommentMutationSpec{
		IssueNumber:      7,
		Marker:           "<!-- marker -->",
		Body:             "body",
		EventKind:        "test",
		DuplicateMessage: "duplicate",
		PostMessage:      "post",
		ListError:        "list comments",
		PostError:        "post comment",
	})
	if err != nil {
		t.Fatalf("ensureIssueCommentMutation returned error: %v", err)
	}
	if exists {
		t.Fatalf("expected other-user marker to be ignored")
	}
}
