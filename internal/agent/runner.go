package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
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
	ActionIssueUpdate ActionType = "issue_update"
	ActionDone        ActionType = "done"
	ActionIdle        ActionType = "idle"
)

type IssueRelation string

const (
	IssueRelationFixes   IssueRelation = "fixes"
	IssueRelationImpacts IssueRelation = "impacts"
	IssueRelationRelates IssueRelation = "relates"
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
	Relation    IssueRelation
	CommentBody string
}

type Result struct {
	RawOutput        string
	Actions          []Action
	Terminal         Action
	ManagerMessages  []string
	QuarantinedLines []string
	Turn             CoordinatorTurn
}

type ProtocolForensics struct {
	RawProtocolLines     []string
	ActiveProtocolLines  []string
	IgnoredProtocolLines []string
	AcceptedTurn         CoordinatorTurn
}

type StreamKind string

const (
	StreamKindProtocol   StreamKind = "protocol"
	StreamKindManager    StreamKind = "manager"
	StreamKindDiagnostic StreamKind = "diagnostic"
)

type StreamLine struct {
	Kind StreamKind
	Line string
}

type CoordinatorTurn struct {
	TurnID    string
	SessionID string
}

type Runner struct {
	Command string
	OnLine  func(StreamLine)
	Turn    CoordinatorTurn
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
	if strings.TrimSpace(r.Turn.TurnID) != "" {
		cmd.Env = append(os.Environ(),
			"SIMUG_PROTOCOL_TURN_ID="+strings.TrimSpace(r.Turn.TurnID),
			"SIMUG_PROTOCOL_SESSION_ID="+strings.TrimSpace(r.Turn.SessionID),
		)
	}
	output := newStreamBuffer(r.OnLine)
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	output.Flush()
	raw := output.RawOutput()
	parsed, parseErr := parseRoutedOutput(raw, r.Turn)
	if parseErr == nil {
		if strings.TrimSpace(r.Turn.TurnID) == "" {
			parsed.Actions = removePromptTemplateEchoSequences(parsed.Actions)
			parsed.Actions = collapseDuplicateTerminalSequences(parsed.Actions)
		}
	}

	if err != nil {
		if parseErr == nil {
			result, resultErr := buildResultFromParsed(raw, parsed, r.Turn)
			if resultErr == nil {
				return result, nil
			}
			parseErr = resultErr
		}

		cause := fmt.Errorf("agent command failed: %w: %s", err, strings.TrimSpace(raw))
		if hint := CodexRuntimeHint(r.Command, raw); hint != "" {
			cause = fmt.Errorf("%w | hint: %s", cause, hint)
		}
		if parseErr != nil {
			cause = fmt.Errorf("%w | protocol recovery failed: %v", cause, parseErr)
		}
		return Result{}, &RunError{
			Cause:     cause,
			RawOutput: raw,
		}
	}
	if parseErr != nil {
		return Result{}, &RunError{
			Cause:     parseErr,
			RawOutput: raw,
		}
	}
	return buildResultFromParsed(raw, parsed, r.Turn)
}

type streamBuffer struct {
	onLine func(StreamLine)

	mu      sync.Mutex
	raw     bytes.Buffer
	partial bytes.Buffer
}

func newStreamBuffer(onLine func(StreamLine)) *streamBuffer {
	return &streamBuffer{onLine: onLine}
}

func (b *streamBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	_, _ = b.raw.Write(p)
	lines := make([]StreamLine, 0, 1)
	for _, ch := range p {
		_ = b.partial.WriteByte(ch)
		if ch == '\n' {
			if line, ok := classifyStreamLine(b.partial.Bytes()); ok {
				lines = append(lines, line)
			}
			b.partial.Reset()
		}
	}
	b.mu.Unlock()

	b.emit(lines)
	return len(p), nil
}

func (b *streamBuffer) Flush() {
	b.mu.Lock()
	var lines []StreamLine
	if b.partial.Len() > 0 {
		if line, ok := classifyStreamLine(b.partial.Bytes()); ok {
			lines = append(lines, line)
		}
		b.partial.Reset()
	}
	b.mu.Unlock()

	b.emit(lines)
}

func (b *streamBuffer) RawOutput() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.raw.String()
}

func (b *streamBuffer) emit(lines []StreamLine) {
	if b == nil || b.onLine == nil {
		return
	}
	for _, line := range lines {
		b.onLine(line)
	}
}

