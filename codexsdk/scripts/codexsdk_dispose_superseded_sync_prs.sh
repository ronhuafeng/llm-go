#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/codexsdk_dispose_superseded_sync_prs.sh \
    --current-pr <number> \
    --default-branch <branch> \
    --run-url <url> \
    --upstream-ref <ref> \
    --upstream-sha <sha>

Close stale upstream-protocol sync PRs after a replacement is published, or
when the default branch already records the selected upstream target. Failed
branch deletion is reported as a warning after its PR has closed.
EOF
}

current_pr=""
default_branch=""
run_url=""
upstream_ref=""
upstream_sha=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --current-pr)
      current_pr="$2"
      shift 2
      ;;
    --default-branch)
      default_branch="$2"
      shift 2
      ;;
    --run-url)
      run_url="$2"
      shift 2
      ;;
    --upstream-ref)
      upstream_ref="$2"
      shift 2
      ;;
    --upstream-sha)
      upstream_sha="$2"
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

if [[ -z "${default_branch}" || -z "${run_url}" || -z "${upstream_ref}" || -z "${upstream_sha}" ]]; then
  usage >&2
  exit 2
fi

gh auth setup-git

stale_prs="$(
  gh pr list \
    --state open \
    --base "${default_branch}" \
    --limit 100 \
    --json number,title,headRefName,body \
    --jq '
      .[]
      | select(
          (.headRefName | startswith("codex/sync-upstream-"))
          or
          (.headRefName | startswith("codex/sync-upstream/"))
        )
      | select(
          .title
          | startswith("Sync Codex protocol baseline to ")
        )
      | select(
          .body != null
          and
          (.body | contains("<!-- codexsdk-upstream-sync"))
        )
      | [.number, .headRefName]
      | @tsv
    '
)"

disposed=0

while IFS=$'\t' read -r pr_number branch; do
  [[ -n "${pr_number}" ]] || continue

  if [[ -n "${current_pr}" && "${pr_number}" == "${current_pr}" ]]; then
    continue
  fi

  if [[ -n "${current_pr}" ]]; then
    comment="Superseded by fresh synchronization PR #${current_pr}. Replacement was generated and validated by ${run_url}."
  else
    comment="Closed by ${run_url} because ${default_branch} already records upstream target ${upstream_ref} (${upstream_sha})."
  fi

  gh pr close "${pr_number}" --comment "${comment}"

  if ! git push origin --delete "${branch}"; then
    echo "::warning title=Superseded branch not deleted::Unable to delete ${branch}; the PR was closed successfully."
  fi

  disposed=$((disposed + 1))
done <<< "${stale_prs}"

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo
    echo "## Superseded sync cleanup"
    echo
    echo "- Disposed PR count: \`${disposed}\`"
    if [[ -n "${current_pr}" ]]; then
      echo "- Preserved replacement PR: \`#${current_pr}\`"
    else
      echo "- Replacement PR: not required; baseline already matches the selected target"
    fi
  } >> "${GITHUB_STEP_SUMMARY}"
fi
