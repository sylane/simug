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

## Priority Realignment (Design-First Execution Order)

When phase ordering conflicts with design alignment, execute tasks in this order:

1. Task 7.1 (remove orchestrator direct project-file mutation paths)
2. Task 7.2 (Codex-mediated issue-task intake instead of planner insertion)
3. Task 7.2a (intent-only planning handshake before execution)
4. Task 7.2b (execution scope lock and repair containment)
5. Task 7.2c (protocol parser hardening against prompt/template echoes)
6. Task 7.2d (post-execution report gate before push/PR creation)
7. Task 7.2e (attempt-level observability + forensic artifacts)
8. Task 7.2f (same-session continuity for staged intent/execute/repair turns)
9. Task 7.3 (optional bootstrap context; no hard-required docs format)
10. Task 5.9 (issue linkage protocol in implementation turns)
11. Task 5.10 (PR-scoped tracked issue ledger)
12. Task 5.12 (close-on-merge issue finalization)
13. Task 5.11 (development-time issue impact/fix comments)
14. Task 5.13 (full lifecycle integration + adversarial tests)
15. Task 6.5a (real Codex protocol conformance canary)
16. Task 6.5b (real Codex repair/restart interaction canary)
17. Task 6.5c (real Codex validation gate integration)
18. Task 6.10a (Codex command auto-detection + non-interactive defaults)
19. Task 6.10b (Codex runtime preflight diagnostics)
20. Task 6.10c (environment-configured Codex compatibility + passing real-Codex gate)
21. Task 6.10d (workflow enforcement of real-Codex gate for all future tasks)
22. Task 6.10e (runbook docs relocation to docs/runbooks/)
23. Task 6.3+ (remaining self-hosting continuation)

Rationale:
- Design requires orchestrator/project ownership boundary first.
- Issue lifecycle completion should be in place before deeper self-hosting and interactive/session expansions.

Design sync gate for this realignment queue (`7.1`, `7.2`, `7.3`, `5.9`-`5.13`):
- Do not mark task DONE unless `docs/DESIGN.md` is updated to reflect final behavior and remove/adjust transitional notes impacted by that task.
- Update `AGENTS.md`/`docs/WORKFLOW.md` in the same commit when operator workflow or task-selection behavior changes.

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

- [x] **Task 3.5: Codex prompt/output archival**
  - Scope: persist prompts sent to Codex and raw streamed output with correlation ids linked to PR/tick/attempt (`run_id`, `tick_seq`, `command_seq` lineage).
  - Done when: protocol parsing failures and behavior regressions are diagnosable from artifacts.

- [x] **Task 3.6: Operator failure explainer**
  - Scope: add a command/flow to summarize last failure reason, violated invariant, and suggested next action from trace data and `.simug/archive/agent/...` attempt artifacts.
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

- [x] **Task 4.4: Prompt contract regression tests**
  - Scope: assert managed/bootstrap/repair prompt builders keep required protocol instructions and examples (`SIMUG:` prefix, terminal action rule, no-push constraints).
  - Done when: prompt contract drift is caught by automated tests before merge.

- [x] **Task 4.5: Prompt-driven simulated Codex protocol matrix tests**
  - Scope: run orchestrator/agent integration with realistic mixed stdout fixtures (narrative text + protocol lines) covering valid, malformed, missing-terminal, and multi-terminal responses, while preserving 4.4 prompt-contract requirements.
  - Done when: parser/orchestrator behavior is deterministic across protocol compliance and failure classes.

- [x] **Task 4.6: Prompt tuning harness for repeatable protocol failures**
  - Scope: add repeatable test cases that model common Codex failure patterns and verify prompt refinements reduce recurring parse/validation failures, reusing and extending the 4.5 protocol matrix corpus.
  - Done when: prompt changes can be validated against a stable failure corpus instead of ad-hoc manual checks.

- [x] **Task 4.7: Dual-entity output routing contract tests**
  - Scope: test prompt/parse behavior for explicit `SIMUG:` (coordinator) and `SIMUG_MANAGER:` (manager) output prefixes, including rejection or quarantine of ambiguous unprefixed output, extending the repeatable 4.6 failure corpus/harness.
  - Done when: coordinator/manager channel separation is regression-tested and deterministic.