func classifyStreamLine(raw []byte) (StreamLine, bool) {
	line := strings.TrimRight(string(raw), "\r\n")
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return StreamLine{}, false
	}

	kind := StreamKindDiagnostic
	switch {
	case strings.HasPrefix(trimmed, protocolPrefixCurrent):
		kind = StreamKindProtocol
	case strings.HasPrefix(trimmed, protocolPrefixManager):
		kind = StreamKindManager
	}
	return StreamLine{Kind: kind, Line: line}, true
}

func parseActions(raw string) ([]Action, error) {
	parsed, err := parseRoutedOutput(raw, CoordinatorTurn{})
	if err != nil {
		return nil, err
	}
	return parsed.Actions, nil
}

func CollectProtocolForensics(raw string, turn CoordinatorTurn) ProtocolForensics {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	forensics := ProtocolForensics{}
	if strings.TrimSpace(turn.TurnID) == "" {
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, protocolPrefixCurrent) {
				forensics.RawProtocolLines = append(forensics.RawProtocolLines, line)
				forensics.ActiveProtocolLines = append(forensics.ActiveProtocolLines, line)
			}
		}
		return forensics
	}

	active := false
	completed := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, protocolPrefixCurrent) {
			continue
		}
		forensics.RawProtocolLines = append(forensics.RawProtocolLines, line)

		jsonPart := strings.TrimSpace(strings.TrimPrefix(line, protocolPrefixCurrent))
		envelope, err := parseCoordinatorEnvelope(jsonPart)
		if err != nil {
			if active && !completed {
				forensics.ActiveProtocolLines = append(forensics.ActiveProtocolLines, line)
			} else {
				forensics.IgnoredProtocolLines = append(forensics.IgnoredProtocolLines, line)
			}
			continue
		}
		if !coordinatorEnvelopeMatchesTurn(envelope, turn) {
			forensics.IgnoredProtocolLines = append(forensics.IgnoredProtocolLines, line)
			continue
		}

		switch envelope.Event {
		case "begin":
			if !active && !completed {
				active = true
				forensics.AcceptedTurn = CoordinatorTurn{TurnID: envelope.TurnID, SessionID: envelope.Session}
				forensics.ActiveProtocolLines = append(forensics.ActiveProtocolLines, line)
				continue
			}
			if active && !completed {
				forensics.ActiveProtocolLines = append(forensics.ActiveProtocolLines, line)
				continue
			}
			forensics.IgnoredProtocolLines = append(forensics.IgnoredProtocolLines, line)
		case "action":
			if active && !completed {
				forensics.ActiveProtocolLines = append(forensics.ActiveProtocolLines, line)
			} else {
				forensics.IgnoredProtocolLines = append(forensics.IgnoredProtocolLines, line)
			}
		case "end":
			if active && !completed {
				forensics.ActiveProtocolLines = append(forensics.ActiveProtocolLines, line)
				completed = true
			} else {
				forensics.IgnoredProtocolLines = append(forensics.IgnoredProtocolLines, line)
			}
		default:
			if active && !completed {
				forensics.ActiveProtocolLines = append(forensics.ActiveProtocolLines, line)
			} else {
				forensics.IgnoredProtocolLines = append(forensics.IgnoredProtocolLines, line)
			}
		}
	}
	return forensics
}

type parsedOutput struct {
	Actions          []Action
	ManagerMessages  []string
	QuarantinedLines []string
}

func parseRoutedOutput(raw string, turn CoordinatorTurn) (parsedOutput, error) {
	if strings.TrimSpace(turn.TurnID) != "" {
		return parseTurnBoundedOutput(raw, turn)
	}
	return parseLegacyRoutedOutput(raw)
}

