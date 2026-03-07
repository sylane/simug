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
