# Self-Host Go/No-Go Checklist

Use this checklist before enabling simug-as-default for simug development.

## Go Criteria (All Required)

- [ ] Local validation green:
  - `GOCACHE=/tmp/go-build go test ./...`
  - coverage report generated and reviewed.
- [ ] Real Codex gate passed recently:
  - `scripts/canary-real-codex-gate.sh`
  - summary artifact retained.
- [ ] Sandbox dry-run evidence verified:
  - `scripts/sandbox-dry-run.sh ...`
  - issue-driven and planning-driven merged PR evidence captured.
- [ ] Self-host canary passed:
  - `scripts/self-host-canary.sh --iterations <n>`
  - summary artifact retained.
- [ ] Chaos stop/restart validation passed:
  - `scripts/chaos-stop-restart.sh`
  - summary artifact retained.
- [ ] No unresolved high-severity invariant failures in latest `.simug/events.log`.
- [ ] Operator rollback plan is rehearsed and documented.

If any item is unchecked: **No-Go**.

## Rollback Procedure

1. Stop self-host automation.
2. Switch to direct/manual development workflow.
3. Preserve diagnostics:
   - `.simug/events.log`
   - `.simug/archive/agent/...`
   - gate/canary/sandbox summary artifacts.
4. Run:
   - `simug explain-last-failure`
5. Open remediation task in planning before re-attempting self-host default.

## Operator Commands

- Start one-shot loop:
  - `simug run --once`
- Continuous loop:
  - `simug run`
- Failure diagnosis:
  - `simug explain-last-failure`
- Real Codex gate:
  - `scripts/canary-real-codex-gate.sh`
- Sandbox verification:
  - `scripts/sandbox-dry-run.sh ...`
- Self-host canary:
  - `scripts/self-host-canary.sh ...`
