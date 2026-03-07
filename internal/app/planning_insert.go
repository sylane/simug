package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"simug/internal/agent"
)

var (
	planningTaskLineRe = regexp.MustCompile(`^-\s+\[( |x)\]\s+\*\*(?:\[IN_PROGRESS\]\s+)?Task\s+([0-9]+(?:\.[0-9]+)*[a-z]*):\s+(.+)\*\*$`)
	planningTaskIDRe   = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)*)([a-z]*)$`)
)

type planningTask struct {
	Status string
	ID     string
	Title  string
	Line   int
}

func ensureIssueDerivedPlanningTask(repoRoot string, report agent.Action) (taskID string, inserted bool, err error) {
	title := normalizeOneLine(report.TaskTitle)
	scope := normalizeOneLine(report.TaskBody)
	if title == "" {
		return "", false, fmt.Errorf("issue-derived task title is empty")
	}
	if scope == "" {
		return "", false, fmt.Errorf("issue-derived task scope is empty")
	}
	if report.IssueNumber <= 0 {
		return "", false, fmt.Errorf("issue-derived task requires positive issue number")
	}

	planningPath := filepath.Join(repoRoot, "docs", "PLANNING.md")
	data, err := os.ReadFile(planningPath)
	if err != nil {
		return "", false, fmt.Errorf("read planning file: %w", err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	lines := []string{}
	if trimmed != "" {
		lines = strings.Split(trimmed, "\n")
	}

	tasks := parsePlanningTasks(lines)
	if len(tasks) == 0 {
		return "", false, fmt.Errorf("planning insertion failed: no task entries found")
	}

	if existing := findExistingIssueTask(lines, report.IssueNumber, title); existing != "" {
		return existing, false, nil
	}

	lastDone := planningTask{}
	foundDone := false
	existingIDs := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		existingIDs[task.ID] = struct{}{}
		if task.Status == "x" {
			lastDone = task
			foundDone = true
		}
	}
	if !foundDone {
		return "", false, fmt.Errorf("planning insertion failed: no completed task available for deterministic suffix derivation")
	}

	newID, err := nextIssueDerivedTaskID(lastDone.ID, existingIDs)
	if err != nil {
		return "", false, fmt.Errorf("derive issue task id from %q: %w", lastDone.ID, err)
	}

	insertAt := findTaskBlockEnd(lines, lastDone.Line)
	block := []string{
		fmt.Sprintf("- [ ] **Task %s: %s**", newID, title),
		fmt.Sprintf("  - Scope: %s", scope),
		fmt.Sprintf("  - Done when: issue-derived requirements from issue #%d are implemented and validated.", report.IssueNumber),
		issueTaskSourceLine(report.IssueNumber),
		"",
	}

	updated := make([]string, 0, len(lines)+len(block))
	updated = append(updated, lines[:insertAt]...)
	updated = append(updated, block...)
	updated = append(updated, lines[insertAt:]...)
	encoded := strings.Join(updated, "\n")
	if !strings.HasSuffix(encoded, "\n") {
		encoded += "\n"
	}
	if err := os.WriteFile(planningPath, []byte(encoded), 0o644); err != nil {
		return "", false, fmt.Errorf("write planning file: %w", err)
	}
	return newID, true, nil
}

func parsePlanningTasks(lines []string) []planningTask {
	out := make([]planningTask, 0, len(lines))
	for i, line := range lines {
		m := planningTaskLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, planningTask{
			Status: m[1],
			ID:     m[2],
			Title:  m[3],
			Line:   i,
		})
	}
	return out
}

func findTaskBlockEnd(lines []string, taskLine int) int {
	for i := taskLine + 1; i < len(lines); i++ {
		if planningTaskLineRe.MatchString(lines[i]) {
			return i
		}
		if strings.HasPrefix(lines[i], "## ") {
			return i
		}
	}
	return len(lines)
}

func nextIssueDerivedTaskID(lastDoneID string, existing map[string]struct{}) (string, error) {
	candidate, err := incrementTaskIDSuffix(lastDoneID)
	if err != nil {
		return "", err
	}
	for attempts := 0; attempts < 1024; attempts++ {
		if _, ok := existing[candidate]; !ok {
			return candidate, nil
		}
		candidate, err = incrementTaskIDSuffix(candidate)
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("unable to derive unique task id after 1024 attempts")
}

func incrementTaskIDSuffix(id string) (string, error) {
	m := planningTaskIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", fmt.Errorf("invalid task id format %q", id)
	}
	base := m[1]
	suffix := m[2]
	if suffix == "" {
		return base + "a", nil
	}

	runes := []rune(suffix)
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] < 'a' || runes[i] > 'z' {
			return "", fmt.Errorf("invalid task id suffix %q", suffix)
		}
		if runes[i] < 'z' {
			runes[i]++
			return base + string(runes), nil
		}
		runes[i] = 'a'
	}
	return base + "a" + string(runes), nil
}

func issueTaskSourceLine(issueNumber int) string {
	return fmt.Sprintf("  - Source: issue #%d (`issue_report`)", issueNumber)
}

func findExistingIssueTask(lines []string, issueNumber int, normalizedTitle string) string {
	source := issueTaskSourceLine(issueNumber)
	for i, line := range lines {
		if line != source {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			m := planningTaskLineRe.FindStringSubmatch(lines[j])
			if m != nil {
				if normalizeOneLine(m[3]) == normalizedTitle {
					return m[2]
				}
				break
			}
			if strings.HasPrefix(lines[j], "## ") {
				break
			}
		}
	}
	return ""
}

func normalizeOneLine(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
