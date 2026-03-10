package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"simug/internal/agent"
	"simug/internal/github"
	"simug/internal/state"
)

func (o *orchestrator) processMergedPRIssueFinalization(ctx context.Context, prNumber int) error {
	if prNumber <= 0 {
		return nil
	}
	for i := range o.state.IssueLinks {
		link := &o.state.IssueLinks[i]
		if link.PRNumber != prNumber || link.Finalized {
			continue
		}
		if link.IssueNumber <= 0 || strings.TrimSpace(link.IdempotencyKey) == "" {
			link.Finalized = true
			continue
		}

		issue, err := github.GetIssue(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("read issue #%d for merged PR #%d finalization: %w", link.IssueNumber, prNumber, err)
		}
		if !strings.EqualFold(strings.TrimSpace(issue.Author.Login), strings.TrimSpace(o.user)) {
			o.logEvent("issue_finalize", "skipping merged-PR finalization for issue outside authenticated-user scope", map[string]any{
				"pr":           prNumber,
				"issue":        link.IssueNumber,
				"issue_author": issue.Author.Login,
				"user":         o.user,
				"relation":     link.Relation,
				"key":          link.IdempotencyKey,
			})
			link.Finalized = true
			continue
		}

		marker := issueFinalizationMarker(*link)
		comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("list issue comments for finalization marker on issue #%d: %w", link.IssueNumber, err)
		}
		hasMarker := false
		for _, comment := range comments {
			if comment.User.Login != o.user {
				continue
			}
			if strings.Contains(comment.Body, marker) {
				hasMarker = true
				break
			}
		}

		if hasMarker {
			o.logEvent("issue_finalize", "issue finalization marker already present; skipping duplicate finalization comment", map[string]any{
				"pr":       prNumber,
				"issue":    link.IssueNumber,
				"relation": link.Relation,
				"key":      link.IdempotencyKey,
				"marker":   marker,
			})
		} else {
			body := buildIssueFinalizationCommentBody(o.repo.FullName(), *link)
			o.logEvent("issue_finalize", "posting merged-PR issue finalization comment", map[string]any{
				"pr":       prNumber,
				"issue":    link.IssueNumber,
				"relation": link.Relation,
				"key":      link.IdempotencyKey,
				"marker":   marker,
			})
			if err := github.CommentIssue(ctx, o.repoRoot, link.IssueNumber, limitString(body, 60000)); err != nil {
				return fmt.Errorf("post merged-PR finalization comment on issue #%d: %w", link.IssueNumber, err)
			}
		}

		if strings.EqualFold(strings.TrimSpace(link.Relation), string(agent.IssueRelationFixes)) &&
			strings.EqualFold(strings.TrimSpace(issue.State), "OPEN") {
			o.logEvent("issue_finalize", "closing issue after merged PR fixed relation", map[string]any{
				"pr":    prNumber,
				"issue": link.IssueNumber,
				"key":   link.IdempotencyKey,
			})
			if err := github.CloseIssue(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber); err != nil {
				return fmt.Errorf("close issue #%d after merged PR finalization: %w", link.IssueNumber, err)
			}
		}

		link.Finalized = true
	}
	return nil
}

func (o *orchestrator) ensureIssueTriageComment(ctx context.Context, report agent.Action) error {
	marker := issueTriageMarker(report)
	comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), report.IssueNumber)
	if err != nil {
		return fmt.Errorf("list issue comments for triage marker on issue #%d: %w", report.IssueNumber, err)
	}
	for _, comment := range comments {
		if comment.User.Login != o.user {
			continue
		}
		if strings.Contains(comment.Body, marker) {
			o.logEvent("issue_triage_comment", "triage marker already present; skipping duplicate issue comment", map[string]any{
				"issue":  report.IssueNumber,
				"marker": marker,
			})
			return nil
		}
	}

	body := buildIssueTriageCommentBody(report)
	o.logEvent("issue_triage_comment", "posting deterministic issue triage analysis comment", map[string]any{
		"issue":      report.IssueNumber,
		"marker":     marker,
		"needs_task": report.NeedsTask,
		"relevant":   report.Relevant,
	})
	if err := github.CommentIssue(ctx, o.repoRoot, report.IssueNumber, limitString(body, 60000)); err != nil {
		return fmt.Errorf("post issue triage comment on issue #%d: %w", report.IssueNumber, err)
	}
	return nil
}

