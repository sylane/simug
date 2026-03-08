# simug Development Workflow

## Purpose

This document defines the mandatory workflow for developing `simug`, including the self-hosted mode where `simug` orchestrates Codex to evolve `simug` itself.

Task prioritization/status is tracked in `docs/PLANNING.md`.
Architecture and behavior contracts are tracked in `docs/DESIGN.md`.

## Companion Documents

- Design source of truth:
  - `docs/DESIGN.md`
- Task backlog/status:
  - `docs/PLANNING.md`

## Core Principles

- Determinism first: prefer explicit state transitions and fail-closed behavior.
- Security first: never allow unvalidated agent or comment input to mutate GitHub state.
- One-lane execution: one worker process and one managed PR at a time.
- Test with intent: include unit tests and mocked integration tests for orchestration paths.
- Persistent task memory: keep one high-signal context file per task under `history/`.
- Changelog discipline: record final task outcomes in `CHANGELOG.md`.
- Small, focused commits: one logical task per commit whenever practical.
- No silent recovery: either repair with bounded retries or stop with a precise error.
- Document decisions: update `docs/DESIGN.md` in the same commit when behavior contracts change.
- Progressive planning refinement: after each completed task, refine future planning items so discovered constraints are preserved.

## Task Status Convention

Use these markers in `docs/PLANNING.md`:

- TODO: `- [ ] **Task ...**`
- IN_PROGRESS: `- [ ] **[IN_PROGRESS] Task ...**`
- DONE: `- [x] **Task ...**`

Rules:

- Keep at most one `[IN_PROGRESS]` task at a time.
- Prefer highest-priority pending task unless explicitly redirected.

## Task ID Source (Mandatory)

Task ID must come from `docs/PLANNING.md` (example: `Task 2.3`).

- If no task exists, add it to `docs/PLANNING.md` first.
- History filenames and task notes must use that same ID.

## Self-Hosted Orchestration Workflow

Use this loop while developing `simug` with `simug`:

1. Start `simug` locally in this repo.
   - For supervised one-shot loops, use `scripts/self-host-loop.sh --iterations <n>`.
2. Ensure managed PR/cardinality checks pass.
3. Drive work from GitHub using authorized `/agent ...` commands.
4. Let orchestrator invoke Codex with the protocol contract.
5. Codex implements one planning task and commits locally.
6. Orchestrator validates branch/commit/clean-tree invariants.
7. Orchestrator (not Codex) pushes and updates PR/comments.
8. Repeat until task is complete or an invariant violation requires human intervention.

## Orchestrator Expectations

Enforced by orchestrator:

- single managed PR lane and branch-policy checks,
- no direct push/PR mutation by Codex,
- clean working tree at terminal action,
- commit movement consistency (`changes=true` implies new commit; `idle` implies no commit change),
- bounded repair attempts with explicit diagnostics.

Required process for Codex during self-hosted development:

- follow this workflow document and `docs/PLANNING.md`,
- create/update a task context file in `history/`,
- run baseline/final validations,
- update `CHANGELOG.md` with final task outcomes,
- prepare `.git/SIMUG_COMMIT_MSG` and commit with it.

## Mandatory Task Lifecycle

1. Read context before edits.
   - Read `docs/DESIGN.md` and related code paths.
2. Mark selected task `[IN_PROGRESS]` in `docs/PLANNING.md`.
3. Create task history file before implementation.
   - Path format:
     `history/<YYYYMMDDTHHMMSSZ>__task-<task-id>__<short-description>.md`
   - Timestamp must be UTC.
4. Run baseline validation before code changes.
   - Preferred baseline: `GOCACHE=/tmp/go-build go test ./...`
   - Coverage baseline:
     - `GOCACHE=/tmp/go-build GOEXPERIMENT=nocoverageredesign go test ./... -coverprofile=coverage.out`
     - `GOCACHE=/tmp/go-build go tool cover -func=coverage.out`
5. Add or update tests for expected behavior.
6. Implement minimal scoped changes.
7. Iterate with focused tests during implementation.
8. Run full relevant validation again.
9. Finalize task history file with durable decisions and evidence only.
10. Update `CHANGELOG.md` with final task outcomes (not intermediate churn).
11. Prepare commit message file and commit.
    - Write `.git/SIMUG_COMMIT_MSG`.
    - Commit with `git commit -F .git/SIMUG_COMMIT_MSG`.
