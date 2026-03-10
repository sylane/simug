# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog:
https://keepachangelog.com/en/1.1.0/

## [Unreleased]

### Added

- Initial changelog scaffold for `simug` task-based development workflow.
- Added opt-in `simug run --verbose` / `-v` console tracing that mirrors per-attempt Codex prompts and live tagged Codex output (`codex[manager]`, `codex[protocol]`, `codex[raw]`) while preserving archived raw output for parsing and forensics.
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
- Added sandbox dry-run runbook + verifier (`docs/SANDBOX_DRY_RUN.md`, `scripts/sandbox-dry-run.sh`) to validate merged issue-driven and planning-driven PR evidence in real GitHub environments.
- Added end-to-end self-host canary workflow (`scripts/self-host-canary.sh`, `docs/SELF_HOST_CANARY.md`) with two-phase restart-boundary execution and summary artifacts.
- Added self-host go/no-go checklist runbook (`docs/SELF_HOST_GO_NO_GO.md`) with explicit release gates, rollback flow, and operator command set.
- Added stop/restart chaos validation workflow (`scripts/chaos-stop-restart.sh`, `docs/CHAOS_STOP_RESTART.md`) covering SIGTERM/SIGKILL interruption recovery checks.
- Added a top-level `Makefile` with thin workflow targets for build/test/coverage/install/run plus self-host/canary/sandbox/chaos commands, and documented usage in `README.md`.
- Added agent-command auto-detection for `SIMUG_AGENT_CMD` defaults, preferring non-interactive `codex exec` with compatibility fallback to `codex`.
- Added explicit startup trace of resolved `agent_command` in runtime output/event log metadata for easier Codex integration diagnosis.
- Added Codex runtime diagnostics classification for common failures (auth/permission/command-not-found) so runner errors include actionable hints.
- Added Codex preflight checks in startup and canary scripts to fail fast on auth/path-permission blockers before orchestration/canary execution.
- Added protocol-sequence collapse for real Codex transcript echoes so identical repeated terminal sequences are normalized to one final actionable sequence.
- Added protocol-first runner recovery so non-zero Codex exits are accepted when emitted output is still fully parseable and protocol-valid.
- Added staged bootstrap intent handshake for no-PR flow: a read-only intent turn (`INTENT_JSON` + `done changes=false`) that persists validated `bootstrap_intent` in state before execution is allowed.
- Added bootstrap intent parsing/validation unit tests and updated integration coverage for deterministic issue selection and pending-task targeting under staged intent flow.
- Added execution scope-lock enforcement for staged bootstrap runs, including planning-status drift checks on non-target tasks and `[IN_PROGRESS]` cardinality/target validation.
- Added scope-lock repair prompt constraints and unit coverage for task-ref parsing, planning-status lock validation, and scope-constrained repair instructions.
- Added protocol parser hardening to filter prompt-template `SIMUG:` action sequences when echoed alongside real agent output, preventing false multi-terminal failures.
- Added agent parser tests covering template-echo filtering and runner-level mixed-output handling.
- Added bootstrap execution report gate requiring exactly one `REPORT_JSON` comment with task/summary/branch/head evidence before push/PR creation.
- Added execution-report validation and prompt-contract tests, plus action filtering so internal report comments are not posted to created PRs.
- Added enriched attempt archive metadata with protocol action counts/types/excerpts, terminal diagnostics, parser hints, and detected rollout/session path references.
- Added diagnostics coverage for archive metadata enrichment and updated failure explainer output to surface protocol and rollout/session forensic context.
- Added staged bootstrap session continuity support with persisted `bootstrap_session_id` state and codex resume command selection (`codex exec resume <id> -`) when available.
- Added session continuity helpers/tests for resume command construction, session-id extraction from artifacts, and bootstrap-session state normalization semantics.
- Added explicit issue-task bootstrap handoff state (`issue_task_intent`) plus active PR task context tracking (`active_task_ref`) so issue-derived triage intent survives to bootstrap/PR linkage without orchestrator planning-file mutation.
- Added repo-relative `SIMUG_GUIDANCE_PATHS` / `SIMUG_PLANNING_PATHS` configuration so bootstrap prompts and planning scope locks can discover non-standard repository guidance files without hard-coding simug-specific filenames.

