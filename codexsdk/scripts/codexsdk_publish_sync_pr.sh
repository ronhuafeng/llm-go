#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/codexsdk_publish_sync_pr.sh --land-ref <branch> --target-ref <ref> --target-kind <kind> --target-sha <sha> --validated-commit <sha> [options]

Options:
  --branch-prefix <prefix>  Sync branch prefix. Defaults to codex/sync-upstream.
  --default-branch <branch> Repository default branch. Inferred from <remote>/HEAD when omitted.
  --remote <name>           Git remote to fetch and push. Defaults to origin.
  --target-kind <kind>      Upstream target kind, such as stable_rust_tag.
  --validated-commit <sha>  Exact commit native checks accepted before publication.

The script assumes HEAD is the committed sync change. It publishes that exact
commit without rebasing or substituting a later landing-ref state. It reuses an
existing open PR only when that PR is an idempotent publication of the same
head, base, and upstream identity; otherwise it pushes a target-SHA-bound sync
branch without overwriting a different remote commit.
EOF
}

branch_prefix="codex/sync-upstream"
default_branch=""
land_ref=""
remote="origin"
target_ref=""
target_kind=""
target_sha=""
validated_commit=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --branch-prefix)
      branch_prefix="$2"
      shift 2
      ;;
    --default-branch)
      default_branch="$2"
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
    --validated-commit)
      validated_commit="$2"
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

if [[ -z "${land_ref}" || -z "${target_ref}" || -z "${target_kind}" || -z "${target_sha}" || -z "${validated_commit}" ]]; then
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

if [[ -n "$(git status --porcelain --untracked-files=all)" ]]; then
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

require_validated_commit() {
  if [[ -z "${validated_commit}" ]]; then
    echo "publish requires a workflow-validated commit; correctness proofs are owned by Actions YAML" >&2
    return 1
  fi
}

confirm_clean_tree() {
  if [[ -n "$(git status --porcelain --untracked-files=all)" ]]; then
    echo "publication tree is dirty; workflow proofs must leave a clean worktree" >&2
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
      jq -r '.peeled_commit_sha'
  )"
  if [[ "${resolved_sha}" != "${target_sha}" ]]; then
    echo "upstream target moved: ${target_ref} resolved to ${resolved_sha}, expected ${target_sha}" >&2
    return 1
  fi
}

fetch_landing_ref() {
  git fetch "${remote}" "refs/heads/${land_ref}:refs/remotes/${remote}/${land_ref}"
}

find_existing_target_pr() {
  if ! command -v gh >/dev/null 2>&1; then
    echo "gh is required to find an existing sync PR" >&2
    return 1
  fi

  CODEXSDK_PR_LIST_JSON="$(
    gh pr list \
      --state open \
      --limit 100 \
      --json number,url,body,baseRefName,headRefOid
  )"
  CODEXSDK_PR_LIST_JSON="${CODEXSDK_PR_LIST_JSON}" python3 - "${land_ref}" "${target_ref}" "${target_kind}" "${target_sha}" "${validated_commit}" <<'PY'
import json
import os
import sys

land_ref, target_ref, target_kind, target_sha, validated_commit = sys.argv[1:6]
prs = json.loads(os.environ["CODEXSDK_PR_LIST_JSON"])
if not isinstance(prs, list):
    raise SystemExit("gh pr list did not return a JSON array")

def metadata(body: str) -> dict[str, str]:
    start = body.find("<!-- codexsdk-upstream-sync")
    if start < 0:
        return {}
    end = body.find("-->", start)
    if end < 0:
        return {}
    parsed: dict[str, str] = {}
    for line in body[start:end].splitlines():
        if ":" not in line:
            continue
        key, _, value = line.partition(":")
        key = key.strip()
        if key in {
            "upstream_ref",
            "upstream_ref_kind",
            "upstream_commit",
            "sync_commit",
            "base_branch",
        }:
            parsed[key] = value.strip()
    return parsed

matches = []
conflicts = []
for pr in prs:
    body = str(pr.get("body") or "")
    meta = metadata(body)
    if not meta or meta.get("upstream_commit") != target_sha:
        continue
    reasons = []
    if str(pr.get("baseRefName") or "") != land_ref or meta.get("base_branch", land_ref) != land_ref:
        reasons.append(f"base={pr.get('baseRefName')!s}/{meta.get('base_branch', '')}")
    if str(pr.get("headRefOid") or "") != validated_commit or meta.get("sync_commit", validated_commit) != validated_commit:
        reasons.append(f"head={pr.get('headRefOid')!s}/{meta.get('sync_commit', '')}")
    if meta.get("upstream_ref") != target_ref:
        reasons.append(f"upstream_ref={meta.get('upstream_ref', '')}")
    if meta.get("upstream_ref_kind") != target_kind:
        reasons.append(f"upstream_ref_kind={meta.get('upstream_ref_kind', '')}")
    if reasons:
        conflicts.append((pr.get("number"), reasons))
    else:
        matches.append(pr)

