# Live GitHub + Real Codex Sandbox Dry-Run

This runbook defines the 6.6 validation flow for end-to-end sandbox verification.

## Goal

Demonstrate, on a sandbox repository, that simug can complete:

- one issue-driven lifecycle (issue triage -> implementation PR -> merge -> issue finalization),
- one planning-driven lifecycle (normal task bootstrap -> PR -> merge),

without manual state repair.

## Prerequisites

- Real Codex gate passed recently (`scripts/canary-real-codex-gate.sh`) with retained artifacts.
- Authenticated `gh` session for sandbox repository owner.
- Sandbox repository with `main` branch and no conflicting open authored PRs.
- Simug binary available (`go build -o bin/simug ./cmd/simug`).

## Execution Outline

1. Prepare sandbox repository and create one authored open issue for issue-driven path.
2. Run simug with real Codex command in the sandbox checkout.
3. Complete one issue-driven PR to merge.
4. Complete one planning-driven PR to merge.
5. Record resulting PR numbers and optional issue number.

## Evidence Verification

Run:

```bash
scripts/sandbox-dry-run.sh \
  --repo <owner/name|path> \
  --issue-pr <issue_driven_pr_number> \
  --planning-pr <planning_driven_pr_number> \
  --issue <issue_number>
```

Notes:

- `--repo` accepts either `owner/name` or a local checkout path (for example `.`).
- This command verifies existing evidence; it does not create PRs.

Optional: include `--run-gate` to execute real-Codex canary gate before evidence verification.

The script writes summary evidence under:

- `.simug/canary/sandbox/dry-run-<timestamp>.json`

## Pass Criteria

- Both PRs are merged (`mergedAt` non-empty).
- Both PR authors match authenticated user.
- If `--issue` is provided, issue state is `CLOSED`.
- Summary artifact file is generated.

## Failure Handling

If verification fails:

1. Inspect `simug explain-last-failure` output from sandbox run logs.
2. Inspect `.simug/events.log` and canary gate artifacts.
3. Capture failure details in planning/history before rerun.
