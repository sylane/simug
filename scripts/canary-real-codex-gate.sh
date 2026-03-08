#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/canary-real-codex-gate.sh [--cmd <agent-cmd>] [--out <dir>] [--retain-days <n>]

Runs both real-Codex canaries (protocol + recovery) as a single validation gate.
Writes gate summary and preserves per-canary artifacts under the selected output root.
EOF
}

agent_cmd="codex"
out_dir=".simug/canary/real-codex"
retain_days="14"

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
    --retain-days)
      retain_days="${2:-}"
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

if ! [[ "$retain_days" =~ ^[0-9]+$ ]]; then
  echo "--retain-days must be a non-negative integer" >&2
  exit 64
fi

mkdir -p "$out_dir"
gate_run="$out_dir/gate-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$gate_run"

start_epoch="$(date +%s)"
echo "running real Codex validation gate"
echo "agent command: $agent_cmd"
echo "gate run directory: $gate_run"

"$(dirname "$0")/canary-real-codex-protocol.sh" --cmd "$agent_cmd" --out "$gate_run/protocol"
"$(dirname "$0")/canary-real-codex-recovery.sh" --cmd "$agent_cmd" --out "$gate_run/recovery"

end_epoch="$(date +%s)"
duration="$((end_epoch-start_epoch))"

cat > "$gate_run/summary.json" <<EOF
{
  "gate_run_dir": "$gate_run",
  "agent_cmd": "$agent_cmd",
  "protocol_canary_dir": "$gate_run/protocol",
  "recovery_canary_dir": "$gate_run/recovery",
  "duration_seconds": $duration,
  "retain_days": $retain_days,
  "recorded_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

if [[ "$retain_days" -gt 0 ]]; then
  find "$out_dir" -mindepth 1 -maxdepth 1 -type d -mtime +"$retain_days" -exec rm -rf {} +
fi

echo "real Codex validation gate completed"
echo "summary: $gate_run/summary.json"
