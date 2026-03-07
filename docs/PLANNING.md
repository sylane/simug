# simug Project Planning

This file tracks backlog and execution status for orchestrator development.

Process definition:
- [Workflow](WORKFLOW.md)

Design source of truth:
- [Design](DESIGN.md)

Status convention:
- TODO: `- [ ] **Task ...**`
- IN_PROGRESS: `- [ ] **[IN_PROGRESS] Task ...**`
- DONE: `- [x] **Task ...**`

## Phase 1: Core Orchestration Safety

- [x] **Task 1.1: Single-process lock and persistent worker state**
  - Scope: `.simug/lock`, `.simug/state.json` lifecycle.
  - Done when: startup enforces one process and state persists across runs.

- [x] **Task 1.2: Managed PR detection and strict cardinality checks**
  - Scope: discover authored open PRs and fail on ambiguity.
  - Done when: worker fails with clear message when more than one open authored PR exists.

- [x] **Task 1.3: Checkout synchronization validation for active PR**
  - Scope: branch, clean-tree, local HEAD, remote HEAD, and PR head SHA alignment.
  - Done when: worker exits on mismatch to prevent desynchronization.

- [x] **Task 1.4: Codex protocol parser and orchestration contract**
  - Scope: `SIMUG: {json}` parsing, terminal action enforcement.
  - Done when: malformed/ambiguous protocol output is rejected.

- [x] **Task 1.5: Bounded repair loop for agent consistency failures**
  - Scope: bounded retries for dirty tree / branch mismatch / commit expectation failures.
  - Done when: worker never loops forever on self-repair.

## Phase 2: PR Lifecycle Automation

- [x] **Task 2.1: No-PR bootstrap flow**
  - Scope: main-sync checks, Codex kickoff, branch naming validation, push + PR creation.
  - Done when: worker can create and start monitoring a new managed PR.

- [x] **Task 2.2: Comment/review polling with per-source high-water cursors**
  - Scope: issue comments, review comments, reviews with persisted cursors.
  - Done when: worker processes new events without reprocessing by default.

- [x] **Task 2.3: Cursor uncertainty replay mode**
  - Scope: migrate from legacy cursor and safely replay uncertain context once.
  - Done when: uncertain state triggers conservative Codex replay.

- [x] **Task 2.4: Orchestrator-owned GitHub mutation path**
  - Scope: PR comments, replies, push, PR creation only from orchestrator.
  - Done when: Codex cannot directly mutate GitHub state in normal flow.

## Phase 3: Hardening and Observability

- [x] **Task 3.1: Stale-lock recovery**
  - Scope: detect dead PID lock and recover automatically.
  - Done when: stale lock no longer blocks startup.

- [x] **Task 3.2: Structured event log**
  - Scope: append `.simug/events.log` JSONL entries for key transitions/actions.
  - Done when: operator can audit runtime decisions after failure.

- [x] **Task 3.3: `/agent` authorization and verb allowlist**
  - Scope: actionable commands only from allowed users and allowed verbs.
  - Done when: unauthorized or unsupported commands are ignored.

- [x] **Task 3.4: High-fidelity action trace log**
  - Scope: append structured per-run/per-tick traces for every orchestrator action (git/gh calls, durations, exit codes, stderr/stdout tails, invariant decisions).
  - Done when: a failed run can be reconstructed from logs without rerunning.

- [ ] **Task 3.5: Codex prompt/output archival**
  - Scope: persist prompts sent to Codex and raw streamed output with correlation ids linked to PR/tick/attempt (`run_id`, `tick_seq`, `command_seq` lineage).
  - Done when: protocol parsing failures and behavior regressions are diagnosable from artifacts.

- [ ] **Task 3.6: Operator failure explainer**
  - Scope: add a command/flow to summarize last failure reason, violated invariant, and suggested next action from trace data.
  - Done when: operator can get actionable failure diagnosis in one command.

## Phase 4: Test Reliability

