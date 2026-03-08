#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/self-host-canary.sh [--repo <path>] [--iterations <n>] [--run-gate]

Runs an end-to-end self-hosting canary in two phases to emulate restart continuity:
  1) initial one-shot loop executions
  2) resumed one-shot loop executions after restart boundary
EOF
}

repo=""
iterations="2"
run_gate=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --iterations)
      iterations="${2:-}"
      shift 2
      ;;
    --run-gate)
      run_gate=1
      shift
      ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

if [[ -z "$repo" ]]; then
  repo="$(pwd)"
fi
repo="$(cd "$repo" && pwd)"

if ! [[ "$iterations" =~ ^[0-9]+$ ]] || [[ "$iterations" -lt 2 ]]; then
  echo "--iterations must be an integer >= 2" >&2
  exit 64
fi

cd "$repo"
mkdir -p .simug/selfhost-canary
canary_dir=".simug/selfhost-canary/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$canary_dir"

if [[ "$run_gate" -eq 1 ]]; then
  "$(dirname "$0")/canary-real-codex-gate.sh"
fi

phase1=$((iterations / 2))
phase2=$((iterations - phase1))

echo "self-host canary directory: $canary_dir"
echo "phase1 iterations: $phase1"
echo "phase2 iterations: $phase2"

"$(dirname "$0")/self-host-loop.sh" --repo "$repo" --iterations "$phase1" >"$canary_dir/phase1.log" 2>&1

# Simulated restart boundary.
sleep 1

"$(dirname "$0")/self-host-loop.sh" --repo "$repo" --iterations "$phase2" >"$canary_dir/phase2.log" 2>&1

latest_selfhost_dir="$(find .simug/selfhost -mindepth 1 -maxdepth 1 -type d | sort | tail -n 1)"
if [[ -z "$latest_selfhost_dir" ]]; then
  echo "missing self-host artifacts under .simug/selfhost" >&2
  exit 1
fi

state_path=".simug/state.json"
if [[ ! -f "$state_path" ]]; then
  echo "missing state file after self-host canary" >&2
  exit 1
fi
events_path=".simug/events.log"
if [[ ! -f "$events_path" ]]; then
  echo "missing events log after self-host canary" >&2
  exit 1
fi

cat > "$canary_dir/summary.json" <<EOF
{
  "repo": "$repo",
  "iterations": $iterations,
  "phase1_iterations": $phase1,
  "phase2_iterations": $phase2,
  "phase1_log": "$canary_dir/phase1.log",
  "phase2_log": "$canary_dir/phase2.log",
  "state_path": "$state_path",
  "events_path": "$events_path",
  "recorded_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

echo "self-host canary completed"
echo "summary: $canary_dir/summary.json"
