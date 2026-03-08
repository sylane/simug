#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/canary-real-codex-protocol.sh [--cmd <agent-cmd>] [--out <dir>]

Runs real-Codex protocol conformance canary scenarios through internal/agent test harness.
Artifacts are written under .simug/canary/real-codex by default.
EOF
}

agent_cmd="codex"
out_dir=".simug/canary/real-codex"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cmd)
      agent_cmd="${2:-}"
      shift 2
      ;;
    --out)
      out_dir="${2:-}"
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

mkdir -p "$out_dir"

echo "running real Codex protocol canary"
echo "agent command: $agent_cmd"
echo "artifact root: $out_dir"

SIMUG_REAL_CODEX=1 \
SIMUG_REAL_CODEX_CMD="$agent_cmd" \
SIMUG_CANARY_OUT_DIR="$out_dir" \
GOCACHE=/tmp/go-build \
go test ./internal/agent -run TestRealCodexProtocolConformanceCanary -count=1 -v