- [x] **Task 4.1: Parsing unit tests for protocol and git remote parsing**
  - Scope: core parser correctness tests.
  - Done when: parser regressions are caught by unit tests.

- [x] **Task 4.2: Mocked integration tests for orchestrator startup paths**
  - Scope: app-level tests with mocked git/gh command runners.
  - Done when: key fail-fast startup invariants are covered without live GitHub.

- [x] **Task 4.3: Expand mocked integration coverage for successful end-to-end tick**
  - Scope: successful managed-PR tick including event poll, agent action application, and cursor update.
  - Done when: one green-path loop is validated fully with mocks.

- [ ] **Task 4.4: Prompt contract regression tests**
  - Scope: assert managed/bootstrap/repair prompt builders keep required protocol instructions and examples (`SIMUG:` prefix, terminal action rule, no-push constraints).
  - Done when: prompt contract drift is caught by automated tests before merge.

- [ ] **Task 4.5: Prompt-driven simulated Codex protocol matrix tests**
  - Scope: run orchestrator/agent integration with realistic mixed stdout fixtures (narrative text + protocol lines) covering valid, malformed, missing-terminal, and multi-terminal responses.
  - Done when: parser/orchestrator behavior is deterministic across protocol compliance and failure classes.

- [ ] **Task 4.6: Prompt tuning harness for repeatable protocol failures**
  - Scope: add repeatable test cases that model common Codex failure patterns and verify prompt refinements reduce recurring parse/validation failures.
  - Done when: prompt changes can be validated against a stable failure corpus instead of ad-hoc manual checks.

- [ ] **Task 4.7: Dual-entity output routing contract tests**
  - Scope: test prompt/parse behavior for explicit `SIMUG:` (coordinator) and `SIMUG_MANAGER:` (manager) output prefixes, including rejection or quarantine of ambiguous unprefixed output.
  - Done when: coordinator/manager channel separation is regression-tested and deterministic.

- [ ] **Task 4.8: Prompt-injection resilience tests**
  - Scope: add adversarial fixtures from GitHub comments/issues that attempt instruction override; verify only authorized `/agent` commands and coordinator directives are actionable.
  - Done when: injection-like inputs are contained and policy regressions fail tests.

## Phase 5: Issue-Driven Intake Before Next Task (Priority)

- [ ] **Task 5.1: Loop-mode state model for issue-first intake**
  - Scope: add explicit orchestrator modes (`managed_pr`, `issue_triage`, `task_bootstrap`) and state persistence for active issue/pending task metadata.
  - Done when: mode transitions are deterministic and restart-safe across PR merge -> issue triage -> task bootstrap.

- [ ] **Task 5.2: Authored-issue discovery and deterministic selection**
  - Scope: list open issues authored by current authenticated user and select one deterministically before next-task bootstrap.
  - Done when: no-PR flow always evaluates authored issues first with predictable ordering.

- [ ] **Task 5.3: Issue triage prompt and protocol extension**
  - Scope: add issue triage prompt path and protocol support for `issue_report` (relevant/not relevant, analysis, needs_task, task proposal metadata).
  - Done when: orchestrator validates and consumes issue triage reports reliably.

- [ ] **Task 5.4: Orchestrator-owned issue analysis comments**
  - Scope: convert `issue_report` outputs into orchestrator-posted comments on the issue, including triage marker metadata for replay/idempotency.
  - Done when: each triaged issue receives a deterministic machine-auditable analysis comment.

- [ ] **Task 5.5: Planning insertion engine for issue-derived tasks**
  - Scope: insert new TODO tasks in `docs/PLANNING.md` immediately after last DONE task with stable ID derivation (suffix policy) and markdown integrity checks.
  - Done when: issue-required tasks are injected deterministically without corrupting planning structure.

- [ ] **Task 5.6: Bootstrap from issue-derived task**
  - Scope: after planning insertion, bootstrap development using the inserted task exactly as normal workflow task execution.
  - Done when: issue-derived tasks produce managed branches/PRs through standard orchestration validation.