- [x] **Task 4.8: Prompt-injection resilience tests**
  - Scope: add adversarial fixtures from GitHub comments/issues that attempt instruction override; verify only authorized `/agent` commands and coordinator directives are actionable, including manager/coordinator channel-prefix abuse attempts.
  - Done when: injection-like inputs are contained and policy regressions fail tests.

## Phase 5: Issue-Driven Intake Before Next Task (Priority)

- [x] **Task 5.1: Loop-mode state model for issue-first intake**
  - Scope: add explicit orchestrator modes (`managed_pr`, `issue_triage`, `task_bootstrap`) and state persistence for active issue/pending task metadata.
  - Done when: mode transitions are deterministic and restart-safe across PR merge -> issue triage -> task bootstrap.

- [x] **Task 5.2: Authored-issue discovery and deterministic selection**
  - Scope: list open issues authored by current authenticated user and select one deterministically before next-task bootstrap, using persisted mode/issue/task metadata introduced in 5.1.
  - Done when: no-PR flow always evaluates authored issues first with predictable ordering.

- [x] **Task 5.3: Issue triage prompt and protocol extension**
  - Scope: add issue triage prompt path and protocol support for `issue_report` (relevant/not relevant, analysis, needs_task, task proposal metadata) while preserving explicit `SIMUG:` / `SIMUG_MANAGER:` channel rules; consume persisted deterministic issue candidate (`active_issue`) selected in 5.2.
  - Done when: orchestrator validates and consumes issue triage reports reliably.

- [x] **Task 5.4: Orchestrator-owned issue analysis comments**
  - Scope: convert validated `issue_report` outputs into orchestrator-posted comments on the issue, including triage marker metadata for replay/idempotency.
  - Done when: each triaged issue receives a deterministic machine-auditable analysis comment.

- [x] **Task 5.5: Planning insertion engine for issue-derived tasks**
  - Scope: insert new TODO tasks in `docs/PLANNING.md` immediately after last DONE task with stable ID derivation (suffix policy) and markdown integrity checks, driven by validated `issue_report.needs_task=true` proposals.
  - Done when: issue-required tasks are injected deterministically without corrupting planning structure.

- [x] **Task 5.6: Bootstrap from issue-derived task**
  - Scope: after planning insertion, bootstrap development using the inserted `pending_task_id` as explicit kickoff target while preserving normal workflow task execution invariants.
  - Done when: issue-derived tasks produce managed branches/PRs through standard orchestration validation.

- [x] **Task 5.7: PR reference back-linking to issue**
  - Scope: after PR creation from issue-derived task, orchestrator posts issue comment linking PR number/url and inserted task ID, using `active_issue` + `pending_task_id` context.
  - Done when: issue timeline shows explicit trace from triage decision to implementation PR.

- [x] **Task 5.8: Issue-first integration and failure tests**
  - Scope: add tests for authored issue filtering, triage report parsing/validation, planning insertion, issue commenting, PR-backlink idempotency, and fallback to normal next-task flow when no issue is actionable.
  - Done when: issue-first behavior is covered by deterministic automated tests.

- [x] **Task 5.9: Issue linkage protocol for implementation turns**
  - Scope: extend coordinator prompt contract so Codex can declare issue linkage during task implementation (`fixes`, `impacts`, `relates`) in a machine-parseable way, and can request orchestrator-owned issue comments without direct GitHub mutation; depends on design-alignment execution order above.
  - Done when: orchestrator can parse and validate issue-linkage intent from normal task-development turns, and `docs/DESIGN.md` documents the finalized linkage protocol contract.

- [x] **Task 5.10: PR-scoped tracked issue ledger in worker state**
  - Scope: persist per-active-PR issue linkage metadata (candidate fixed issues, impacted issues, comment intents, provenance) with restart-safe idempotency keys.
  - Done when: stop/restart never loses which issues are associated with the active PR and pending issue-side actions, and `docs/DESIGN.md` state schema reflects the new fields.

- [x] **Task 5.11: Orchestrator-owned issue updates during development**
  - Scope: apply validated issue-update intents as orchestrator comments on issues (same authenticated-user scope first), including references to planning/task context and deterministic dedupe markers.
  - Done when: Codex can surface relevant issue impact/fix context and orchestrator posts it idempotently, and `docs/DESIGN.md` documents comment semantics/idempotency markers.

