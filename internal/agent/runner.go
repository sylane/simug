package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	protocolPrefixCurrent = "SIMUG:"
)

type ActionType string

const (
	ActionComment ActionType = "comment"
	ActionReply   ActionType = "reply"
	ActionDone    ActionType = "done"
	ActionIdle    ActionType = "idle"
)

type Action struct {
	Type      ActionType
	Body      string
	CommentID int64
	Summary   string
	Changes   bool
	PRTitle   string
	PRBody    string
	Reason    string
}

type Result struct {
	RawOutput string
	Actions   []Action
	Terminal  Action
}

type Runner struct {
	Command string
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
		return Result{}, fmt.Errorf("agent command failed: %w: %s", err, strings.TrimSpace(raw))
	}

	actions, err := parseActions(raw)
	if err != nil {
		return Result{}, err
	}

	terminalCount := 0
	var terminal Action
	for _, a := range actions {
		if a.Type == ActionDone || a.Type == ActionIdle {
			terminalCount++
			terminal = a
		}
	}
	if terminalCount != 1 {
		return Result{}, fmt.Errorf("agent protocol requires exactly one terminal action (done or idle), got %d", terminalCount)
	}

	return Result{RawOutput: raw, Actions: actions, Terminal: terminal}, nil
}

func parseActions(raw string) ([]Action, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	// Allow large protocol payloads while keeping an upper cap.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var actions []Action
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, protocolPrefixCurrent) {
			continue
		}
		jsonPart := strings.TrimSpace(strings.TrimPrefix(line, protocolPrefixCurrent))
		action, err := parseActionJSON(jsonPart)
		if err != nil {
			return nil, fmt.Errorf("parse protocol line %q: %w", line, err)
		}
		actions = append(actions, action)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan agent output: %w", err)
	}
	if len(actions) == 0 {
		return nil, fmt.Errorf("agent output missing protocol lines (expected lines beginning with %q)", protocolPrefixCurrent)
	}
	return actions, nil
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