func issueTriageMarker(report agent.Action) string {
	return fmt.Sprintf("<!-- simug:issue-triage:v1 issue=%d relevant=%t needs_task=%t -->", report.IssueNumber, report.Relevant, report.NeedsTask)
}

func buildIssueTriageCommentBody(report agent.Action) string {
	var b strings.Builder
	b.WriteString(issueTriageMarker(report))
	b.WriteString("\n")
	b.WriteString("### simug issue triage analysis\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", report.IssueNumber))
	b.WriteString(fmt.Sprintf("- Relevant: %t\n", report.Relevant))
	b.WriteString(fmt.Sprintf("- Needs task: %t\n", report.NeedsTask))
	b.WriteString("\n")
	b.WriteString("Analysis:\n")
	b.WriteString(strings.TrimSpace(report.Analysis))
	b.WriteString("\n")
	if report.NeedsTask {
		b.WriteString("\n")
		b.WriteString("Proposed task title:\n")
		b.WriteString(strings.TrimSpace(report.TaskTitle))
		b.WriteString("\n\n")
		b.WriteString("Proposed task body:\n")
		b.WriteString(strings.TrimSpace(report.TaskBody))
		b.WriteString("\n")
	}
	return b.String()
}

func (o *orchestrator) maybePostIssueDerivedPRBacklink(ctx context.Context, prNumber int, issueNumber int, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if issueNumber <= 0 {
		return nil
	}

	marker := issuePRBacklinkMarker(issueNumber, taskID, prNumber)
	comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), issueNumber)
	if err != nil {
		return fmt.Errorf("list issue comments for PR backlink marker on issue #%d: %w", issueNumber, err)
	}
	for _, comment := range comments {
		if comment.User.Login != o.user {
			continue
		}
		if strings.Contains(comment.Body, marker) {
			o.logEvent("issue_backlink", "PR backlink marker already present; skipping duplicate issue comment", map[string]any{
				"issue":   issueNumber,
				"task_id": taskID,
				"pr":      prNumber,
				"marker":  marker,
			})
			return nil
		}
	}

	body := buildIssuePRBacklinkCommentBody(o.repo.FullName(), issueNumber, taskID, prNumber)
	o.logEvent("issue_backlink", "posting issue-to-PR backlink comment", map[string]any{
		"issue":   issueNumber,
		"task_id": taskID,
		"pr":      prNumber,
		"marker":  marker,
	})
	if err := github.CommentIssue(ctx, o.repoRoot, issueNumber, limitString(body, 60000)); err != nil {
		return fmt.Errorf("post issue-to-PR backlink comment on issue #%d: %w", issueNumber, err)
	}
	return nil
}

func issuePRBacklinkMarker(issueNumber int, taskID string, prNumber int) string {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Sprintf("<!-- simug:issue-pr-link:v1 issue=%d pr=%d -->", issueNumber, prNumber)
	}
	return fmt.Sprintf("<!-- simug:issue-pr-link:v1 issue=%d task=%s pr=%d -->", issueNumber, strings.TrimSpace(taskID), prNumber)
}

func buildIssuePRBacklinkCommentBody(repoFullName string, issueNumber int, taskID string, prNumber int) string {
	url := fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, prNumber)
	var b strings.Builder
	b.WriteString(issuePRBacklinkMarker(issueNumber, taskID, prNumber))
	b.WriteString("\n")
	b.WriteString("### simug implementation PR link\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", issueNumber))
	if strings.TrimSpace(taskID) != "" {
		b.WriteString(fmt.Sprintf("- Task: Task %s\n", strings.TrimSpace(taskID)))
	}
	b.WriteString(fmt.Sprintf("- PR: #%d (%s)\n", prNumber, url))
	return b.String()
}

