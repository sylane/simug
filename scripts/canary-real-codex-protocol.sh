#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/canary-real-codex-protocol.sh [--cmd <agent-cmd>] [--out <dir>]

Runs real-Codex protocol conformance canary scenarios through internal/agent test harness.
Artifacts are written under .simug/canary/real-codex by default.
Default command prefers non-interactive Codex (`codex exec`) when available.
EOF
}

default_agent_cmd() {
  if command -v codex >/dev/null 2>&1; then
    if codex exec --help >/dev/null 2>&1; then
      printf 'codex exec'
      return
    fi
    printf 'codex'
    return
  fi
  printf 'codex exec'
}

preflight_agent_cmd() {
  if [[ "$agent_cmd" != codex* ]]; then
    return 0
  fi

  local output
  output="$(bash -lc "$agent_cmd --help" 2>&1 || true)"

  if printf '%s' "$output" | grep -qi "permission denied" && \
     printf '%s' "$output" | grep -qiE "\\.codex/tmp/arg0|failed to clean up stale arg0 temp dirs|failed to renew cache ttl|could not update path"; then
    echo "codex preflight failed: runtime paths appear unwritable; fix ~/.codex permissions (especially ~/.codex/tmp/arg0) or set CODEX_HOME/CODEX_SQLITE_HOME to writable paths" >&2
    return 2
  fi

  if printf '%s' "$output" | grep -qiE "401 unauthorized|invalid_api_key|authentication failed"; then
    echo "codex preflight failed: authentication appears invalid or missing; run codex login in this environment" >&2
    return 2
  fi
}

agent_cmd="${SIMUG_REAL_CODEX_CMD:-$(default_agent_cmd)}"
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

preflight_agent_cmd

SIMUG_REAL_CODEX=1 \
SIMUG_REAL_CODEX_CMD="$agent_cmd" \
SIMUG_CANARY_OUT_DIR="$out_dir" \
GOCACHE=/tmp/go-build \
go test ./internal/agent -run TestRealCodexProtocolConformanceCanary -count=1 -v
