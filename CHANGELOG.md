# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog:
https://keepachangelog.com/en/1.1.0/

## [Unreleased]

### Added

- Initial changelog scaffold for `simug` task-based development workflow.
- High-fidelity orchestrator tracing in `.simug/events.log` with per-run/per-tick command traces (`git`/`gh`), exit codes, stdout/stderr tails, and explicit invariant decision events.
- Per-attempt Codex archive artifacts under `.simug/archive/agent/...` storing prompt input, raw output, and metadata linked to run/tick/attempt context.
- New `simug explain-last-failure` command to summarize the latest failed tick with violated invariant context and suggested next action using event and archive artifacts.
- Prompt contract regression tests for managed/bootstrap/repair prompt builders to catch drift in required protocol rules (`SIMUG:` lines, terminal-action rule, no-push constraints).
- Prompt-driven protocol matrix tests covering mixed stdout with valid, malformed JSON, missing-terminal, and multi-terminal Codex response classes.
- Prompt-tuning harness tests for repeatable failure corpus scenarios, plus bounded retry handling for Codex execution/protocol failures via repair prompts.
- Dual-entity routing contract coverage for `SIMUG:` (coordinator) and `SIMUG_MANAGER:` (manager), with deterministic quarantine of ambiguous unprefixed output lines.
- Prompt-injection resilience fixtures covering malicious GitHub text and channel-prefix abuse attempts, ensuring only authorized `/agent` directives are actionable.
- Explicit persisted loop-mode state model (`managed_pr`, `issue_triage`, `task_bootstrap`) with deterministic mode transitions and restart-safe metadata fields (`active_issue`, `pending_task_id`).
- Authored open-issue discovery for no-PR intake via `gh api`, with PR filtering, deterministic oldest-first selection (lowest issue number), and persisted `active_issue` candidate tracking across issue-first bootstrap fallback.
- Issue-triage prompt path with strict `issue_report` protocol parsing/validation (mode-compatible actions, selected issue matching, non-empty analysis, task metadata requirements) plus integration coverage for valid and invalid triage flows.
- Orchestrator-owned issue triage analysis comments with deterministic marker metadata (`simug:issue-triage:v1`) and duplicate-skip idempotency checks before posting.
- Deterministic planning insertion engine for `issue_report.needs_task=true`, adding issue-derived TODO entries after the last DONE task with suffix-based IDs (`...a`, `...b`, ...), markdown-structure guards, source markers, and persisted `pending_task_id`.
- Bootstrap prompt targeting for issue-derived flow: when `pending_task_id` is present, simug skips re-triage and instructs Codex to start explicitly from that task before other backlog items.