func (o *orchestrator) processPendingIssueUpdateComments(ctx context.Context, prNumber int) error {
	if prNumber <= 0 {
		return nil
	}
	for i := range o.state.IssueLinks {
		link := &o.state.IssueLinks[i]
		if link.PRNumber != prNumber || link.CommentPosted {
			continue
		}
		if link.IssueNumber <= 0 || strings.TrimSpace(link.IdempotencyKey) == "" {
			continue
		}

		issue, err := github.GetIssue(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("read issue #%d for issue update intent: %w", link.IssueNumber, err)
		}
		if !strings.EqualFold(strings.TrimSpace(issue.Author.Login), strings.TrimSpace(o.user)) {
			o.logEvent("issue_update_comment", "skipping issue update for issue outside authenticated-user scope", map[string]any{
				"pr":           prNumber,
				"issue":        link.IssueNumber,
				"issue_author": issue.Author.Login,
				"user":         o.user,
				"relation":     link.Relation,
				"key":          link.IdempotencyKey,
			})
			continue
		}

		marker := issueUpdateMarker(*link)
		comments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), link.IssueNumber)
		if err != nil {
			return fmt.Errorf("list comments for issue update marker on issue #%d: %w", link.IssueNumber, err)
		}
		exists := false
		for _, comment := range comments {
			if comment.User.Login != o.user {
				continue
			}
			if strings.Contains(comment.Body, marker) {
				exists = true
				break
			}
		}
		if exists {
			link.CommentPosted = true
			o.logEvent("issue_update_comment", "issue update marker already present; marking as posted", map[string]any{
				"pr":       prNumber,
				"issue":    link.IssueNumber,
				"relation": link.Relation,
				"key":      link.IdempotencyKey,
				"marker":   marker,
			})
			continue
		}

		body := buildIssueUpdateCommentBody(o.repo.FullName(), *link, o.currentTaskContextID())
		o.logEvent("issue_update_comment", "posting issue update comment from tracked linkage intent", map[string]any{
			"pr":       prNumber,
			"issue":    link.IssueNumber,
			"relation": link.Relation,
			"key":      link.IdempotencyKey,
			"marker":   marker,
		})
		if err := github.CommentIssue(ctx, o.repoRoot, link.IssueNumber, limitString(body, 60000)); err != nil {
			return fmt.Errorf("post issue update comment on issue #%d: %w", link.IssueNumber, err)
		}
		link.CommentPosted = true
	}
	return nil
}

func issueUpdateMarker(link state.IssueLink) string {
	return fmt.Sprintf(
		"<!-- simug:issue-update:v1 issue=%d relation=%s key=%s pr=%d -->",
		link.IssueNumber,
		strings.TrimSpace(link.Relation),
		strings.TrimSpace(link.IdempotencyKey),
		link.PRNumber,
	)
}

func issueFinalizationMarker(link state.IssueLink) string {
	return fmt.Sprintf(
		"<!-- simug:issue-finalize:v1 issue=%d relation=%s key=%s pr=%d -->",
		link.IssueNumber,
		strings.TrimSpace(link.Relation),
		strings.TrimSpace(link.IdempotencyKey),
		link.PRNumber,
	)
}

func (o *orchestrator) currentTaskContextID() string {
	if taskRef := strings.TrimSpace(o.state.ActiveTaskRef); taskRef != "" {
		if taskID, err := parseTaskIDFromRef(taskRef); err == nil {
			return taskID
		}
	}
	if taskID := strings.TrimSpace(o.state.PendingTaskID); taskID != "" {
		return taskID
	}
	return ""
}

func buildIssueUpdateCommentBody(repoFullName string, link state.IssueLink, taskID string) string {
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, link.PRNumber)
	var b strings.Builder
	b.WriteString(issueUpdateMarker(link))
	b.WriteString("\n")
	b.WriteString("### simug issue linkage update\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", link.IssueNumber))
	b.WriteString(fmt.Sprintf("- Relation: %s\n", strings.TrimSpace(link.Relation)))
	b.WriteString(fmt.Sprintf("- PR: #%d (%s)\n", link.PRNumber, prURL))
	if strings.TrimSpace(taskID) != "" {
		b.WriteString(fmt.Sprintf("- Task context: Task %s\n", strings.TrimSpace(taskID)))
	}
	b.WriteString("\n")
	b.WriteString("Context:\n")
	b.WriteString(strings.TrimSpace(link.CommentBody))
	b.WriteString("\n")
	return b.String()
}