if conflicts:
    detail = "; ".join(f"#{number} ({', '.join(reasons)})" for number, reasons in conflicts)
    raise SystemExit(
        f"existing sync PR for {target_sha} is not an exact publication of the validated commit: {detail}"
    )
if len(matches) > 1:
    numbers = ", ".join(f"#{pr.get('number')}" for pr in matches)
    raise SystemExit(f"multiple exact sync PRs for {target_sha}: {numbers}")
if matches:
    pr = matches[0]
    sys.stdout.write(f"{pr.get('number')}\t{pr.get('url')}\n")
PY
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
print(f"{prefix.rstrip('-/')}-{name[:64]}-{target_sha[:12]}")
PY
}

push_sync_branch() {
  local sync_branch=$1
  local expected_remote=""

  if git fetch "${remote}" "refs/heads/${sync_branch}:refs/remotes/${remote}/${sync_branch}" 2>/dev/null; then
    expected_remote="$(git rev-parse "refs/remotes/${remote}/${sync_branch}")"
  fi

  if [[ -n "${expected_remote}" ]]; then
    if [[ "${expected_remote}" == "$(git rev-parse HEAD)" ]]; then
      return 0
    fi
    echo "Refusing to overwrite existing sync branch ${sync_branch} at ${expected_remote}." >&2
    echo "Recover or close the existing sync PR before retrying this target." >&2
    return 1
  fi
  git push "${remote}" "HEAD:refs/heads/${sync_branch}"
}

create_or_update_pr() {
  local sync_branch=$1
  local sync_commit=$2
  local title
  local body_file
  local pr_number
  local pr_url
  local fix_description

  if ! command -v gh >/dev/null 2>&1; then
    echo "gh is required to create or update the sync PR" >&2
    return 1
  fi

  title="Sync Codex protocol baseline to ${target_ref}"
  fix_description="This PR advances the checked-in protocol baseline for the selected upstream target, regenerates deterministic SDK artifacts, includes any one-run handwritten updates required by real drift, and publishes only after native checks passed."
  body_file="$(mktemp)"
  cat > "${body_file}" <<EOF
<!-- codexsdk-upstream-sync
upstream_ref: ${target_ref}
upstream_ref_kind: ${target_kind}
upstream_commit: ${target_sha}
sync_commit: ${sync_commit}
base_branch: ${land_ref}
-->

Automated upstream protocol sync.

## Description

${fix_description}

It does not merge itself, tag, or bypass branch protection.

## Sync Metadata

- Upstream ref: \`${target_ref}\`
- Upstream ref kind: \`${target_kind}\`
- Upstream commit: \`${target_sha}\`
- Sync commit: \`${sync_commit}\`
- Base branch: \`${land_ref}\`

This PR was generated by the upstream protocol sync workflow. It stops at the protected PR boundary and does not tag or bypass branch protection. Merge should happen only after branch protection and the required \`PR verification\` check accept this head commit.
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

require_validated_commit
if [[ "$(git rev-parse HEAD)" != "${validated_commit}" ]]; then
  echo "validated commit ${validated_commit} does not match HEAD $(git rev-parse HEAD)" >&2
  exit 1
fi
fetch_landing_ref
landing_sha="$(git rev-parse "${remote}/${land_ref}")"
validated_parent="$(git rev-parse "${validated_commit}^")"
if [[ "${landing_sha}" != "${validated_parent}" ]]; then
  echo "landing ref ${land_ref} moved to ${landing_sha}; validated parent is ${validated_parent}. Rerun protocol sync against current ${land_ref}." >&2
  exit 1
fi
confirm_target_still_points_at_sha
confirm_clean_tree

existing_pr="$(find_existing_target_pr)"
if [[ -n "${existing_pr}" ]]; then
  IFS=$'\t' read -r pr_number pr_url <<< "${existing_pr}"
  write_output "pr_number" "${pr_number}"
  write_output "pr_url" "${pr_url}"
  printf '%s\n' "${pr_url}"
  exit 0
fi

sync_branch="$(sync_branch_name)"
sync_commit="$(git rev-parse HEAD)"
push_sync_branch "${sync_branch}"

write_output "sync_branch" "${sync_branch}"
write_output "sync_commit" "${sync_commit}"

create_or_update_pr "${sync_branch}" "${sync_commit}"
