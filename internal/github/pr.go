package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

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

type PullRequest struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	HeadRefName string     `json:"headRefName"`
	HeadRefOid  string     `json:"headRefOid"`
	BaseRefName string     `json:"baseRefName"`
	MergedAt    *time.Time `json:"mergedAt"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Author struct {
		Login string `json:"login"`
	} `json:"user"`
}

type IssueComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type ReviewComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type Review struct {
	ID          int64      `json:"id"`
	Body        string     `json:"body"`
	SubmittedAt *time.Time `json:"submitted_at"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

func CurrentUser(ctx context.Context, repoRoot string) (string, error) {
	out, err := run(ctx, repoRoot, "gh", "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	login := strings.TrimSpace(out)
	if login == "" {
		return "", fmt.Errorf("empty login from gh api user")
	}
	return login, nil
}

func ListOpenPRsByAuthor(ctx context.Context, repoRoot, author string) ([]PullRequest, error) {
	out, err := run(
		ctx,
		repoRoot,
		"gh",
		"pr",
		"list",
		"--state",
		"open",
		"--author",
		author,
		"--json",
		"number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt",
	)
	if err != nil {
		return nil, err
	}
	var prs []PullRequest
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("decode gh pr list output: %w", err)
	}
	return prs, nil
}

func GetPR(ctx context.Context, repoRoot string, number int) (PullRequest, error) {
	out, err := run(
		ctx,
		repoRoot,
		"gh",
		"pr",
		"view",
		strconv.Itoa(number),
		"--json",
		"number,title,state,headRefName,headRefOid,baseRefName,author,mergedAt",
	)
	if err != nil {
		return PullRequest{}, err
	}
	var pr PullRequest
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return PullRequest{}, fmt.Errorf("decode gh pr view output: %w", err)
	}
	return pr, nil
}

func CreatePR(ctx context.Context, repoRoot, title, body, base, head string, assignSelf bool) (int, error) {
	args := []string{"pr", "create", "--title", title, "--body", body, "--base", base, "--head", head}
	if assignSelf {
		args = append(args, "--assignee", "@me")
	}
	out, err := run(ctx, repoRoot, "gh", args...)
	if err != nil {
		return 0, err
	}

	number, err := parsePRNumber(out)
	if err != nil {
		return 0, fmt.Errorf("parse created pr number: %w", err)
	}
	return number, nil
}

func CommentPR(ctx context.Context, repoRoot string, number int, body string) error {
	_, err := run(ctx, repoRoot, "gh", "pr", "comment", strconv.Itoa(number), "--body", body)
	return err
}

func ReplyToReviewComment(ctx context.Context, repoRoot, repoFullName string, commentID int64, body string) error {
	path := fmt.Sprintf("repos/%s/pulls/comments/%d/replies", repoFullName, commentID)
	_, err := run(ctx, repoRoot, "gh", "api", path, "--method", "POST", "-f", "body="+body)
	return err
}

func ListIssueComments(ctx context.Context, repoRoot, repoFullName string, prNumber int) ([]IssueComment, error) {
	path := fmt.Sprintf("repos/%s/issues/%d/comments", repoFullName, prNumber)
	out, err := run(ctx, repoRoot, "gh", "api", path, "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}
	comments, err := decodeSlurped[IssueComment](out)
	if err != nil {
		return nil, fmt.Errorf("decode issue comments: %w", err)
	}
	sort.Slice(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	return comments, nil
}

func ListReviewComments(ctx context.Context, repoRoot, repoFullName string, prNumber int) ([]ReviewComment, error) {
	path := fmt.Sprintf("repos/%s/pulls/%d/comments", repoFullName, prNumber)
	out, err := run(ctx, repoRoot, "gh", "api", path, "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}
	comments, err := decodeSlurped[ReviewComment](out)
	if err != nil {
		return nil, fmt.Errorf("decode review comments: %w", err)
	}
	sort.Slice(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	return comments, nil
}

func ListReviews(ctx context.Context, repoRoot, repoFullName string, prNumber int) ([]Review, error) {
	path := fmt.Sprintf("repos/%s/pulls/%d/reviews", repoFullName, prNumber)
	out, err := run(ctx, repoRoot, "gh", "api", path, "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}
	reviews, err := decodeSlurped[Review](out)
	if err != nil {
		return nil, fmt.Errorf("decode reviews: %w", err)
	}
	sort.Slice(reviews, func(i, j int) bool { return reviews[i].ID < reviews[j].ID })
	return reviews, nil
}

func ListOpenIssuesByAuthor(ctx context.Context, repoRoot, repoFullName, author string) ([]Issue, error) {
	path := fmt.Sprintf("repos/%s/issues?state=open&creator=%s", repoFullName, url.QueryEscape(author))
	out, err := run(ctx, repoRoot, "gh", "api", path, "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}

	type rawIssue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		PullRequest json.RawMessage `json:"pull_request"`
	}

	rawIssues, err := decodeSlurped[rawIssue](out)
	if err != nil {
		return nil, fmt.Errorf("decode authored issues: %w", err)
	}

	issues := make([]Issue, 0, len(rawIssues))
	for _, raw := range rawIssues {
		if len(raw.PullRequest) > 0 && string(raw.PullRequest) != "null" {
			continue
		}
		issue := Issue{
			Number: raw.Number,
			Title:  raw.Title,
			Body:   raw.Body,
			State:  raw.State,
		}
		issue.Author.Login = raw.User.Login
		issues = append(issues, issue)
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Number < issues[j].Number })
	return issues, nil
}

func parsePRNumber(raw string) (int, error) {
	for _, token := range strings.Fields(raw) {
		u, err := url.Parse(token)
		if err != nil || u.Host == "" {
			continue
		}
		m := regexp.MustCompile(`/pull/([0-9]+)$`).FindStringSubmatch(u.Path)
		if len(m) != 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, fmt.Errorf("no pull request URL found in output: %q", strings.TrimSpace(raw))
}

func decodeSlurped[T any](raw string) ([]T, error) {
	var pages [][]T
	if err := json.Unmarshal([]byte(raw), &pages); err == nil {
		var out []T
		for _, page := range pages {
			out = append(out, page...)
		}
		return out, nil
	}

	var out []T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
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
