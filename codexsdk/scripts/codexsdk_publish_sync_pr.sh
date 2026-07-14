#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/codexsdk_publish_sync_pr.sh --land-ref <branch> --target-ref <ref> --target-kind <kind> --target-sha <sha> [options]

Options:
  --branch-prefix <prefix>  Sync branch prefix. Defaults to codex/sync-upstream.
  --candidate <path>        Candidate schema directory validated against the checked-in baseline.
  --default-branch <branch> Repository default branch. Inferred from <remote>/HEAD when omitted.
  --drift-analysis <path>   Drift analysis markdown to include in the PR body.
  --drift-sha <sha>         Drift fingerprint that produced this sync candidate.
  --remote <name>           Git remote to fetch and push. Defaults to origin.
  --target-kind <kind>      Upstream target kind, such as stable_rust_tag.

The script assumes HEAD is the committed sync change. It validates, rebases onto
the current remote landing ref, pushes a sync branch with force-with-lease when
updating an existing sync branch, then creates or updates a PR against the
landing ref.
EOF
}

branch_prefix="codex/sync-upstream"
candidate=""
default_branch=""
drift_analysis=""
drift_sha=""
land_ref=""
remote="origin"
target_ref=""
target_kind=""
target_sha=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --branch-prefix)
      branch_prefix="$2"
      shift 2
      ;;
    --candidate)
      candidate="$2"
      shift 2
      ;;
    --default-branch)
      default_branch="$2"
      shift 2
      ;;
    --drift-analysis)
      drift_analysis="$2"
      shift 2
      ;;
    --drift-sha)
      drift_sha="$2"
      shift 2
      ;;
    --land-ref)
      land_ref="$2"
      shift 2
      ;;
    --remote)
      remote="$2"
      shift 2
      ;;
    --target-ref)
      target_ref="$2"
      shift 2
      ;;
    --target-kind)
      target_kind="$2"
      shift 2
      ;;
    --target-sha)
      target_sha="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${land_ref}" || -z "${target_ref}" || -z "${target_kind}" || -z "${target_sha}" ]]; then
  usage >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

normalize_branch_ref() {
  local ref=$1
  ref="${ref#refs/heads/}"
  ref="${ref#refs/remotes/${remote}/}"
  ref="${ref#${remote}/}"
  printf '%s\n' "${ref}"
}

resolve_default_branch() {
  local symbolic_ref
  local remote_head

  if [[ -n "${default_branch}" ]]; then
    normalize_branch_ref "${default_branch}"
    return 0
  fi

  if symbolic_ref="$(git symbolic-ref --quiet --short "refs/remotes/${remote}/HEAD" 2>/dev/null)"; then
    normalize_branch_ref "${symbolic_ref}"
    return 0
  fi

  remote_head="$(
    git remote show "${remote}" 2>/dev/null |
      sed -n 's/^[[:space:]]*HEAD branch: //p' |
      head -n 1
  )"
  if [[ -n "${remote_head}" && "${remote_head}" != "(unknown)" ]]; then
    normalize_branch_ref "${remote_head}"
    return 0
  fi

  echo "unable to determine repository default branch; pass --default-branch explicitly" >&2
  return 1
}

land_ref="$(normalize_branch_ref "${land_ref}")"
default_branch="$(resolve_default_branch)"
if [[ "${land_ref}" != "${default_branch}" ]]; then
  echo "Refusing landing ref ${land_ref}; sync PRs may target only repository default branch ${default_branch}." >&2
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "worktree must be clean before publishing a sync PR" >&2
  git status --short >&2
  exit 1
fi

write_output() {
  local name=$1
  local value=$2
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf '%s=%s\n' "${name}" "${value}" >> "${GITHUB_OUTPUT}"
  fi
}

validate_sync() {
  local args=(
    --target-sha "${target_sha}"
  )
  if [[ -n "${candidate}" ]]; then
    args+=(--candidate "${candidate}")
  fi
  scripts/codexsdk_validate_sync.sh "${args[@]}" || return 1
  if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "validation changed the committed sync tree; commit validation changes before publishing" >&2
    git status --short >&2
    return 1
  fi
}

confirm_target_still_points_at_sha() {
  local resolved_sha
  resolved_sha="$(
    python3 scripts/codexsdk_resolve_upstream.py \
      --upstream-ref "${target_ref}" \
      --json |
      jq -r '.upstream_sha'
  )"
  if [[ "${resolved_sha}" != "${target_sha}" ]]; then
    echo "upstream target moved: ${target_ref} resolved to ${resolved_sha}, expected ${target_sha}" >&2
    return 1
  fi
}