- [ ] **Task 5.7: PR reference back-linking to issue**
  - Scope: after PR creation from issue-derived task, orchestrator posts issue comment linking PR number/url and inserted task ID.
  - Done when: issue timeline shows explicit trace from triage decision to implementation PR.

- [ ] **Task 5.8: Issue-first integration and failure tests**
  - Scope: add tests for authored issue filtering, triage report parsing/validation, planning insertion, issue commenting, and fallback to normal next-task flow when no issue is actionable.
  - Done when: issue-first behavior is covered by deterministic automated tests.

## Phase 6: Self-Hosting Readiness (Priority)

- [ ] **Task 6.1: Single-task self-host run mode**
  - Scope: add a dedicated mode (for example `simug run --once`) that completes one task/tick, persists state, then exits with explicit status codes for supervisor scripts.
  - Done when: orchestrator can safely stop after one self-development unit of work without losing progress context.

- [ ] **Task 6.2: Self-host supervisor wrapper script**
  - Scope: add a wrapper script for simug-on-simug development that rebuilds binary, starts one-shot mode, captures logs/artifacts, and restarts/resumes deterministically.
  - Done when: operator can run repeatable self-host loops with one command.

- [ ] **Task 6.3: Crash-safe in-flight attempt journal**
  - Scope: persist attempt context before/after each Codex invocation (expected branch, pre/post head, attempt index, prompt hash, runtime mode).
  - Done when: orchestrator restart can deterministically recover interrupted attempts.

- [ ] **Task 6.4: Restart recovery state machine**
  - Scope: implement explicit recovery transitions for interrupted runs (resume/replay/repair/abort) with invariant checks.
  - Done when: restart behavior is deterministic and documented for each failure mode.

- [ ] **Task 6.5: Failure-injection integration tests**
  - Scope: add tests for malformed protocol, partial Codex output, no/multiple terminal actions, git/gh command failures, and restart mid-attempt.
  - Done when: known failure classes are reproducible and covered by automated tests.

- [ ] **Task 6.6: Live GitHub dry-run on a sandbox repo**
  - Scope: validate issue-first intake plus polling/replies/push/PR transitions end-to-end with real API.
  - Done when: at least one issue-driven and one planning-driven PR lifecycle complete without manual state repair.

- [ ] **Task 6.7: End-to-end self-hosting canary workflow**
  - Scope: scripted canary that runs simug on simug with trace capture and verifies PR/session continuity across at least one restart.
  - Done when: self-hosted dogfood loop passes reproducibly.

- [ ] **Task 6.8: Self-hosting go/no-go checklist and runbook**
  - Scope: define explicit gates for enabling simug-as-default for simug development (required tasks/tests/log artifacts, rollback procedure, operator commands).
  - Done when: team has a concrete and auditable criterion for switching from direct/manual development to self-host default.

- [ ] **Task 6.9: Stop/restart chaos validation**
  - Scope: run scripted stop/restart/crash-style interruption scenarios at different loop points and verify safe recovery invariants (branch, clean tree, state mode, active PR/issue coherence).
  - Done when: worker is demonstrably safe to stop/restart at arbitrary points without desynchronizing state.

## Phase 7: Environment and Release Readiness

- [ ] **Task 7.1: Tooling gate enablement in CI/local dev image**
  - Scope: ensure `go`, `gofmt`, and `gh` availability where checks run.
  - Done when: required runtime/build tools are consistently present.

- [ ] **Task 7.2: Run full verification gates in tool-enabled environment**
  - Scope: `gofmt -w`, `go test ./...`, and manual `simug run` smoke flow.
  - Done when: all gates pass with evidence.

## Phase 8: Codex Session Foundations

- [ ] **Task 8.1: Agent runtime abstraction (one-shot vs session-backed)**
  - Scope: introduce an interface for Codex runtimes so orchestrator can swap current subprocess runner for a session-capable backend without changing core PR logic.
  - Done when: orchestration flow compiles/tests against runtime abstraction and current behavior remains unchanged by default.

