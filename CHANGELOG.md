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
- Issue-to-PR backlink comments after issue-derived PR creation, with deterministic backlink markers and authenticated-author duplicate suppression.
- Expanded issue-first regression coverage for insertion failures, spoofed-marker handling, pending-task bootstrap targeting, and fallback/no-actionable-issue paths.
- Added one-shot runtime mode (`simug run --once`) with explicit CLI parsing and exit codes for supervisor-friendly single-tick self-host loops.
- Added `scripts/self-host-loop.sh` wrapper to rebuild/run one-shot iterations, snapshot `.simug/state.json`, and capture per-iteration logs under `.simug/selfhost/<timestamp>/`.
- Added protocol support for `issue_update` actions so Codex can declare implementation-time issue linkage intents (`fixes`/`impacts`/`relates`) in machine-parseable form for orchestrator processing.
- Added PR-scoped issue linkage ledger persistence in `.simug/state.json` (`issue_links`) with deterministic idempotency keys so restart/retry preserves issue intent context.
- Added orchestrator-owned implementation-time issue update comments from tracked issue linkage intents, with same-user scope checks and deterministic marker-based duplicate suppression.
- Added merged-PR issue finalization workflow that rehydrates inactive PR state, posts idempotent finalization comments (`simug:issue-finalize:v1`), closes `fixes` issues, and marks tracked issue links finalized.
- Added lifecycle/adversarial integration coverage for merged-finalization scope rejection, malformed issue linkage payload rejection, and restart-safe progression from managed issue updates to merge finalization.
- Added crash-safe `in_flight_attempt` journaling in worker state, persisted before/after each Codex invocation with prompt hash, mode, branch, attempt index, pre/post head, and error context.
- Added deterministic restart recovery state machine (`resume`/`replay`/`repair`/`abort`) that consumes persisted attempt journals, records `last_recovery`, and fail-closes on dirty/inconsistent recovery invariants.
- Added failure-injection integration coverage for startup recovery transitions, fetch-origin failures in no-PR intake, and merged-finalization close-issue failures.
- Added real-Codex protocol conformance canary harness (`TestRealCodexProtocolConformanceCanary`) plus `scripts/canary-real-codex-protocol.sh` for repeatable runtime validation with archived artifacts.
- Added real-Codex repair/restart canary harness (`TestRealCodexRepairInteractionCanary`, `TestRealCodexRestartRecoveryBoundaryCanary`) plus `scripts/canary-real-codex-recovery.sh`.
- Added `scripts/canary-real-codex-gate.sh` and `docs/REAL_CODEX_GATE.md` to make protocol+recovery canaries an auditable operator/release validation gate with retention policy.

### Changed

- Reworked planning roadmap by inserting a dedicated maintainability/modularity/safety hardening phase between self-host readiness and environment/release readiness, and renumbered later phases accordingly.
- Clarified design/README ownership boundary that orchestrator should not directly edit project planning/workflow/source files; repository content updates are Codex-authored through normal commits.
- Extended issue roadmap with post-triage lifecycle tasks (protocol linkage during implementation, PR-scoped issue ledger, orchestrator-owned issue updates, and close-on-merge finalization).
- Added design-first execution ordering in planning to prioritize ownership-boundary and issue-lifecycle completion before remaining self-hosting milestones.
- Expanded design document with feasibility assessment, explicit known gaps, issue lifecycle target behavior, and mode-to-action handling matrix.
