# simug

`simug` is a local single-repo orchestrator for Codex-driven pull request workflows.

It watches one GitHub PR lane at a time, feeds new PR events/comments to Codex, validates Codex output, and applies GitHub mutations (push/comments/PR creation) only through the orchestrator.

## Operator Contract

`simug` is intended to run as a reliable coordinator between:

- the manager (human operator),
- the orchestrator (`simug` itself),
- the coding agent session (Codex).

Contract:

- `simug` owns repository and GitHub side effects (push, PR/comment mutations, task progression).
- Codex produces protocol output and code changes, but does not directly mutate GitHub.
- Manager steering enters through authorized commands/comments and is validated by `simug`.
- Protocol and repository-state validations must pass before `simug` performs remote mutations.
- State is persisted so the loop can stop/restart safely and resume deterministically.

Operating model:

- Primary loop handles managed PR task work.
- After merge, loop can switch to issue triage before continuing planned tasks.
- A paused/manual-steering mode is expected where manager messages temporarily gate autonomous progression.

## What It Does

- Enforces one worker process per repo (`.simug/lock`).
- Finds at most one managed open PR for the authenticated user.
- Fails fast on ambiguous or desynchronized state.
- Polls issue comments, review comments, and reviews.
- Tracks processed events with persistent cursors in `.simug/state.json`.
- Invokes Codex with a strict `SIMUG: {json}` protocol.
- Validates branch policy, clean working tree, and commit expectations.
- Pushes/creates/updates PRs from the orchestrator only.

## Requirements

- Linux/macOS shell environment
- `git`
- `go` (1.22+)
- `gh` (GitHub CLI)
- Authenticated GitHub session (`gh auth login`)

## Install Dependencies (Ubuntu/Debian)

```bash
sudo apt-get update
sudo apt-get install -y golang-go curl

curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
sudo chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg

echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  | sudo tee /etc/apt/sources.list.d/github-cli.list >/dev/null

sudo apt-get update
sudo apt-get install -y gh
```

Verify:

```bash
go version
gh --version
git --version
```

Authenticate GitHub CLI:

```bash
gh auth login
```

## Build

From `simug` repo root:

```bash
go build -o bin/simug ./cmd/simug
```

This creates the binary at `./bin/simug` (inside this repo).

Install globally to your Go bin directory (recommended if you want `simug` available in any repo):

```bash
go install ./cmd/simug
```

Then ensure your Go bin directory is on `PATH` (commonly `~/go/bin`).

Run without building a binary (only from the `simug` source repo):

```bash
go run ./cmd/simug run
```

## Running in Any Repository

Yes, once built/installed you can run `simug` from any GitHub repository checkout.

- If installed on `PATH`: run `simug run` from the target repo.
- If not on `PATH`: use an absolute path (for example `/path/to/simug/bin/simug run`).
- Relative paths also work, but they are relative to your current directory.

`simug` detects the repository from your current working directory (`git rev-parse --show-toplevel`), so start it inside the repo you want to manage (repo root or any subdirectory).

## Configuration Surface

Current interface: environment variables.

Planned extension: command-line flags for common runtime options (without breaking env-based automation).

Current variables:

- `SIMUG_AGENT_CMD` (default: `codex`)
- `SIMUG_POLL_SECONDS` (default: `30`)
- `SIMUG_MAIN_BRANCH` (default: `main`)
- `SIMUG_BRANCH_PREFIX` (default: `agent/`)
- `SIMUG_MAX_REPAIR_ATTEMPTS` (default: `2`)
- `SIMUG_ALLOWED_COMMAND_USERS` (default: current authenticated user)
- `SIMUG_ALLOWED_COMMAND_VERBS` (default: `do,retry,status,continue,comment,report,help`)

Example:

```bash
export SIMUG_AGENT_CMD="codex"
export SIMUG_POLL_SECONDS=20
export SIMUG_ALLOWED_COMMAND_USERS="my-github-login,teammate-login"
export SIMUG_ALLOWED_COMMAND_VERBS="do,retry,status,comment"
```

## Codex Runtime Permissions (Self-Hosted `simug` Dev Only)

This section is only for developing `simug` itself with Codex.

For normal `simug` usage on another project repository:

- `simug` (the orchestrator process) needs `gh`/`git` network access.
- Codex does not need direct `gh` CLI access, because `simug` owns GitHub mutations.

If you do run Codex directly in this repo while developing `simug`, sandboxed `gh`/`git` network operations can fail even when local shell auth is valid.

Example Codex configuration for self-hosted `simug` development:

1. Enable network in workspace-write sandbox.
2. Keep approvals enabled (`on-request`) or run with full access for unattended flows.
3. Ensure Codex sees valid GitHub auth (`~/.config/gh/hosts.yml` or `GH_TOKEN`/`GITHUB_TOKEN`).

