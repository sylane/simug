package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Repo struct {
	Owner string
	Name  string
}

type commandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) (string, error)
}

type CommandTrace struct {
	Dir        string
	Name       string
	Args       []string
	Duration   time.Duration
	ExitCode   int
	StdoutTail string
	StderrTail string
	Error      string
}

type commandError struct {
	name     string
	args     []string
	cause    error
	stdout   string
	stderr   string
	exitCode int
}

func (e *commandError) Error() string {
	cmd := strings.TrimSpace(e.name + " " + strings.Join(e.args, " "))
	detail := strings.TrimSpace(e.stderr)
	if detail == "" {
		detail = strings.TrimSpace(e.stdout)
	}
	if detail == "" {
		return fmt.Sprintf("%s failed with exit code %d", cmd, e.exitCode)
	}
	return fmt.Sprintf("%s failed with exit code %d: %s", cmd, e.exitCode, detail)
}

func (e *commandError) Unwrap() error {
	return e.cause
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stdoutText := stdout.String()
	stderrText := stderr.String()
	if err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return stdoutText, &commandError{
			name:     name,
			args:     append([]string(nil), args...),
			cause:    err,
			stdout:   stdoutText,
			stderr:   stderrText,
			exitCode: exitCode,
		}
	}
	return stdoutText, nil
}

var (
	runnerMu    sync.RWMutex
	runner      commandRunner = osCommandRunner{}
	traceHookMu sync.RWMutex
	traceHook   func(CommandTrace)
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

func SetCommandTraceHook(hook func(CommandTrace)) (restore func()) {
	traceHookMu.Lock()
	prev := traceHook
	traceHook = hook
	traceHookMu.Unlock()
	return func() {
		traceHookMu.Lock()
		traceHook = prev
		traceHookMu.Unlock()
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

func DeleteLocalBranch(ctx context.Context, repoRoot, branch string) error {
	_, err := run(ctx, repoRoot, "git", "branch", "-d", branch)
	return err
}

func IsAncestor(ctx context.Context, repoRoot, ancestorRef, descendantRef string) (bool, error) {
	_, err := run(ctx, repoRoot, "git", "merge-base", "--is-ancestor", ancestorRef, descendantRef)
	if err == nil {
		return true, nil
	}
	var cmdErr *commandError
	if errors.As(err, &cmdErr) && cmdErr.exitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s failed: %w", ancestorRef, descendantRef, err)
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

	start := time.Now()
	out, err := active.Run(ctx, dir, name, args...)
	trace := CommandTrace{
		Dir:        dir,
		Name:       name,
		Args:       append([]string(nil), args...),
		Duration:   time.Since(start),
		ExitCode:   0,
		StdoutTail: tailText(out, 400),
	}
	if err != nil {
		trace.Error = err.Error()
		trace.ExitCode = -1

		var cmdErr *commandError
		if errors.As(err, &cmdErr) {
			trace.ExitCode = cmdErr.exitCode
			trace.StdoutTail = tailText(cmdErr.stdout, 400)
			trace.StderrTail = tailText(cmdErr.stderr, 400)
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				trace.ExitCode = exitErr.ExitCode()
			}
		}
	}
	emitTrace(trace)
	return out, err
}

func emitTrace(trace CommandTrace) {
	traceHookMu.RLock()
	hook := traceHook
	traceHookMu.RUnlock()
	if hook != nil {
		hook(trace)
	}
}

func tailText(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) <= max {
		return trimmed
	}
	if max < 4 {
		return trimmed[len(trimmed)-max:]
	}
	return "..." + trimmed[len(trimmed)-(max-3):]
}