func buildIssueFinalizationCommentBody(repoFullName string, link state.IssueLink) string {
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, link.PRNumber)
	relation := strings.TrimSpace(link.Relation)
	var b strings.Builder
	b.WriteString(issueFinalizationMarker(link))
	b.WriteString("\n")
	b.WriteString("### simug merged PR issue finalization\n")
	b.WriteString(fmt.Sprintf("- Issue: #%d\n", link.IssueNumber))
	b.WriteString(fmt.Sprintf("- Relation: %s\n", relation))
	b.WriteString(fmt.Sprintf("- PR: #%d (%s)\n", link.PRNumber, prURL))
	if strings.EqualFold(relation, string(agent.IssueRelationFixes)) {
		b.WriteString("- Outcome: closing issue because this PR is marked as a fix\n")
	} else {
		b.WriteString("- Outcome: informational update only (issue remains open)\n")
	}
	b.WriteString("\n")
	b.WriteString("Context:\n")
	b.WriteString(strings.TrimSpace(link.CommentBody))
	b.WriteString("\n")
	return b.String()
}

func (o *orchestrator) pollEvents(ctx context.Context, prNumber int) (eventPoll, error) {
	issueComments, err := github.ListIssueComments(ctx, o.repoRoot, o.repo.FullName(), prNumber)
	if err != nil {
		return eventPoll{}, fmt.Errorf("list issue comments: %w", err)
	}
	reviewComments, err := github.ListReviewComments(ctx, o.repoRoot, o.repo.FullName(), prNumber)
	if err != nil {
		return eventPoll{}, fmt.Errorf("list review comments: %w", err)
	}
	reviews, err := github.ListReviews(ctx, o.repoRoot, o.repo.FullName(), prNumber)
	if err != nil {
		return eventPoll{}, fmt.Errorf("list reviews: %w", err)
	}

	out := eventPoll{
		IssueByID:  make(map[int64]event),
		ReviewByID: make(map[int64]event),
	}

	for _, c := range issueComments {
		out.MaxIssueID = maxInt64(out.MaxIssueID, c.ID)
		if !o.state.CursorUncertain && c.ID <= o.state.LastIssueCommentID {
			continue
		}
		e := event{Source: "issue_comment", ID: c.ID, Author: c.User.Login, Body: limitString(c.Body, maxCommentBodyChars), CreatedAt: c.CreatedAt}
		e.Commands, e.UnauthorizedCommands = parseAgentCommands(e.Body, e.Author, o.cfg.AllowedUsers, o.cfg.AllowedVerbs)
		out.Events = append(out.Events, e)
		out.IssueByID[c.ID] = e
	}

	for _, c := range reviewComments {
		out.MaxReviewComID = maxInt64(out.MaxReviewComID, c.ID)
		if !o.state.CursorUncertain && c.ID <= o.state.LastReviewCommentID {
			continue
		}
		e := event{
			Source:    "review_comment",
			ID:        c.ID,
			Author:    c.User.Login,
			Body:      limitString(c.Body, maxCommentBodyChars),
			CreatedAt: c.CreatedAt,
			ReviewContext: &reviewCommentContext{
				Path:         strings.TrimSpace(c.Path),
				DiffHunk:     strings.TrimSpace(c.DiffHunk),
				Line:         c.Line,
				OriginalLine: c.OriginalLine,
				Side:         strings.TrimSpace(c.Side),
				StartLine:    c.StartLine,
				StartSide:    strings.TrimSpace(c.StartSide),
			},
		}
		e.Commands, e.UnauthorizedCommands = parseAgentCommands(e.Body, e.Author, o.cfg.AllowedUsers, o.cfg.AllowedVerbs)
		out.Events = append(out.Events, e)
		out.ReviewByID[c.ID] = e
	}

	for _, r := range reviews {
		out.MaxReviewID = maxInt64(out.MaxReviewID, r.ID)
		if !o.state.CursorUncertain && r.ID <= o.state.LastReviewID {
			continue
		}
		createdAt := time.Now().UTC()
		if r.SubmittedAt != nil {
			createdAt = *r.SubmittedAt
		}
		e := event{Source: "review", ID: r.ID, Author: r.User.Login, Body: limitString(r.Body, maxCommentBodyChars), CreatedAt: createdAt}
		e.Commands, e.UnauthorizedCommands = parseAgentCommands(e.Body, e.Author, o.cfg.AllowedUsers, o.cfg.AllowedVerbs)
		out.Events = append(out.Events, e)
	}

	sort.Slice(out.Events, func(i, j int) bool {
		if out.Events[i].CreatedAt.Equal(out.Events[j].CreatedAt) {
			return out.Events[i].ID < out.Events[j].ID
		}
		return out.Events[i].CreatedAt.Before(out.Events[j].CreatedAt)
	})

	return out, nil
}