- [x] **Task 5.12: Close-on-merge issue finalization workflow**
  - Scope: when managed PR is detected merged, orchestrator comments on tracked fixed issues with PR reference, then closes those issues idempotently; non-fixed impacted issues receive informational-only comments.
  - Done when: merged PRs automatically finalize tracked fixed issues without duplicate comments/closures across restarts, and `docs/DESIGN.md` explicitly describes merge-triggered closure policy.

- [x] **Task 5.13: Lifecycle integration + adversarial tests**
  - Scope: add tests for protocol parsing, state persistence across restart, unauthorized issue attempts, malformed linkage payloads, merged-PR finalization (including inactive-PR state edge cases), and duplicate-suppression behavior.
  - Done when: issue lifecycle (triage -> implementation updates -> merge finalization) is deterministically covered by automated tests, with design/workflow docs updated for any contract changes discovered.

## Phase 6: Self-Hosting Readiness (Priority)

Execution note:
- Resume remaining Phase 6 tasks after the design-alignment queue above is complete.

- [x] **Task 6.1: Single-task self-host run mode**
  - Scope: add a dedicated mode (for example `simug run --once`) that completes one task/tick, persists state, then exits with explicit status codes for supervisor scripts.
  - Done when: orchestrator can safely stop after one self-development unit of work without losing progress context.

- [x] **Task 6.2: Self-host supervisor wrapper script**
  - Scope: add a wrapper script for simug-on-simug development that rebuilds binary, starts one-shot mode (`simug run --once`), captures logs/artifacts, and restarts/resumes deterministically using explicit exit-code outcomes.
  - Done when: operator can run repeatable self-host loops with one command.

- [x] **Task 6.3: Crash-safe in-flight attempt journal**
  - Scope: persist attempt context before/after each Codex invocation (expected branch, pre/post head, attempt index, prompt hash, runtime mode), aligned with wrapper snapshot/log artifacts.
  - Done when: orchestrator restart can deterministically recover interrupted attempts.

- [x] **Task 6.4: Restart recovery state machine**
  - Scope: implement explicit recovery transitions for interrupted runs (resume/replay/repair/abort) with invariant checks, consuming `in_flight_attempt` journal phases persisted in 6.3.
  - Done when: restart behavior is deterministic and documented for each failure mode.

- [x] **Task 6.5: Failure-injection integration tests**
  - Scope: add deterministic tests for malformed protocol, partial Codex output, no/multiple terminal actions, git/gh command failures, restart mid-attempt, recovery action transitions (`resume`/`replay`/`repair`/`abort`), and issue-link finalization fault paths (scope rejection + duplicate markers + close failures).
  - Done when: known failure classes are reproducible and covered by automated tests.

- [x] **Task 6.5a: Real Codex protocol conformance canary**
  - Scope: run scripted canary scenarios against a real Codex runtime (not shell fixtures) to validate protocol parseability, channel prefix discipline, terminal-action cardinality, and malformed/partial output handling under managed/triage/bootstrap prompts.
  - Done when: canary provides repeatable pass/fail results with archived prompt/output artifacts for debugging failures.

- [x] **Task 6.5b: Real Codex repair/restart interaction canary**
  - Scope: execute real Codex runs that intentionally trigger repair paths and one-shot restart/resume boundaries, validating orchestrator recovery semantics with live agent behavior and artifact continuity from 6.5a canary roots.
  - Done when: repair/restart behavior with real Codex is reproducibly validated across scripted interruption scenarios.

- [x] **Task 6.5c: Real Codex validation gate integration**
  - Scope: integrate real Codex canaries into operator/release workflow (manual gate and/or scheduled gate) with clear prerequisites, cost/runtime expectations, and artifact retention, including protocol + recovery canary runners.
  - Done when: release readiness requires recent successful real Codex validation evidence.

- [x] **Task 6.6: Live GitHub + real Codex dry-run on a sandbox repo**
  - Scope: validate issue-first intake plus polling/replies/push/PR transitions end-to-end with real GitHub API and real Codex runtime, using 6.5c validation gate artifacts as prerequisite evidence.
  - Done when: at least one issue-driven and one planning-driven PR lifecycle complete without manual state repair.

- [x] **Task 6.7: End-to-end self-hosting canary workflow**
  - Scope: scripted canary that runs simug on simug with trace capture and verifies PR/session continuity across at least one restart, reusing sandbox/gate evidence flows.
  - Done when: self-hosted dogfood loop passes reproducibly.