- [ ] **Task 8.2: PR-to-session continuity model**
  - Scope: formalize mapping of managed PR lane to Codex thread lifecycle (create/resume/fork/close) and recovery semantics after crashes.
  - Done when: each PR has deterministic Codex session lineage and restart recovery rules.

- [ ] **Task 8.3: Codex app-server integration prototype**
  - Scope: add runtime adapter for `codex app-server` JSON-RPC over stdio using `thread/start`, `thread/resume`, `turn/start`, and `turn/steer`.
  - Done when: one managed PR task loop can run end-to-end through app-server APIs.

- [ ] **Task 8.4: Structured Codex event ingestion**
  - Scope: support `codex exec --json` event stream parsing as a first-class input path (thread/turn/item events + final message extraction), while preserving `SIMUG:` action protocol validation.
  - Done when: orchestrator can consume structured JSONL events and still deterministically extract terminal actions.

- [ ] **Task 8.5: Persist Codex session identity in worker state**
  - Scope: extend `.simug/state.json` with Codex thread/session metadata (thread id, runtime mode, last turn id, optional rollout path), keyed to active PR/branch.
  - Done when: worker restart can recover the same Codex session context from persisted state.

- [ ] **Task 8.6: Resume-on-restart for Codex sessions**
  - Scope: resume the correct Codex session for managed PR flow after orchestrator restart instead of starting a fresh context each tick.
  - Done when: restart during an active PR preserves Codex conversational continuity.

- [ ] **Task 8.7: Local transcript with actor attribution**
  - Scope: add structured transcript log/output model that distinguishes `orchestrator`, `manager`, and `codex` messages (plus tool/command events).
  - Done when: operators can replay and audit a run with clear actor boundaries.

- [ ] **Task 8.8: Session strategy policy (fresh vs reused)**
  - Scope: define and implement configurable session strategy (`fresh_per_task` vs `reuse_until_pr_closed`) with explicit defaults and state persistence semantics.
  - Done when: strategy can be selected intentionally and behaves deterministically across task loops.

- [ ] **Task 8.9: Session lifecycle test matrix**
  - Scope: add automated tests covering fresh session per task, reused session across loops, and resumed session after restart/interruption.
  - Done when: session management behavior is verified across all supported lifecycle paths.

## Phase 9: Interactive Parity and Manager Pinch-In

- [ ] **Task 9.1: Dual-entity communication protocol and prompt contract**
  - Scope: formalize coordinator vs manager interaction contract, including how coordinator instructs Codex about both entities and mandatory output prefixes (`SIMUG:` vs `SIMUG_MANAGER:`).
  - Done when: Codex interaction is explicitly multiplexed with machine-friendly and human-friendly channels.

- [ ] **Task 9.2: Live console stream with channel filtering**
  - Scope: stream Codex conversation/events in real time while filtering/routing coordinator protocol vs manager-facing text cleanly in console output.
  - Done when: operator sees meaningful manager-facing output without protocol noise, and coordinator keeps full machine trace.

- [ ] **Task 9.3: Manager control channel (pause/message/resume)**
  - Scope: implement manager commands to pause autonomous loop, send steer messages to Codex, and resume execution with strict authorization/audit logging.
  - Done when: sending manager message pauses loop deterministically until explicit resume.

- [ ] **Task 9.4: Terminal interaction bridge**
  - Scope: support pass-through for terminal interaction events and control (`command/exec`, write/resize/terminate) where needed for manager-assisted flows.
  - Done when: manager/orchestrator can observe and, when authorized, interact with running command sessions.

- [ ] **Task 9.5: Compatibility + migration tests**
  - Scope: regression suite covering legacy one-shot runner, new session-backed runner, pause/resume manager channel, and interactive/session-migration scenarios.
  - Done when: migration to session-backed interactive mode is verified without regression in safety invariants.