func (o *orchestrator) applyActions(ctx context.Context, prNumber int, actions []agent.Action, poll eventPoll) error {
	for _, a := range actions {
		switch a.Type {
		case agent.ActionComment:
			o.logEvent("github_comment", "posting PR comment", map[string]any{"pr": prNumber})
			if err := github.CommentPR(ctx, o.repoRoot, prNumber, limitString(strings.TrimSpace(a.Body), 60000)); err != nil {
				return fmt.Errorf("post PR comment: %w", err)
			}
		case agent.ActionReply:
			body := limitString(strings.TrimSpace(a.Body), 60000)
			if replyEvent, ok := poll.ReviewByID[a.CommentID]; ok {
				o.logEvent("github_reply", "replying to review comment", map[string]any{"pr": prNumber, "comment_id": replyEvent.ID})
				if err := github.ReplyToReviewComment(ctx, o.repoRoot, o.repo.FullName(), prNumber, replyEvent.ID, body); err != nil {
					return fmt.Errorf("reply to review comment %d: %w", replyEvent.ID, err)
				}
				continue
			}

			if issueEvent, ok := poll.IssueByID[a.CommentID]; ok {
				mention := ""
				if issueEvent.Author != "" {
					mention = "@" + issueEvent.Author + " "
				}
				o.logEvent("github_reply_fallback", "replying through PR comment fallback", map[string]any{"pr": prNumber, "comment_id": issueEvent.ID})
				if err := github.CommentPR(ctx, o.repoRoot, prNumber, mention+body); err != nil {
					return fmt.Errorf("fallback reply for issue comment %d: %w", issueEvent.ID, err)
				}
				continue
			}

			o.logEvent("github_reply_fallback", "reply target not found, posting regular comment", map[string]any{"pr": prNumber, "comment_id": a.CommentID})
			if err := github.CommentPR(ctx, o.repoRoot, prNumber, body); err != nil {
				return fmt.Errorf("reply fallback comment for unknown comment id %d: %w", a.CommentID, err)
			}
		case agent.ActionDone, agent.ActionIdle:
			// Terminal actions are consumed by orchestrator state flow.
		case agent.ActionIssueUpdate:
			key := issueUpdateIdempotencyKey(prNumber, a)
			if o.hasIssueLinkByKey(key) {
				o.logEvent("issue_update_intent", "duplicate issue update intent already tracked", map[string]any{
					"pr":    prNumber,
					"issue": a.IssueNumber,
					"key":   key,
				})
				continue
			}
			link := state.IssueLink{
				PRNumber:       prNumber,
				IssueNumber:    a.IssueNumber,
				Relation:       string(a.Relation),
				CommentBody:    strings.TrimSpace(a.CommentBody),
				Provenance:     fmt.Sprintf("run=%s tick=%d", o.runID, o.tickSeq),
				IdempotencyKey: key,
				RecordedAt:     time.Now().UTC(),
			}
			o.state.IssueLinks = append(o.state.IssueLinks, link)
			o.logEvent("issue_update_intent", "tracked issue update intent in worker state", map[string]any{
				"pr":           prNumber,
				"issue":        a.IssueNumber,
				"relation":     string(a.Relation),
				"key":          key,
				"comment_tail": tailString(strings.TrimSpace(a.CommentBody), 200),
			})
		default:
			return fmt.Errorf("unsupported action type %q", a.Type)
		}
	}
	return nil
}

func (o *orchestrator) hasIssueLinkByKey(key string) bool {
	for _, link := range o.state.IssueLinks {
		if strings.TrimSpace(link.IdempotencyKey) == strings.TrimSpace(key) {
			return true
		}
	}
	return false
}

func issueUpdateIdempotencyKey(prNumber int, action agent.Action) string {
	canonical := fmt.Sprintf(
		"pr=%d|issue=%d|relation=%s|comment=%s",
		prNumber,
		action.IssueNumber,
		strings.TrimSpace(string(action.Relation)),
		normalizeOneLine(action.CommentBody),
	)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