12. Mark task DONE in `docs/PLANNING.md`.
13. Refine future planning items in `docs/PLANNING.md` with discovered constraints.
    - Update relevant future tasks, not only immediately next tasks.
    - If no refinements are needed, record explicit `None` in `history/` for this task.
14. Report completion with validation summary and residual risks.

## Planning Refinement Rules

- Mandatory per completed task: run one refinement pass over future tasks in `docs/PLANNING.md`.
- Allowed without extra approval:
  - clarification notes,
  - dependency/order notes,
  - missing acceptance details,
  - clearly missing tasks that do not change approved design behavior.
- Requires manager approval:
  - major scope rewrites,
  - phase-wide reprioritization or resequencing,
  - design/behavior changes not already approved.
- Keep refinements concise and evidence-based.

## Compact Templates

Use these inline templates directly; no external template files are required.

History file template (`history/<YYYYMMDDTHHMMSSZ>__task-<task-id>__<short-description>.md`):

```md
# Task <task-id>: <short title>

## Objective
- <what this task must achieve>

## Scope
- In: <touched modules/behaviors>
- Out: <explicit non-goals>

## Key Decisions
- <decision>: <rationale/tradeoff>

## Validation
- Baseline: `<command(s)>` -> <result>
- Final: `<command(s)>` -> <result>

## Residual Risks
- <risk or "None">

## Planning Refinements
- <updates made to docs/PLANNING.md or "None">
```

Commit message template (`.git/SIMUG_COMMIT_MSG`):

```text
<type>(<scope>): <short summary>

- <final outcome 1>
- <final outcome 2>
- <final outcome 3>
```

Message rules:

- reflect final behavior/outcomes, not step-by-step implementation churn,
- exclude validation logs and acceptance checklist text,
- keep concise and reviewer-oriented.

## Commit Message Discipline

- Use `.git/SIMUG_COMMIT_MSG` for each task commit.
- Keep message focused on final behavior/outcomes and key architecture effects.
- Do not include acceptance criteria checklists or test logs in commit message body.
- Do not duplicate full commit text inside `history/` files.

## Quality Gates

Before finalizing a task:

- `GOCACHE=/tmp/go-build go test ./...` passes.
- Coverage report is generated and reviewed (`coverage.out` + `go tool cover -func`).
- Added/updated tests cover changed behavior.
- No unresolved invariant regressions.
- State/protocol changes are reflected in `docs/DESIGN.md`.
- `CHANGELOG.md` is updated with final outcomes.
- `.git/SIMUG_COMMIT_MSG` exists and is non-empty.
- Planning refinement pass is completed and reflected in `docs/PLANNING.md` or explicitly recorded as `None` in task history.
- Task context exists in `history/` and includes concise validation evidence.
- For real-runtime readiness gates, `scripts/canary-real-codex-gate.sh` is executed and artifacts are retained per `docs/REAL_CODEX_GATE.md`.

If `go` or `gh` is unavailable in the execution environment, record this explicitly in task completion notes and run the gates on the next environment where tools are available.

## Definition of Done (Per Task)

A task is DONE only if all are true:

- task marked `[x]` in `docs/PLANNING.md`,
- task context file exists and is finalized in `history/`,
- tests are added/updated and passing,
- full relevant suites rerun after code changes,
- `docs/DESIGN.md` updated when behavior contracts changed,
- `CHANGELOG.md` updated,
- `.git/SIMUG_COMMIT_MSG` prepared,
- planning refinement pass completed for future tasks (or explicitly `None` in task history),
- completion report includes residual risks/follow-ups.

## Protocol Discipline

- Codex messages consumed by orchestrator must be emitted as:
  - `SIMUG: {json}`
- Exactly one terminal action (`done` or `idle`) is required.
- Orchestrator must reject malformed or unknown protocol actions.

## Security Discipline

- Only `/agent` commands from authorized users are actionable.
- Unsupported `/agent` verbs are ignored and surfaced to Codex as ignored inputs.
- Orchestrator owns all push/PR/comment network side effects.
- Never execute shell fragments from GitHub comments.

## Failure Handling Policy

- Recoverable consistency failures: bounded Codex repair attempts.
- Non-recoverable/ambiguous state: stop with a clear actionable error.
- Never loop indefinitely on repair instructions.
