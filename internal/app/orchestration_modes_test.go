package app

import (
	"context"
	"testing"

	"simug/internal/git"
)

func TestEnsureMainReadyAllowsGitHubMergedBranchWithoutAncestorCheck(t *testing.T) {
	branch := "agent/20260313-120000-rebased-task"
	runner := mockCommandRunner{responses: map[string]string{
		commandKey("git", "status", "--porcelain"):                                     "\n",
		commandKey("git", "fetch", "--prune", "origin"):                                "",
		commandKey("git", "rev-parse", "--abbrev-ref", "HEAD"):                         branch + "\n",
		commandKey("git", "checkout", "main"):                                          "",
		commandKey("git", "rev-list", "--left-right", "--count", "HEAD...origin/main"): "0 1\n",
		commandKey("git", "pull", "--ff-only", "origin", "main"):                       "",
		commandKey("git", "branch", "-d", branch):                                      "",
	}}

	restoreGit := git.SetCommandRunnerForTest(runner)
	defer restoreGit()

	o := orchestrator{
		repoRoot: "/tmp/repo",
		cfg: config{
			MainBranch: "main",
		},
	}

	err := o.ensureMainReady(context.Background(), mergedBranchTransitionContext{
		PRNumber: 42,
		Branch:   branch,
	})
	if err != nil {
		t.Fatalf("ensureMainReady returned error: %v", err)
	}
}