fetch_landing_ref() {
  git fetch "${remote}" "refs/heads/${land_ref}:refs/remotes/${remote}/${land_ref}"
}

sync_branch_name() {
  python3 - "$branch_prefix" "$target_ref" "$target_sha" <<'PY'
import re
import sys

prefix, target_ref, target_sha = sys.argv[1:4]
name = re.sub(r"^refs/(heads|tags)/", "", target_ref) or target_sha[:12]
name = re.sub(r"[^A-Za-z0-9._-]+", "-", name).strip("-.")
if not name:
    name = target_sha[:12]
print(f"{prefix.rstrip('-/')}-{name[:80]}")
PY
}

push_sync_branch() {
  local sync_branch=$1
  local expected_remote=""

  if git fetch "${remote}" "refs/heads/${sync_branch}:refs/remotes/${remote}/${sync_branch}" 2>/dev/null; then
    expected_remote="$(git rev-parse "refs/remotes/${remote}/${sync_branch}")"
  fi

  if [[ -n "${expected_remote}" ]]; then
    git push \
      --force-with-lease="refs/heads/${sync_branch}:${expected_remote}" \
      "${remote}" "HEAD:refs/heads/${sync_branch}"
  else
    git push "${remote}" "HEAD:refs/heads/${sync_branch}"
  fi
}

render_drift_analysis() {
  if [[ -z "${drift_analysis}" || ! -f "${drift_analysis}" ]]; then
    printf '%s\n' "_No drift analysis artifact was provided._"
    return 0
  fi

  cat "${drift_analysis}"
}

create_or_update_pr() {
  local sync_branch=$1
  local sync_commit=$2
  local title
  local body_file
  local pr_number
  local pr_url

  if ! command -v gh >/dev/null 2>&1; then
    echo "gh is required to create or update the sync PR" >&2
    return 1
  fi

  title="Sync Codex protocol baseline to ${target_ref}"
  body_file="$(mktemp)"
  cat > "${body_file}" <<EOF
<!-- codexsdk-upstream-sync
phase: fix
upstream_ref: ${target_ref}
upstream_ref_kind: ${target_kind}
upstream_commit: ${target_sha}
drift_sha256: ${drift_sha}
sync_commit: ${sync_commit}
base_branch: ${land_ref}
-->

Automated upstream protocol sync.

## Drift Analysis

$(render_drift_analysis)

## Fix Description

This PR applies the regenerated upstream schema candidate, runs the bounded Codex repair pass for handwritten compatibility, validates the synchronized baseline, commits the result, and publishes it through the protected PR path.

It does not merge itself, tag, or bypass branch protection. The finalize workflow owns landed verification, sync tag handling, and drift verification after merge.

## Sync Metadata

- Upstream ref: \`${target_ref}\`
- Upstream ref kind: \`${target_kind}\`
- Upstream commit: \`${target_sha}\`
- Drift fingerprint: \`${drift_sha:-not provided}\`
- Sync commit: \`${sync_commit}\`
- Base branch: \`${land_ref}\`

This PR was generated by the upstream protocol sync workflow. It stops at the protected PR boundary and does not tag or bypass branch protection. Merge should happen only through branch protection, repository auto-merge rules, and the required \`Go\` check on this head commit.

After this PR lands, the post-merge finalize trigger should pick up this PR automatically. Scheduled finalize remains the fallback, and manual finalize remains the explicit recovery path.
EOF

  pr_number="$(
    gh pr list \
      --base "${land_ref}" \
      --head "${sync_branch}" \
      --state open \
      --json number \
      --jq '.[0].number // empty'
  )"

  if [[ -n "${pr_number}" ]]; then
    gh pr edit "${pr_number}" --title "${title}" --body-file "${body_file}" >/dev/null
    pr_url="$(gh pr view "${pr_number}" --json url --jq '.url')"
  else
    pr_url="$(
      gh pr create \
        --base "${land_ref}" \
        --head "${sync_branch}" \
        --title "${title}" \
        --body-file "${body_file}"
    )"
    pr_number="$(gh pr view "${pr_url}" --json number --jq '.number')"
  fi

  rm -f "${body_file}"
  write_output "pr_number" "${pr_number}"
  write_output "pr_url" "${pr_url}"
  printf '%s\n' "${pr_url}"
}

validate_sync
fetch_landing_ref
git rebase "${remote}/${land_ref}"
confirm_target_still_points_at_sha
validate_sync

sync_branch="$(sync_branch_name)"
sync_commit="$(git rev-parse HEAD)"
push_sync_branch "${sync_branch}"

write_output "sync_branch" "${sync_branch}"
write_output "sync_commit" "${sync_commit}"

create_or_update_pr "${sync_branch}" "${sync_commit}"
