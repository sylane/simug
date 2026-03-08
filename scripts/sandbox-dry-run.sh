#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: scripts/sandbox-dry-run.sh --repo <owner/name|path> --issue-pr <n> --planning-pr <n> [--issue <n>] [--run-gate]

Validates sandbox dry-run evidence for real GitHub + real Codex execution.
Checks merged PR evidence for issue-driven and planning-driven paths and writes summary artifacts.
EOF
}

die() {
  echo "error: $*" >&2
  exit 64
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    die "missing required command '$cmd' (install it and retry)"
  fi
}

is_uint() {
  [[ "$1" =~ ^[0-9]+$ ]] && [[ "$1" -gt 0 ]]
}

parse_repo_slug_from_remote() {
  local remote_url="$1"
  local slug
  slug="$(printf '%s' "$remote_url" | sed -E 's#^(git@|ssh://git@|https://|http://)?github\.com[:/]##; s#\.git$##')"
  if [[ "$slug" =~ ^[^/]+/[^/]+$ ]]; then
    printf '%s\n' "$slug"
    return 0
  fi
  return 1
}

resolve_repo_slug() {
  local repo_arg="$1"
  if [[ -d "$repo_arg" ]]; then
    if ! git -C "$repo_arg" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
      die "--repo path '$repo_arg' is not a git repository checkout"
    fi
    if ! git -C "$repo_arg" remote get-url origin >/dev/null 2>&1; then
      die "--repo path '$repo_arg' has no 'origin' remote; set it first (e.g. git -C \"$repo_arg\" remote add origin git@github.com:<owner>/<repo>.git)"
    fi
    local from_gh
    from_gh="$(cd "$repo_arg" && gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || true)"
    if [[ "$from_gh" =~ ^[^/]+/[^/]+$ ]]; then
      printf '%s\n' "$from_gh"
      return 0
    fi
    local remote_url
    remote_url="$(git -C "$repo_arg" remote get-url origin)"
    if parse_repo_slug_from_remote "$remote_url"; then
      return 0
    fi
    die "could not resolve owner/name from '$repo_arg' origin remote '$remote_url'"
  fi

  if [[ "$repo_arg" =~ ^[^/]+/[^/]+$ ]]; then
    printf '%s\n' "$repo_arg"
    return 0
  fi
  die "--repo must be either <owner>/<name> or a local git checkout path"
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
  usage >&2
  die "--repo, --issue-pr, and --planning-pr are required (example: make sandbox-dry-run REPO=. ISSUE_PR=123 PLANNING_PR=124)"
fi

require_cmd gh
require_cmd git
gh auth status >/dev/null 2>&1 || die "gh is not authenticated; run 'gh auth login' and retry"

is_uint "$issue_pr" || die "--issue-pr must be a positive integer"
is_uint "$planning_pr" || die "--planning-pr must be a positive integer"
if [[ -n "$issue_number" ]]; then
  is_uint "$issue_number" || die "--issue must be a positive integer"
fi

repo_slug="$(resolve_repo_slug "$repo")"

if [[ "$run_gate" -eq 1 ]]; then
  "$(dirname "$0")/canary-real-codex-gate.sh"
fi

user="$(gh api user -q .login)"

validate_pr() {
  local pr_number="$1"
  local name="$2"
  local pr_state
  local merged_at
  local author
  pr_state="$(gh pr view "$pr_number" --repo "$repo_slug" --json state --jq .state)"
  merged_at="$(gh pr view "$pr_number" --repo "$repo_slug" --json mergedAt --jq .mergedAt)"
  author="$(gh pr view "$pr_number" --repo "$repo_slug" --json author --jq .author.login)"
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
  issue_state="$(gh issue view "$issue_number" --repo "$repo_slug" --json state --jq .state)"
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
  "repo_slug": "$repo_slug",
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