- [x] **Task 6.8: Self-hosting go/no-go checklist and runbook**
  - Scope: define explicit gates for enabling simug-as-default for simug development (required tasks/tests/log artifacts including self-host canary + real-Codex gate outputs, rollback procedure, operator commands including failure diagnosis flow such as `simug explain-last-failure`).
  - Done when: team has a concrete and auditable criterion for switching from direct/manual development to self-host default.

- [x] **Task 6.9: Stop/restart chaos validation**
  - Scope: run scripted stop/restart/crash-style interruption scenarios at different loop points and verify safe recovery invariants (branch, clean tree, state mode, active PR/issue coherence), reporting pass/fail against the 6.8 go/no-go checklist criteria.
  - Done when: worker is demonstrably safe to stop/restart at arbitrary points without desynchronizing state.

- [x] **Task 6.10a: Codex command auto-detection + non-interactive defaults**
  - Scope: when `SIMUG_AGENT_CMD` is unset, detect available Codex CLI mode and prefer `codex exec` for non-interactive operation with fallback compatibility; align canary scripts/Make targets/docs with the same non-interactive default while preserving explicit `--cmd`/env overrides.
  - Done when: out-of-box runs use a non-interactive Codex command by default in most environments, and operators can still override command/profile/config explicitly.

- [x] **Task 6.10b: Codex runtime preflight diagnostics**
  - Scope: add deterministic preflight checks and actionable failure messages for common Codex runtime blockers (missing command, auth missing/invalid, unwritable Codex home/cache paths including `~/.codex/tmp/arg0` style permission failures) without taking ownership of Codex account setup.
  - Done when: canary/simug startup failures surface precise diagnostics that identify environment fix actions instead of opaque command errors.

- [x] **Task 6.10c: Environment-configured Codex compatibility + passing real-Codex gate**
  - Scope: make simug real-runtime integration work with environment-configured Codex out of the box (`codex exec` default path), including robust parsing/ingestion of real Codex output and elimination of known protocol false failures in canary flows.
  - Done when: `scripts/canary-real-codex-gate.sh` passes in a standard environment-configured Codex setup without requiring custom `CODEX_HOME`/`CODEX_SQLITE_HOME` overrides.

- [x] **Task 6.10d: Workflow enforcement of real-Codex gate for all future tasks**
  - Scope: codify `scripts/canary-real-codex-gate.sh` as mandatory Definition-of-Done evidence for all future implementation tasks and align AGENTS/workflow wording with this requirement.
  - Done when: workflow/agents docs explicitly block task finalization without recent passing real-Codex gate evidence (or explicit manager waiver recorded in task history).

- [x] **Task 6.10e: Runbook docs relocation to `docs/runbooks/`**
  - Scope: move operational validation runbooks out of top-level `docs/` into `docs/runbooks/` and update all references to preserve navigation and workflow clarity.
  - Done when: runbooks live under `docs/runbooks/`, top-level docs remain design/planning/workflow-oriented, and all links/references resolve correctly.

## Phase 7: Maintainability, Modularity, and Safety Hardening (Priority)

- [x] **Task 7.1: Enforce orchestrator/project ownership boundary**
  - Scope: remove direct orchestrator writes to project workflow/planning/source files (starting with `docs/PLANNING.md` insertion), keeping project edits Codex-authored via normal commits while simug writes only `.simug/*` runtime artifacts and orchestrator-owned GitHub mutations.
  - Done when: a runtime write-path audit confirms non-test orchestrator writes are limited to `.simug/*`, and `docs/DESIGN.md` no longer carries stale divergence wording for this boundary.

- [x] **Task 7.2: Replace planner insertion with Codex-mediated issue-task intake**
  - Scope: change issue-triage flow so `issue_report.needs_task=true` produces coordinator intent and bootstrap instructions, not markdown parsing/insertion by simug.
  - Done when: issue-derived work can proceed end-to-end without simug parsing or editing planning files, and `docs/DESIGN.md`/`docs/WORKFLOW.md` describe the finalized Codex-mediated flow.
  - Refinement: `Task 7.1` removed runtime planning insertion and now logs issue-task intent without `pending_task_id` assignment; define explicit bootstrap handoff fields and backlink/task-context semantics for issue-derived work.

