package git

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Repo struct {
	Owner string
	Name  string
}

type commandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) (string, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

var (
	runnerMu sync.RWMutex
	runner   commandRunner = osCommandRunner{}
)

func SetCommandRunnerForTest(r commandRunner) (restore func()) {
	runnerMu.Lock()
	prev := runner
	runner = r
	runnerMu.Unlock()
	return func() {
		runnerMu.Lock()
		runner = prev
		runnerMu.Unlock()
	}
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

func RepoRoot(ctx context.Context, startDir string) (string, error) {
	out, err := run(ctx, startDir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve git repo root: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func ResolveGitHubRepo(ctx context.Context, repoRoot string) (Repo, error) {
	remote, err := run(ctx, repoRoot, "git", "remote", "get-url", "origin")
	if err != nil {
		return Repo{}, fmt.Errorf("read origin remote url: %w", err)
	}
	return parseGitHubURL(strings.TrimSpace(remote))
}

func parseGitHubURL(remote string) (Repo, error) {
	trimmed := strings.TrimSuffix(remote, ".git")

	if strings.HasPrefix(trimmed, "git@github.com:") {
		re := regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+)$`)
		m := re.FindStringSubmatch(trimmed)
		if len(m) == 3 {
			return Repo{Owner: m[1], Name: m[2]}, nil
		}
	}

	u, err := url.Parse(trimmed)
	if err == nil && strings.EqualFold(u.Host, "github.com") {
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) >= 2 {
			return Repo{Owner: parts[0], Name: parts[1]}, nil
		}
	}

	return Repo{}, fmt.Errorf("unsupported GitHub remote format: %q", remote)
}

func CurrentBranch(ctx context.Context, repoRoot string) (string, error) {
	out, err := run(ctx, repoRoot, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func HeadSHA(ctx context.Context, repoRoot string) (string, error) {
	out, err := run(ctx, repoRoot, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func RefSHA(ctx context.Context, repoRoot, ref string) (string, error) {
	out, err := run(ctx, repoRoot, "git", "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func IsClean(ctx context.Context, repoRoot string) (bool, string, error) {
	out, err := run(ctx, repoRoot, "git", "status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	trimmed := strings.TrimSpace(out)
	return trimmed == "", trimmed, nil
}

func FetchOrigin(ctx context.Context, repoRoot string) error {
	_, err := run(ctx, repoRoot, "git", "fetch", "--prune", "origin")
	return err
}

func Checkout(ctx context.Context, repoRoot, branch string) error {
	_, err := run(ctx, repoRoot, "git", "checkout", branch)
	return err
}

func PullFFOnly(ctx context.Context, repoRoot, remote, branch string) error {
	_, err := run(ctx, repoRoot, "git", "pull", "--ff-only", remote, branch)
	return err
}

func IsAncestor(ctx context.Context, repoRoot, ancestorRef, descendantRef string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", ancestorRef, descendantRef)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s failed: %w: %s", ancestorRef, descendantRef, err, strings.TrimSpace(string(out)))
}

func AheadBehind(ctx context.Context, repoRoot, leftRef, rightRef string) (int, int, error) {
	out, err := run(ctx, repoRoot, "git", "rev-list", "--left-right", "--count", leftRef+"..."+rightRef)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list count output %q", strings.TrimSpace(out))
	}
	left, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse left count: %w", err)
	}
	right, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse right count: %w", err)
	}
	return left, right, nil
}

func Push(ctx context.Context, repoRoot, remote, branch string) error {
	_, err := run(ctx, repoRoot, "git", "push", remote, "HEAD:"+branch)
	return err
}

func CommitCountBetween(ctx context.Context, repoRoot, olderRef, newerRef string) (int, error) {
	out, err := run(ctx, repoRoot, "git", "rev-list", "--count", olderRef+".."+newerRef)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse commit count: %w", err)
	}
	return count, nil
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	runnerMu.RLock()
	active := runner
	runnerMu.RUnlock()
	return active.Run(ctx, dir, name, args...)
}
