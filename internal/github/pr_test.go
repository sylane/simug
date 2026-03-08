package github

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type mockCommandRunner struct {
	responses map[string]string
	errors    map[string]error
}

func (m mockCommandRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if err, ok := m.errors[key]; ok {
		return "", err
	}
	if out, ok := m.responses[key]; ok {
		return out, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func TestParsePRNumber(t *testing.T) {
	n, err := parsePRNumber("https://github.com/o/r/pull/123\n")
	if err != nil {
		t.Fatalf("parsePRNumber error: %v", err)
	}
	if n != 123 {
		t.Fatalf("got %d want 123", n)
	}
}

func TestDecodeSlurped(t *testing.T) {
	vals, err := decodeSlurped[map[string]any](`[[{"id":1}],[{"id":2}]]`)
	if err != nil {
		t.Fatalf("decodeSlurped error: %v", err)
	}
	if len(vals) != 2 {
		t.Fatalf("expected 2 vals, got %d", len(vals))
	}
}

func TestCurrentUserRejectsEmptyLogin(t *testing.T) {
	r := mockCommandRunner{responses: map[string]string{
		"gh api user --jq .login": "   \n",
	}}
	restore := SetCommandRunnerForTest(r)
	defer restore()

	_, err := CurrentUser(context.Background(), "/tmp")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestListOpenPRsByAuthorParsesJSON(t *testing.T) {
	r := mockCommandRunner{responses: map[string]string{
		"gh pr list --state open --author alice --json number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt": `[{"number":7,"title":"t","state":"OPEN","headRefName":"agent/20260307-120000-next-task","headRefOid":"abc","baseRefName":"main","author":{"login":"alice"},"mergedAt":null}]`,
	}}
	restore := SetCommandRunnerForTest(r)
	defer restore()

	prs, err := ListOpenPRsByAuthor(context.Background(), "/tmp", "alice")
	if err != nil {
		t.Fatalf("ListOpenPRsByAuthor error: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 7 {
		t.Fatalf("unexpected prs: %#v", prs)
	}
}

func TestRunEmitsCommandTraceOnSuccess(t *testing.T) {
	r := mockCommandRunner{responses: map[string]string{
		"gh api user --jq .login": "alice\n",
	}}
	restoreRunner := SetCommandRunnerForTest(r)
	defer restoreRunner()

	var got []CommandTrace
	restoreTrace := SetCommandTraceHook(func(trace CommandTrace) { got = append(got, trace) })
	defer restoreTrace()

	login, err := CurrentUser(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("CurrentUser returned error: %v", err)
	}
	if login != "alice" {
		t.Fatalf("login=%q, want alice", login)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(got))
	}
	if got[0].Name != "gh" || got[0].ExitCode != 0 {
		t.Fatalf("unexpected trace: %#v", got[0])
	}
}

func TestRunEmitsCommandTraceOnError(t *testing.T) {
	r := mockCommandRunner{errors: map[string]error{
		"gh api user --jq .login": fmt.Errorf("no auth"),
	}}
	restoreRunner := SetCommandRunnerForTest(r)
	defer restoreRunner()

	var got []CommandTrace
	restoreTrace := SetCommandTraceHook(func(trace CommandTrace) { got = append(got, trace) })
	defer restoreTrace()

	_, err := CurrentUser(context.Background(), "/tmp")
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(got))
	}
	if got[0].ExitCode != -1 {
		t.Fatalf("expected exit code -1, got %d", got[0].ExitCode)
	}
	if got[0].Error == "" {
		t.Fatalf("expected trace error to be populated")
	}
}

func TestListOpenIssuesByAuthorFiltersPRsAndSortsByNumber(t *testing.T) {
	r := mockCommandRunner{responses: map[string]string{
		"gh api repos/example/simug/issues?state=open&creator=alice --paginate --slurp": `[[` +
			`{"number":9,"title":"z","state":"OPEN","user":{"login":"alice"}},` +
			`{"number":2,"title":"pr","state":"OPEN","user":{"login":"alice"},"pull_request":{"url":"x"}},` +
			`{"number":3,"title":"a","state":"OPEN","user":{"login":"alice"}}` +
			`]]`,
	}}
	restore := SetCommandRunnerForTest(r)
	defer restore()

	issues, err := ListOpenIssuesByAuthor(context.Background(), "/tmp", "example/simug", "alice")
	if err != nil {
		t.Fatalf("ListOpenIssuesByAuthor returned error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %#v", issues)
	}
	if issues[0].Number != 3 || issues[1].Number != 9 {
		t.Fatalf("issues not sorted/filtered as expected: %#v", issues)
	}
}

func TestCommentIssueUsesGhIssueComment(t *testing.T) {
	r := mockCommandRunner{responses: map[string]string{
		"gh issue comment 7 --body triage-body": "",
	}}
	restore := SetCommandRunnerForTest(r)
	defer restore()

	if err := CommentIssue(context.Background(), "/tmp", 7, "triage-body"); err != nil {
		t.Fatalf("CommentIssue returned error: %v", err)
	}
}

func TestGetIssueParsesResponse(t *testing.T) {
	r := mockCommandRunner{responses: map[string]string{
		"gh api repos/example/simug/issues/7": `{"number":7,"title":"Issue","body":"Body","state":"OPEN","user":{"login":"alice"}}`,
	}}
	restore := SetCommandRunnerForTest(r)
	defer restore()

	issue, err := GetIssue(context.Background(), "/tmp", "example/simug", 7)
	if err != nil {
		t.Fatalf("GetIssue returned error: %v", err)
	}
	if issue.Number != 7 || issue.Author.Login != "alice" {
		t.Fatalf("unexpected issue: %#v", issue)
	}
}