- [x] **Task 7.2a: Intent-only planning handshake before execution**
  - Scope: split bootstrap into a read-only `intent` turn where Codex proposes task scope/branch slug/PR draft/check plan without editing files; orchestrator validates and persists approved intent before any write-enabled execution turn.
  - Done when: intent turn is machine-validated, leaves clean tree unchanged, and execution turn cannot start without an approved persisted intent.

- [x] **Task 7.2b: Execution scope lock and repair containment**
  - Scope: bind execution/repair prompts to the approved intent and reject drift (task switching, unrelated planning edits, extra `[IN_PROGRESS]` mutations) during repair loops.
  - Done when: protocol/invariant repair attempts are constrained to the same approved task scope and cannot silently advance to a different task.

- [x] **Task 7.2c: Protocol parser hardening against prompt/template echoes**
  - Scope: harden protocol ingestion so template/example `SIMUG:` lines echoed from prompts/transcripts are ignored, and only canonical agent-emitted actions contribute to terminal-action cardinality checks.
  - Done when: known repros with echoed protocol examples no longer cause false multi-terminal failures.

- [x] **Task 7.2d: Post-execution report gate before push/PR creation**
  - Scope: require a structured terminal report (summary + commit evidence + branch identity) and validate commit movement before orchestrator push/PR creation.
  - Done when: orchestrator never attempts PR creation without validated report payload and verified commit advancement.

- [x] **Task 7.2e: Attempt-level observability and forensic artifacts**
  - Scope: persist parsed protocol excerpts, terminal-detection diagnostics, and rollout/session references in per-attempt archives and `explain-last-failure` output.
  - Done when: every failed attempt has actionable non-empty forensic artifacts explaining why validation failed.

- [x] **Task 7.2f: Same-session continuity for staged turns**
  - Scope: ensure staged `intent -> execute -> repair` turns run on the same Codex session/thread identity with restart-safe persistence in worker state.
  - Done when: state/events prove session continuity across staged turns and restart recovery resumes the same session when available.

- [x] **Task 7.3: Bootstrap context abstraction (no hard-required docs format)**
  - Scope: make prompt bootstrap context optional/discoverable and configurable per repo, with no hard dependency on `docs/WORKFLOW.md` or `docs/PLANNING.md` existence/format.
  - Done when: simug orchestrates safely in repositories that do not provide these files, and `docs/DESIGN.md` + `AGENTS.md` clarify guidance-file handling and fallback behavior.

- [ ] **Task 7.4: Modularize orchestration loop**
  - Scope: split `internal/app/run.go` mode handlers, validation stages, and mutation/application paths into focused components.
  - Done when: orchestration logic has smaller cohesive units with dedicated tests and reduced duplication.

- [ ] **Task 7.5: Single-source prompt contract**
  - Scope: centralize protocol instructions/examples so runtime prompt builders and prompt tests use shared constants/renderers.
  - Done when: prompt contract drift is caught without duplicated literal maintenance across code/tests.

- [ ] **Task 7.6: Idempotent GitHub mutation primitives**
  - Scope: extract shared marker/comment dedupe patterns used by issue analysis and issue-to-PR backlink flows into reusable helpers.
  - Done when: mutation idempotency logic is uniform and no longer duplicated across orchestrator paths.

- [ ] **Task 7.7: Integration test harness consolidation**
  - Scope: reduce fixture and setup duplication in integration tests with helper builders and explicit scenario matrices.
  - Done when: adding new orchestration scenarios requires minimal boilerplate while preserving determinism.

- [ ] **Task 7.8: Reliability and coverage uplift gates**
  - Scope: increase coverage in low-coverage packages (`internal/git`, `internal/github`, `internal/runtimepaths`) and add deterministic failure-injection tests for restart/protocol/command failure classes.
  - Done when: documented coverage/failure-matrix gates are enforced in local and CI validation flows.

- [ ] **Task 7.9: Protocol robustness fuzz/property tests**
  - Scope: add fuzz/property-driven tests for dual-channel protocol parsing (`SIMUG:` / `SIMUG_MANAGER:`), terminal-action cardinality, and malformed JSON handling.
  - Done when: parser robustness regressions are caught by automated fuzz/property checks.

## Phase 8: Environment and Release Readiness

- [ ] **Task 8.1: Tooling gate enablement in CI/local dev image**
  - Scope: ensure `go`, `gofmt`, and `gh` availability where checks run.
  - Done when: required runtime/build tools are consistently present.

