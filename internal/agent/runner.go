package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	protocolPrefixCurrent = "SIMUG:"
	protocolPrefixManager = "SIMUG_MANAGER:"
)

type ActionType string

const (
	ActionComment     ActionType = "comment"
	ActionReply       ActionType = "reply"
	ActionIssueReport ActionType = "issue_report"
	ActionDone        ActionType = "done"
	ActionIdle        ActionType = "idle"
)

type Action struct {
	Type        ActionType
	Body        string
	CommentID   int64
	Summary     string
	Changes     bool
	PRTitle     string
	PRBody      string
	Reason      string
	IssueNumber int
	Relevant    bool
	Analysis    string
	NeedsTask   bool
	TaskTitle   string
	TaskBody    string
}

type Result struct {
	RawOutput        string
	Actions          []Action
	Terminal         Action
	ManagerMessages  []string
	QuarantinedLines []string
}

type Runner struct {
	Command string
}

type RunError struct {
	Cause     error
	RawOutput string
}

func (e *RunError) Error() string {
	if e == nil || e.Cause == nil {
		return ""
	}
	return e.Cause.Error()
}

func (e *RunError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func RawOutputFromError(err error) string {
	var runErr *RunError
	if errors.As(err, &runErr) {
		return runErr.RawOutput
	}
	return ""
}

func (r Runner) Run(ctx context.Context, prompt string) (Result, error) {
	if strings.TrimSpace(r.Command) == "" {
		return Result{}, fmt.Errorf("agent command is empty (set SIMUG_AGENT_CMD)")
	}

	cmd := exec.CommandContext(ctx, "bash", "-lc", r.Command)
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.CombinedOutput()
	raw := string(out)
	if err != nil {
		return Result{}, &RunError{
			Cause:     fmt.Errorf("agent command failed: %w: %s", err, strings.TrimSpace(raw)),
			RawOutput: raw,
		}
	}

	parsed, err := parseRoutedOutput(raw)
	if err != nil {
		return Result{}, &RunError{
			Cause:     err,
			RawOutput: raw,
		}
	}

	terminalCount := 0
	var terminal Action
	for _, a := range parsed.Actions {
		if a.Type == ActionDone || a.Type == ActionIdle {
			terminalCount++
			terminal = a
		}
	}
	if terminalCount != 1 {
		return Result{}, &RunError{
			Cause:     fmt.Errorf("agent protocol requires exactly one terminal action (done or idle), got %d", terminalCount),
			RawOutput: raw,
		}
	}

	return Result{
		RawOutput:        raw,
		Actions:          parsed.Actions,
		Terminal:         terminal,
		ManagerMessages:  parsed.ManagerMessages,
		QuarantinedLines: parsed.QuarantinedLines,
	}, nil
}

func parseActions(raw string) ([]Action, error) {
	parsed, err := parseRoutedOutput(raw)
	if err != nil {
		return nil, err
	}
	return parsed.Actions, nil
}

type parsedOutput struct {
	Actions          []Action
	ManagerMessages  []string
	QuarantinedLines []string
}

func parseRoutedOutput(raw string) (parsedOutput, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	// Allow large protocol payloads while keeping an upper cap.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	out := parsedOutput{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, protocolPrefixCurrent):
			jsonPart := strings.TrimSpace(strings.TrimPrefix(line, protocolPrefixCurrent))
			action, err := parseActionJSON(jsonPart)
			if err != nil {
				return parsedOutput{}, fmt.Errorf("parse protocol line %q: %w", line, err)
			}
			out.Actions = append(out.Actions, action)
		case strings.HasPrefix(line, protocolPrefixManager):
			message := strings.TrimSpace(strings.TrimPrefix(line, protocolPrefixManager))
			if message != "" {
				out.ManagerMessages = append(out.ManagerMessages, message)
			}
		case line != "":
			out.QuarantinedLines = append(out.QuarantinedLines, line)
		default:
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedOutput{}, fmt.Errorf("scan agent output: %w", err)
	}
	if len(out.Actions) == 0 {
		return parsedOutput{}, fmt.Errorf("agent output missing protocol lines (expected lines beginning with %q)", protocolPrefixCurrent)
	}
	return out, nil
}

func parseActionJSON(raw string) (Action, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Action{}, fmt.Errorf("invalid json: %w", err)
	}

	typeValue, ok := payload["action"]
	if !ok {
		return Action{}, fmt.Errorf("missing field action")
	}
	actionName, ok := typeValue.(string)
	if !ok {
		return Action{}, fmt.Errorf("field action must be a string")
	}

	a := Action{Type: ActionType(actionName)}
	switch a.Type {
	case ActionComment:
		a.Body = stringField(payload, "body")
		if strings.TrimSpace(a.Body) == "" {
			return Action{}, fmt.Errorf("comment action requires non-empty body")
		}
	case ActionReply:
		a.Body = stringField(payload, "body")
		if strings.TrimSpace(a.Body) == "" {
			return Action{}, fmt.Errorf("reply action requires non-empty body")
		}
		id, err := int64Field(payload, "comment_id")
		if err != nil {
			return Action{}, fmt.Errorf("reply action invalid comment_id: %w", err)
		}
		a.CommentID = id
	case ActionDone:
		a.Summary = stringField(payload, "summary")
		changes, ok := payload["changes"].(bool)
		if !ok {
			return Action{}, fmt.Errorf("done action requires boolean field changes")
		}
		a.Changes = changes
		a.PRTitle = stringField(payload, "pr_title")
		a.PRBody = stringField(payload, "pr_body")
	case ActionIssueReport:
		issueNumber, err := int64Field(payload, "issue_number")
		if err != nil {
			return Action{}, fmt.Errorf("issue_report action invalid issue_number: %w", err)
		}
		if issueNumber <= 0 {
			return Action{}, fmt.Errorf("issue_report action requires positive issue_number")
		}
		relevant, err := boolField(payload, "relevant")
		if err != nil {
			return Action{}, fmt.Errorf("issue_report action invalid relevant: %w", err)
		}
		needsTask, err := boolField(payload, "needs_task")
		if err != nil {
			return Action{}, fmt.Errorf("issue_report action invalid needs_task: %w", err)
		}
		a.IssueNumber = int(issueNumber)
		a.Relevant = relevant
		a.Analysis = stringField(payload, "analysis")
		a.NeedsTask = needsTask
		a.TaskTitle = stringField(payload, "task_title")
		a.TaskBody = stringField(payload, "task_body")
	case ActionIdle:
		a.Reason = stringField(payload, "reason")
	default:
		return Action{}, fmt.Errorf("unsupported action %q", actionName)
	}

	return a, nil
}

func stringField(payload map[string]any, key string) string {
	v, ok := payload[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func int64Field(payload map[string]any, key string) (int64, error) {
	v, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("missing field %s", key)
	}
	switch vv := v.(type) {
	case float64:
		return int64(vv), nil
	case string:
		id, err := strconv.ParseInt(vv, 10, 64)
		if err != nil {
			return 0, err
		}
		return id, nil
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}

func boolField(payload map[string]any, key string) (bool, error) {
	v, ok := payload[key]
	if !ok {
		return false, fmt.Errorf("missing field %s", key)
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("unsupported type %T", v)
	}
	return b, nil
}
