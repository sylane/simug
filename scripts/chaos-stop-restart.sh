#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/chaos-stop-restart.sh [--repo <path>] [--agent-cmd <cmd>] [--sleep-seconds <n>]

Runs stop/restart chaos scenarios against simug and validates basic recovery invariants.
EOF
}

repo=""
agent_cmd='printf '\''SIMUG: {"action":"idle","reason":"chaos"}\n'\'''
sleep_seconds="2"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --agent-cmd)
      agent_cmd="${2:-}"
      shift 2
      ;;
    --sleep-seconds)
      sleep_seconds="${2:-}"
      shift 2
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

if ! [[ "$sleep_seconds" =~ ^[0-9]+$ ]] || [[ "$sleep_seconds" -lt 1 ]]; then
  echo "--sleep-seconds must be an integer >= 1" >&2
  exit 64
fi

cd "$repo"
mkdir -p bin .simug/chaos

run_dir=".simug/chaos/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run_dir"

GOCACHE=/tmp/go-build go build -o bin/simug ./cmd/simug

validate_invariants() {
  local label="$1"
  local git_status
  git_status="$(git status --porcelain)"
  if [[ -n "$git_status" ]]; then
    echo "[$label] dirty tree detected after chaos scenario" >&2
    echo "$git_status" >&2
    exit 1
  fi

  local state_path=".simug/state.json"
  if [[ ! -f "$state_path" ]]; then
    echo "[$label] missing state file" >&2
    exit 1
  fi

  local mode
  local active_pr
  local active_branch
  mode="$(sed -n 's/.*"mode": "\(.*\)".*/\1/p' "$state_path" | head -n 1)"
  active_pr="$(sed -n 's/.*"active_pr": \([0-9]*\).*/\1/p' "$state_path" | head -n 1)"
  active_branch="$(sed -n 's/.*"active_branch": "\(.*\)".*/\1/p' "$state_path" | head -n 1)"

  if [[ -z "$mode" ]]; then
    echo "[$label] state mode is empty" >&2
    exit 1
  fi
  if [[ -n "$active_pr" && "$active_pr" -gt 0 && "$mode" != "managed_pr" ]]; then
    echo "[$label] active_pr/mode coherence failed (active_pr=$active_pr mode=$mode)" >&2
    exit 1
  fi
  if [[ "$mode" == "managed_pr" && -z "$active_branch" ]]; then
    echo "[$label] managed_pr mode requires non-empty active_branch" >&2
    exit 1
  fi
}

echo "scenario 1: SIGTERM during continuous run"
SIMUG_AGENT_CMD="$agent_cmd" ./bin/simug run >"$run_dir/scenario1.log" 2>&1 &
pid1=$!
sleep "$sleep_seconds"
kill -TERM "$pid1" || true
wait "$pid1" || true

echo "scenario 1 restart: run --once"
SIMUG_AGENT_CMD="$agent_cmd" ./bin/simug run --once >"$run_dir/scenario1-restart.log" 2>&1 || {
  cat "$run_dir/scenario1-restart.log" >&2
  exit 1
}
validate_invariants "scenario1"

echo "scenario 2: SIGKILL during continuous run"
SIMUG_AGENT_CMD="$agent_cmd" ./bin/simug run >"$run_dir/scenario2.log" 2>&1 &
pid2=$!
sleep "$sleep_seconds"
kill -KILL "$pid2" || true
wait "$pid2" || true

echo "scenario 2 restart: run --once"
SIMUG_AGENT_CMD="$agent_cmd" ./bin/simug run --once >"$run_dir/scenario2-restart.log" 2>&1 || {
  cat "$run_dir/scenario2-restart.log" >&2
  exit 1
}
validate_invariants "scenario2"

cat > "$run_dir/summary.json" <<EOF
{
  "repo": "$repo",
  "agent_cmd": "$agent_cmd",
  "sleep_seconds": $sleep_seconds,
  "scenario1_log": "$run_dir/scenario1.log",
  "scenario1_restart_log": "$run_dir/scenario1-restart.log",
  "scenario2_log": "$run_dir/scenario2.log",
  "scenario2_restart_log": "$run_dir/scenario2-restart.log",
  "validated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

echo "chaos stop/restart validation completed"
echo "summary: $run_dir/summary.json"