- [ ] **Task 8.2: Run full verification gates in tool-enabled environment**
  - Scope: `gofmt -w`, `go test ./...`, and manual `simug run` smoke flow.
  - Done when: all gates pass with evidence.

## Phase 9: Codex Session Foundations

- [ ] **Task 9.1: Agent runtime abstraction (one-shot vs session-backed)**
  - Scope: introduce an interface for Codex runtimes so orchestrator can swap current subprocess runner for a session-capable backend without changing core PR logic.
  - Done when: orchestration flow compiles/tests against runtime abstraction and current behavior remains unchanged by default.

- [ ] **Task 9.2: PR-to-session continuity model**
  - Scope: formalize mapping of managed PR lane to Codex thread lifecycle (create/resume/fork/close) and recovery semantics after crashes.
  - Done when: each PR has deterministic Codex session lineage and restart recovery rules.

- [ ] **Task 9.3: Codex app-server integration prototype**
  - Scope: add runtime adapter for `codex app-server` JSON-RPC over stdio using `thread/start`, `thread/resume`, `turn/start`, and `turn/steer`.
  - Done when: one managed PR task loop can run end-to-end through app-server APIs.

- [ ] **Task 9.4: Structured Codex event ingestion**
  - Scope: support `codex exec --json` event stream parsing as a first-class input path (thread/turn/item events + final message extraction), while preserving `SIMUG:` action protocol validation.
  - Done when: orchestrator can consume structured JSONL events and still deterministically extract terminal actions.

- [ ] **Task 9.5: Persist Codex session identity in worker state**
  - Scope: extend `.simug/state.json` with Codex thread/session metadata (thread id, runtime mode, last turn id, optional rollout path), keyed to active PR/branch.
  - Done when: worker restart can recover the same Codex session context from persisted state.

- [ ] **Task 9.6: Resume-on-restart for Codex sessions**
  - Scope: resume the correct Codex session for managed PR flow after orchestrator restart instead of starting a fresh context each tick.
  - Done when: restart during an active PR preserves Codex conversational continuity.

- [ ] **Task 9.7: Local transcript with actor attribution**
  - Scope: add structured transcript log/output model that distinguishes `orchestrator`, `manager`, and `codex` messages (plus tool/command events).
  - Done when: operators can replay and audit a run with clear actor boundaries.

- [ ] **Task 9.8: Session strategy policy (fresh vs reused)**
  - Scope: define and implement configurable session strategy (`fresh_per_task` vs `reuse_until_pr_closed`) with explicit defaults and state persistence semantics.
  - Done when: strategy can be selected intentionally and behaves deterministically across task loops.

- [ ] **Task 9.9: Session lifecycle test matrix**
  - Scope: add automated tests covering fresh session per task, reused session across loops, and resumed session after restart/interruption.
  - Done when: session management behavior is verified across all supported lifecycle paths.

## Phase 10: Interactive Parity and Manager Pinch-In

- [ ] **Task 10.1: Dual-entity communication protocol and prompt contract**
  - Scope: formalize coordinator vs manager interaction contract, including how coordinator instructs Codex about both entities and mandatory output prefixes (`SIMUG:` vs `SIMUG_MANAGER:`).
  - Done when: Codex interaction is explicitly multiplexed with machine-friendly and human-friendly channels.

- [x] **Task 10.2: Live console stream with channel filtering**
  - Scope: stream Codex conversation/events in real time while filtering/routing coordinator protocol vs manager-facing text cleanly in console output.
  - Done when: operator sees meaningful manager-facing output without protocol noise, and coordinator keeps full machine trace.

- [ ] **Task 10.3: Manager control channel (pause/message/resume)**
  - Scope: implement manager commands to pause autonomous loop, send steer messages to Codex, and resume execution with strict authorization/audit logging.
  - Done when: sending manager message pauses loop deterministically until explicit resume.

- [ ] **Task 10.4: Terminal interaction bridge**
  - Scope: support pass-through for terminal interaction events and control (`command/exec`, write/resize/terminate) where needed for manager-assisted flows.
  - Done when: manager/orchestrator can observe and, when authorized, interact with running command sessions.

- [ ] **Task 10.5: Compatibility + migration tests**
  - Scope: regression suite covering legacy one-shot runner, new session-backed runner, pause/resume manager channel, and interactive/session-migration scenarios.
  - Done when: migration to session-backed interactive mode is verified without regression in safety invariants.
