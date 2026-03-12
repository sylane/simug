# Real Codex Validation Gate

This document defines the operator gate for real-runtime Codex validation before release/self-host readiness decisions.

## Purpose

Run both real-Codex canary suites as one gate:

- protocol conformance canary (`6.5a`)
- repair/restart recovery canary (`6.5b`)

Artifacts are retained for audit and failure diagnosis.

## Prerequisites

- `gh auth status` is healthy in the runtime session.
- `SIMUG_REAL_CODEX_CMD` runtime command is available (default auto-detect prefers `codex exec`).
- Network access is enabled for the Codex runtime environment.
- Workspace has write access to artifact root (default `.simug/canary/real-codex`).

## Execute Gate

```bash
scripts/canary-real-codex-gate.sh --cmd "codex exec" --out .simug/canary/real-codex --retain-days 14
```

If `--cmd` is omitted, scripts auto-detect and prefer non-interactive `codex exec`.
Scripts run a codex preflight probe and fail fast on missing auth or fatal runtime-path blockers before canary tests start.
Successful probes that still emit sandbox-style `~/.codex/tmp/arg0` maintenance warnings are surfaced as warnings and the gate continues.

## Expected Runtime/Cost Envelope

- Typical duration: 2-10 minutes depending on Codex latency.
- Token/runtime cost: bounded by canary prompt count; reruns should be deliberate and recorded.
- Failures are actionable and should be investigated before release gate pass.

## Output Artifacts

Per gate run:

- `gate-<timestamp>/protocol/...`: protocol canary scenario artifacts
- `gate-<timestamp>/recovery/...`: recovery canary scenario artifacts
- `gate-<timestamp>/summary.json`: gate metadata, directories, runtime duration

Retention:

- `--retain-days` prunes old gate run directories under output root.
- Keep at least one recent successful gate artifact set for release evidence.

## Pass/Fail Policy

- Pass: both canary scripts complete successfully and summary file is written.
- Fail: any canary failure, script error, or missing artifact output.

On failure:

1. Inspect scenario-level `result.json` and `raw_output.txt`.
2. Re-run failed canary with same command/output root to confirm reproducibility.
3. Open/track remediation task before declaring release readiness.
