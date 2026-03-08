# Stop/Restart Chaos Validation

This runbook covers 6.9 interruption validation for simug runtime safety.

## Goal

Validate that stop/restart interruptions do not desynchronize core invariants:

- clean working tree
- coherent `mode`/`active_pr`/`active_branch` state
- restartability after SIGTERM and SIGKILL interruptions

## Command

```bash
scripts/chaos-stop-restart.sh --repo . --sleep-seconds 2
```

Optional:

- `--agent-cmd "<cmd>"` to override test agent command.

## Scenarios

1. Continuous run interrupted via `SIGTERM`, then resumed with `run --once`.
2. Continuous run interrupted via `SIGKILL`, then resumed with `run --once`.

## Output Artifacts

- `.simug/chaos/<timestamp>/scenario1.log`
- `.simug/chaos/<timestamp>/scenario1-restart.log`
- `.simug/chaos/<timestamp>/scenario2.log`
- `.simug/chaos/<timestamp>/scenario2-restart.log`
- `.simug/chaos/<timestamp>/summary.json`

## Pass Criteria

- Both restart executions exit successfully.
- Working tree remains clean after each scenario.
- `.simug/state.json` exists and contains coherent mode/PR fields.
- Summary artifact is generated.

## Checklist Linkage

Treat successful chaos summary artifacts as mandatory evidence for
`docs/SELF_HOST_GO_NO_GO.md` before declaring self-host default readiness.