func parseLegacyRoutedOutput(raw string) (parsedOutput, error) {
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

type coordinatorEnvelope struct {
	Envelope string          `json:"envelope"`
	Event    string          `json:"event"`
	TurnID   string          `json:"turn_id"`
	Session  string          `json:"session_id,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

func parseTurnBoundedOutput(raw string, turn CoordinatorTurn) (parsedOutput, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	out := parsedOutput{}
	active := false
	completed := false
	sawBegin := false
	sawEnd := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, protocolPrefixCurrent):
			jsonPart := strings.TrimSpace(strings.TrimPrefix(line, protocolPrefixCurrent))
			envelope, err := parseCoordinatorEnvelope(jsonPart)
			if err != nil {
				if active && !completed {
					return parsedOutput{}, fmt.Errorf("parse active coordinator line %q: %w", line, err)
				}
				continue
			}
			if !coordinatorEnvelopeMatchesTurn(envelope, turn) {
				continue
			}
			switch envelope.Event {
			case "begin":
				if completed {
					continue
				}
				if active {
					return parsedOutput{}, fmt.Errorf("duplicate active coordinator begin for turn %q", strings.TrimSpace(turn.TurnID))
				}
				active = true
				sawBegin = true
			case "action":
				if !active || completed {
					continue
				}
				action, err := parseActionJSON(strings.TrimSpace(string(envelope.Payload)))
				if err != nil {
					return parsedOutput{}, fmt.Errorf("parse active coordinator action %q: %w", line, err)
				}
				out.Actions = append(out.Actions, action)
			case "end":
				if !active || completed {
					continue
				}
				completed = true
				sawEnd = true
			default:
				if active && !completed {
					return parsedOutput{}, fmt.Errorf("unsupported coordinator envelope event %q", envelope.Event)
				}
			}
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
	if !sawBegin {
		return parsedOutput{}, fmt.Errorf("agent output missing active coordinator envelope begin for turn %q", strings.TrimSpace(turn.TurnID))
	}
	if !sawEnd {
		return parsedOutput{}, fmt.Errorf("active coordinator envelope missing end event for turn %q", strings.TrimSpace(turn.TurnID))
	}
	if len(out.Actions) == 0 {
		return parsedOutput{}, fmt.Errorf("active coordinator envelope for turn %q contained no action payloads", strings.TrimSpace(turn.TurnID))
	}
	return out, nil
}

func parseCoordinatorEnvelope(raw string) (coordinatorEnvelope, error) {
	var envelope coordinatorEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return coordinatorEnvelope{}, fmt.Errorf("invalid json: %w", err)
	}
	if strings.TrimSpace(envelope.Envelope) != "coordinator" {
		return coordinatorEnvelope{}, fmt.Errorf("missing or invalid envelope")
	}
	if strings.TrimSpace(envelope.TurnID) == "" {
		return coordinatorEnvelope{}, fmt.Errorf("missing field turn_id")
	}
	switch strings.TrimSpace(envelope.Event) {
	case "begin", "end":
		return envelope, nil
	case "action":
		if len(bytes.TrimSpace(envelope.Payload)) == 0 {
			return coordinatorEnvelope{}, fmt.Errorf("action envelope requires non-empty payload")
		}
		return envelope, nil
	default:
		return coordinatorEnvelope{}, fmt.Errorf("missing or invalid event")
	}
}

func coordinatorEnvelopeMatchesTurn(envelope coordinatorEnvelope, turn CoordinatorTurn) bool {
	if strings.TrimSpace(envelope.TurnID) != strings.TrimSpace(turn.TurnID) {
		return false
	}
	return strings.TrimSpace(envelope.Session) == strings.TrimSpace(turn.SessionID)
}

func removePromptTemplateEchoSequences(actions []Action) []Action {
	sequences, complete := splitTerminalSequences(actions)
	if !complete || len(sequences) < 2 {
		return actions
	}

	keep := make([][]Action, 0, len(sequences))
	removedTemplate := false
	for _, sequence := range sequences {
		if isPromptTemplateSequence(sequence) {
			removedTemplate = true
			continue
		}
		keep = append(keep, sequence)
	}
	if !removedTemplate || len(keep) == 0 {
		return actions
	}

	filtered := make([]Action, 0, len(actions))
	for _, sequence := range keep {
		filtered = append(filtered, sequence...)
	}
	return filtered
}

func splitTerminalSequences(actions []Action) ([][]Action, bool) {
	terminalIndexes := make([]int, 0, 2)
	for i, a := range actions {
		if a.Type == ActionDone || a.Type == ActionIdle {
			terminalIndexes = append(terminalIndexes, i)
		}
	}
	if len(terminalIndexes) == 0 {
		return nil, false
	}

	sequences := make([][]Action, 0, len(terminalIndexes))
	start := 0
	for _, terminalIndex := range terminalIndexes {
		sequence := make([]Action, terminalIndex-start+1)
		copy(sequence, actions[start:terminalIndex+1])
		sequences = append(sequences, sequence)
		start = terminalIndex + 1
	}
	return sequences, start == len(actions)
}

func isPromptTemplateSequence(sequence []Action) bool {
	if len(sequence) == 0 {
		return false
	}
	for _, action := range sequence {
		if !isPromptTemplateAction(action) {
			return false
		}
	}
	return true
}

func isPromptTemplateAction(action Action) bool {
	switch action.Type {
	case ActionComment:
		return action.Body == "..." ||
			action.Body == `INTENT_JSON:{"task_ref":"Task 7.2a","summary":"...","branch_slug":"intent-handshake","pr_title":"...","pr_body":"...","checks":["GOCACHE=/tmp/go-build go test ./..."]}`
	case ActionReply:
		return action.CommentID == 123 && action.Body == "..."
	case ActionIssueReport:
		return action.IssueNumber == 123 &&
			action.Relevant &&
			action.Analysis == "..." &&
			action.NeedsTask &&
			action.TaskTitle == "..." &&
			action.TaskBody == "..."
	case ActionIssueUpdate:
		return action.IssueNumber == 123 &&
			(action.Relation == IssueRelationFixes || action.Relation == IssueRelationImpacts || action.Relation == IssueRelationRelates) &&
			(action.CommentBody == "Task implementation covers this issue because ..." ||
				action.CommentBody == "This task has direct impact on this issue because ..." ||
				action.CommentBody == "This work affects this issue because ...")
	case ActionDone:
		if action.Summary == "..." && action.Changes &&
			(action.PRTitle == "" || action.PRTitle == "optional") &&
			(action.PRBody == "" || action.PRBody == "optional") {
			return true
		}
		return !action.Changes && (action.Summary == "issue triaged" || action.Summary == "intent prepared")
	case ActionIdle:
		return action.Reason == "..." || action.Reason == "no task available"
	default:
		return false
	}
}

func collapseDuplicateTerminalSequences(actions []Action) []Action {
	sequences, complete := splitTerminalSequences(actions)
	if !complete || len(sequences) < 2 {
		return actions
	}

	last := sequences[len(sequences)-1]
	for i := 0; i < len(sequences)-1; i++ {
		if !actionSequenceEqual(sequences[i], last) {
			return actions
		}
	}
	return last
}

func actionSequenceEqual(a, b []Action) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func buildResultFromParsed(raw string, parsed parsedOutput, turn CoordinatorTurn) (Result, error) {
	terminalCount := 0
	var terminal Action
	for _, a := range parsed.Actions {
		if a.Type == ActionDone || a.Type == ActionIdle {
			terminalCount++
			terminal = a
		}
	}
	if terminalCount != 1 {
		return Result{}, fmt.Errorf("agent protocol requires exactly one terminal action (done or idle), got %d", terminalCount)
	}

	return Result{
		RawOutput:        raw,
		Actions:          parsed.Actions,
		Terminal:         terminal,
		ManagerMessages:  parsed.ManagerMessages,
		QuarantinedLines: parsed.QuarantinedLines,
		Turn:             turn,
	}, nil
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
	case ActionIssueUpdate:
		issueNumber, err := int64Field(payload, "issue_number")
		if err != nil {
			return Action{}, fmt.Errorf("issue_update action invalid issue_number: %w", err)
		}
		if issueNumber <= 0 {
			return Action{}, fmt.Errorf("issue_update action requires positive issue_number")
		}
		relation := IssueRelation(strings.TrimSpace(stringField(payload, "relation")))
		if !isValidIssueRelation(relation) {
			return Action{}, fmt.Errorf("issue_update action invalid relation %q", relation)
		}
		commentBody := stringField(payload, "comment")
		if strings.TrimSpace(commentBody) == "" {
			return Action{}, fmt.Errorf("issue_update action requires non-empty comment")
		}
		a.IssueNumber = int(issueNumber)
		a.Relation = relation
		a.CommentBody = commentBody
	case ActionIdle:
		a.Reason = stringField(payload, "reason")
	default:
		return Action{}, fmt.Errorf("unsupported action %q", actionName)
	}

	return a, nil
}

func isValidIssueRelation(relation IssueRelation) bool {
	switch relation {
	case IssueRelationFixes, IssueRelationImpacts, IssueRelationRelates:
		return true
	default:
		return false
	}
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
