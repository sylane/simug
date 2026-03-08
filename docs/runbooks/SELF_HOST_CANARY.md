# Self-Hosting Canary Workflow

This runbook defines the scripted canary for simug-on-simug validation.

## Goal

Verify that self-hosted one-shot loops remain stable across a restart boundary and produce consistent runtime artifacts.

## Command

```bash
scripts/self-host-canary.sh --repo . --iterations 4
```

Optional:

- `--run-gate`: execute real-Codex validation gate before self-host canary.

## Behavior

The script executes in two phases:

1. Phase 1 runs `self-host-loop.sh` for half of iterations.
2. Restart boundary is simulated.
3. Phase 2 runs `self-host-loop.sh` for remaining iterations.

Outputs:

- `.simug/selfhost-canary/<timestamp>/phase1.log`
- `.simug/selfhost-canary/<timestamp>/phase2.log`
- `.simug/selfhost-canary/<timestamp>/summary.json`
- Existing runtime artifacts (`.simug/state.json`, `.simug/events.log`, `.simug/selfhost/...`)

## Pass Criteria

- Both phases complete with zero exit code.
- `.simug/state.json` and `.simug/events.log` exist after run.
- Summary file is generated and references both phase logs.

## Failure Handling

1. Inspect phase logs in canary output directory.
2. Run `simug explain-last-failure`.
3. Review `.simug/events.log` and latest archive attempt artifacts.