Example `~/.codex/config.toml`:

```toml
[profiles.simug]
approval_policy = "on-request"
sandbox_mode = "workspace-write"

[sandbox_workspace_write]
network_access = true
```

Use that profile when launching Codex (or in `SIMUG_AGENT_CMD`) for this self-hosted workflow:

```bash
codex --profile simug
export SIMUG_AGENT_CMD="codex --profile simug"
```

One-off equivalent without editing config file (self-hosted workflow):

```bash
codex --sandbox workspace-write --ask-for-approval on-request \
  --config sandbox_workspace_write.network_access=true
```

If you need fully unattended execution (no approval prompts), use only in a trusted/dev container:

```bash
codex --sandbox danger-full-access --ask-for-approval never
# or: codex --dangerously-bypass-approvals-and-sandbox
```

Quick verification from the same Codex session (self-hosted workflow):

```bash
gh auth status
gh api user -q .login
gh pr list --limit 1
git ls-remote origin >/dev/null
curl -I https://api.github.com
```

If `gh auth status` reports `token ... is invalid` but `curl` cannot resolve `api.github.com`, the root cause is sandbox/network restriction, not token validity.

## How to Use

1. `cd` into a GitHub repository checkout.
2. Ensure `origin` points to GitHub.
3. Ensure working tree is clean.
4. Start worker:

```bash
simug run
# one-shot mode (single tick):
simug run --once
# or with absolute path:
/path/to/simug/bin/simug run
```

5. Drive execution through PR comments using `/agent ...` commands from authorized users.
6. Let `simug` keep running and monitoring PR events.

When a run fails, get a one-command diagnosis:

```bash
simug explain-last-failure
```

## Managed PR Rules

A PR is managed only when:

- it is open,
- it is authored by the authenticated GitHub user,
- its head branch matches the managed pattern (default `agent/<timestamp>-<slug>`).

If more than one authored open PR exists, worker exits with a clear error to prevent desync.

## Self-Host Loop Helper

For simug-on-simug development, use the wrapper script:

```bash
scripts/self-host-loop.sh --iterations 5
```

What it does per iteration:

- rebuilds `bin/simug`,
- runs `./bin/simug run --once`,
- captures stdout/stderr and state snapshots under `.simug/selfhost/<timestamp>/`.

The script exits immediately on the first non-zero `simug` exit code so supervisor behavior stays deterministic.

## Runtime Files

`simug` writes:

- `.simug/state.json` (persistent cursors and active PR state)
- `.simug/lock` (single-process guard)
- `.simug/events.log` (JSONL event/audit log)
- `.simug/archive/agent/...` (per-attempt Codex prompt/output archival artifacts)

`events.log` includes high-fidelity trace entries (`command_trace`, `invariant_decision`, `tick_start`, `tick_end`) with run/tick correlation IDs for post-failure reconstruction.

## Codex Protocol

Codex must emit machine-readable lines:

```text
SIMUG_MANAGER: <human-friendly manager message>
SIMUG: {"action":"comment","body":"..."}
SIMUG: {"action":"reply","comment_id":123,"body":"..."}
SIMUG: {"action":"done","summary":"...","changes":true}
SIMUG: {"action":"idle","reason":"..."}
```

Rules:

- exactly one terminal action (`done` or `idle`),
- malformed protocol is treated as failure,
- manager-facing human text must use `SIMUG_MANAGER:` prefix,
- unprefixed non-empty output lines are quarantined by the orchestrator (not treated as protocol),
- Codex must not push or create PR directly.

## Typical Dev Loop (Self-Hosted)

- Start `simug` in this repository.
- Use GitHub comments (`/agent ...`) to steer work.
- `simug` invokes Codex, validates output, pushes, and posts replies/comments.
- Repeat until PR merge, then it can bootstrap the next task.

## Troubleshooting

- `multiple open PRs authored by ...`
  - close/merge extra PRs so only one managed lane remains.
- `checkout mismatch for PR ...`
  - checkout correct branch and sync local/remote/PR head.
- `working tree is dirty`
  - commit/stash/clean before running.
- `agent failed validation after ... attempts`
  - inspect `.simug/events.log` and last Codex output contract.
- need a fast failure summary
  - run `simug explain-last-failure` to get the last failed tick reason, invariant context, and suggested next action.
- `gh ... failed`
  - verify `gh auth status`, repo permissions, and network access.

## Developer Verification

```bash
gofmt -w $(find . -name '*.go')
go test ./...
GOCACHE=/tmp/go-build GOEXPERIMENT=nocoverageredesign go test ./... -coverprofile=coverage.out
GOCACHE=/tmp/go-build go tool cover -func=coverage.out
```

## Documentation

- [Design](docs/DESIGN.md)
- [Workflow](docs/WORKFLOW.md)
- [Planning](docs/PLANNING.md)
