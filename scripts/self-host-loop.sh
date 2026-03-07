#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/self-host-loop.sh [--repo <path>] [--iterations <n>]

Builds simug, executes `simug run --once` repeatedly, and captures per-iteration logs/state snapshots under:
  .simug/selfhost/<timestamp>/
EOF
}

repo=""
iterations=1

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

if ! [[ "$iterations" =~ ^[0-9]+$ ]] || [[ "$iterations" -lt 1 ]]; then
  echo "--iterations must be a positive integer" >&2
  exit 64
fi

cd "$repo"
mkdir -p bin .simug/selfhost

run_dir=".simug/selfhost/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run_dir"
echo "self-host run directory: $run_dir"

for ((i = 1; i <= iterations; i++)); do
  iter_tag="$(printf 'iter-%03d' "$i")"
  log_path="$run_dir/${iter_tag}.log"
  state_snapshot="$run_dir/${iter_tag}.state.json"

  echo "[$i/$iterations] building bin/simug"
  GOCACHE=/tmp/go-build go build -o bin/simug ./cmd/simug

  echo "[$i/$iterations] running one-shot tick (log: $log_path)"
  set +e
  ./bin/simug run --once >"$log_path" 2>&1
  rc=$?
  set -e

  if [[ -f .simug/state.json ]]; then
    cp .simug/state.json "$state_snapshot"
  fi

  echo "[$i/$iterations] exit_code=$rc"
  if [[ "$rc" -ne 0 ]]; then
    echo "self-host loop stopped due to non-zero simug exit code" >&2
    exit "$rc"
  fi
done

echo "self-host loop completed successfully"
