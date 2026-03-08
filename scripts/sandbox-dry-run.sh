#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/sandbox-dry-run.sh --repo <owner/name> --issue-pr <n> --planning-pr <n> [--issue <n>] [--run-gate]

Validates sandbox dry-run evidence for real GitHub + real Codex execution.
Checks merged PR evidence for issue-driven and planning-driven paths and writes summary artifacts.
EOF
}

repo=""
issue_pr=""
planning_pr=""
issue_number=""
run_gate=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      repo="${2:-}"
      shift 2
      ;;
    --issue-pr)
      issue_pr="${2:-}"
      shift 2
      ;;
    --planning-pr)
      planning_pr="${2:-}"
      shift 2
      ;;
    --issue)
      issue_number="${2:-}"
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

if [[ -z "$repo" || -z "$issue_pr" || -z "$planning_pr" ]]; then
  echo "--repo, --issue-pr, and --planning-pr are required" >&2
  exit 64
fi

if [[ "$run_gate" -eq 1 ]]; then
  "$(dirname "$0")/canary-real-codex-gate.sh"
fi

gh auth status >/dev/null
user="$(gh api user -q .login)"

validate_pr() {
  local pr_number="$1"
  local name="$2"
  local pr_state
  local merged_at
  local author
  pr_state="$(gh pr view "$pr_number" --repo "$repo" --json state --jq .state)"
  merged_at="$(gh pr view "$pr_number" --repo "$repo" --json mergedAt --jq .mergedAt)"
  author="$(gh pr view "$pr_number" --repo "$repo" --json author --jq .author.login)"
  if [[ "$pr_state" != "MERGED" && "$pr_state" != "CLOSED" ]]; then
    echo "$name PR is not merged/closed (state=$pr_state)" >&2
    exit 1
  fi
  if [[ -z "$merged_at" ]]; then
    echo "$name PR has empty mergedAt" >&2
    exit 1
  fi
  if [[ "$author" != "$user" ]]; then
    echo "$name PR author ($author) does not match authenticated user ($user)" >&2
    exit 1
  fi
}

validate_pr "$issue_pr" "issue-driven"
validate_pr "$planning_pr" "planning-driven"

issue_state=""
if [[ -n "$issue_number" ]]; then
  issue_state="$(gh issue view "$issue_number" --repo "$repo" --json state --jq .state)"
  if [[ "$issue_state" != "CLOSED" ]]; then
    echo "issue #$issue_number is not CLOSED (state=$issue_state)" >&2
    exit 1
  fi
fi

out_dir=".simug/canary/sandbox"
mkdir -p "$out_dir"
summary_path="$out_dir/dry-run-$(date -u +%Y%m%dT%H%M%SZ).json"

cat > "$summary_path" <<EOF
{
  "repo": "$repo",
  "user": "$user",
  "issue_pr": $issue_pr,
  "planning_pr": $planning_pr,
  "issue_number": "${issue_number}",
  "issue_state": "${issue_state}",
  "validated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

echo "sandbox dry-run evidence verified"
echo "summary: $summary_path"
