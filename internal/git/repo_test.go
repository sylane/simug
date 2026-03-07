package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockCommandRunner struct {
	run func(ctx context.Context, dir, name string, args ...string) (string, error)
}

func (m mockCommandRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	return m.run(ctx, dir, name, args...)
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name       string
		remote     string
		wantOwner  string
		wantRepo   string
		shouldFail bool
	}{
		{name: "ssh", remote: "git@github.com:octo/example.git", wantOwner: "octo", wantRepo: "example"},
		{name: "https", remote: "https://github.com/octo/example.git", wantOwner: "octo", wantRepo: "example"},
		{name: "https no git suffix", remote: "https://github.com/octo/example", wantOwner: "octo", wantRepo: "example"},
		{name: "unsupported", remote: "https://gitlab.com/octo/example.git", shouldFail: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, err := parseGitHubURL(tc.remote)
			if tc.shouldFail {
				if err == nil {
					t.Fatalf("expected error for remote %q", tc.remote)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseGitHubURL(%q) returned error: %v", tc.remote, err)
			}
			if repo.Owner != tc.wantOwner || repo.Name != tc.wantRepo {
				t.Fatalf("parseGitHubURL(%q) = %s/%s, want %s/%s", tc.remote, repo.Owner, repo.Name, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}

func TestRunEmitsCommandTraceOnSuccess(t *testing.T) {
	restoreRunner := SetCommandRunnerForTest(mockCommandRunner{
		run: func(_ context.Context, _ string, name string, args ...string) (string, error) {
			if name != "git" || strings.Join(args, " ") != "rev-parse --show-toplevel" {
				t.Fatalf("unexpected command %s %s", name, strings.Join(args, " "))
			}
			return "/tmp/repo\n", nil
		},
	})
	defer restoreRunner()

	var got []CommandTrace
	restoreTrace := SetCommandTraceHook(func(trace CommandTrace) { got = append(got, trace) })
	defer restoreTrace()

	root, err := RepoRoot(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("RepoRoot returned error: %v", err)
	}
	if root != "/tmp/repo" {
		t.Fatalf("RepoRoot = %q, want /tmp/repo", root)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(got))
	}
	if got[0].Name != "git" || got[0].ExitCode != 0 {
		t.Fatalf("unexpected trace: %#v", got[0])
	}
	if got[0].StdoutTail != "/tmp/repo" {
		t.Fatalf("unexpected stdout tail: %q", got[0].StdoutTail)
	}
}

func TestRunEmitsCommandTraceOnError(t *testing.T) {
	restoreRunner := SetCommandRunnerForTest(mockCommandRunner{
		run: func(_ context.Context, _ string, _ string, _ ...string) (string, error) {
			return "", errors.New("boom")
		},
	})
	defer restoreRunner()

	var got []CommandTrace
	restoreTrace := SetCommandTraceHook(func(trace CommandTrace) { got = append(got, trace) })
	defer restoreTrace()

	_, err := RepoRoot(context.Background(), "/tmp")
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