### Changed

- Modularized the large orchestration loop by extracting managed-PR flow, no-PR bootstrap flow, agent validation, and GitHub mutation/state-transition helpers from `internal/app/run.go` into focused `internal/app/orchestration_*.go` files, with added state-transition unit coverage.
- When a managed branch is already merged into `origin/main`, no-PR intake now checks out `main`, fast-forwards it, and deletes the merged local branch so dogfood runs do not accumulate stale agent branches.
- Bootstrap execution now requires exactly one commit from the staged baseline, and simug aborts automatic bootstrap repair/recovery when a failed attempt already advanced `HEAD`.
- Managed-PR prompts now include inline review comment file/hunk/line metadata, and review-comment replies use the pull-number-scoped GitHub API path expected by GitHub.
- Refined the Phase 7 backlog to queue follow-up work for inline review-context propagation, review-comment reply endpoint correctness, bootstrap single-commit fail-closed validation, and merged-branch local cleanup ahead of `Task 7.4`.
- Removed legacy orchestrator-side planning insertion during issue triage; `issue_report.needs_task=true` now records intent without mutating project files, preserving the `.simug/*`-only runtime write boundary.
- Updated issue-to-PR backlink behavior to remain idempotent when no `pending_task_id` is present, keeping issue traceability without requiring orchestrator planning edits.
- Updated design documentation to reflect that runtime orchestrator writes are limited to `.simug/*` artifacts and no longer include transitional planning-file mutation caveats.
- Reworked planning roadmap by inserting a dedicated maintainability/modularity/safety hardening phase between self-host readiness and environment/release readiness, and renumbered later phases accordingly.
- Clarified design/README ownership boundary that orchestrator should not directly edit project planning/workflow/source files; repository content updates are Codex-authored through normal commits.
- Extended issue roadmap with post-triage lifecycle tasks (protocol linkage during implementation, PR-scoped issue ledger, orchestrator-owned issue updates, and close-on-merge finalization).
- Added design-first execution ordering in planning to prioritize ownership-boundary and issue-lifecycle completion before remaining self-hosting milestones.
- Expanded design document with feasibility assessment, explicit known gaps, issue lifecycle target behavior, and mode-to-action handling matrix.
- Updated real-Codex canary scripts, gate docs, README examples, and Make defaults to use/auto-detect non-interactive `codex exec` by default while preserving explicit command overrides.
- Tightened workflow/agent policy so real-Codex gate evidence (`scripts/canary-real-codex-gate.sh`) is a mandatory completion criterion for future tasks, and documented runbook docs as operational validation procedures.
- Declared `history/*` files immutable after commit in workflow rules; follow-up clarifications must be recorded in new history files.
- Moved operational runbooks from `docs/` to `docs/runbooks/` and updated project references so top-level docs remain focused on design/planning/workflow guidance.
- Changed no-PR bootstrap orchestration from single-turn execution to a two-stage contract (`intent` tick, then `execution` tick) with explicit commit/no-commit invariants per stage.
- Tightened bootstrap intent contract so `task_ref` must include canonical `Task <id>` for deterministic scope locking during execution/repair attempts.
- Updated issue-derived bootstrap prompting/backlink semantics so triaged issue task proposals are injected into intent context, approved intent `task_ref` drives downstream task metadata, and issue backlink/comments use validated task context.
- Updated bootstrap guidance handling to discover optional repo instruction/workflow/planning files at prompt time and to skip planning-status lock enforcement when no supported planning file covers the approved task.
- Updated bootstrap guidance discovery to prepend repo-configured candidate paths before the default filename set, keeping optional-file fallback behavior for repositories with custom doc layouts.
