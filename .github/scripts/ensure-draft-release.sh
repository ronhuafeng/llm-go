#!/usr/bin/env bash
set -euo pipefail

: "${REPOCTL:?REPOCTL is required}"
: "${TAG:?TAG is required}"
: "${COMMIT:?COMMIT is required}"
: "${NOTES:?NOTES is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

readonly visibility_attempts="${DRAFT_VISIBILITY_ATTEMPTS:-6}"
readonly visibility_delay_seconds="${DRAFT_VISIBILITY_DELAY_SECONDS:-2}"
case "$visibility_attempts" in
  "" | *[!0-9]* | 0)
    echo "Draft visibility attempts must be a positive integer" >&2
    exit 2
    ;;
esac
case "$visibility_delay_seconds" in
  "" | *[!0-9]*)
    echo "Draft visibility delay seconds must be a non-negative integer" >&2
    exit 2
    ;;
esac

readonly releases_json="$RUNNER_TEMP/releases.json"

list_releases() {
  gh api --paginate --slurp "repos/$GITHUB_REPOSITORY/releases?per_page=100" > "$releases_json"
}

select_draft_release() {
  "$REPOCTL" select-draft-release -input "$releases_json" -tag "$TAG" -target "$COMMIT"
}

list_releases
lookup_exit=0
select_draft_release || lookup_exit=$?

case "$lookup_exit" in
  0)
    exit 0
    ;;
  3)
    gh release create "$TAG" --repo "$GITHUB_REPOSITORY" --verify-tag --target "$COMMIT" --draft --title "$TAG (verification pending)" --notes-file "$NOTES"
    ;;
  *)
    exit "$lookup_exit"
    ;;
esac

attempt=1
while [ "$attempt" -le "$visibility_attempts" ]; do
  list_releases
  lookup_exit=0
  select_draft_release || lookup_exit=$?

  case "$lookup_exit" in
    0)
      exit 0
      ;;
    3)
      if [ "$attempt" -eq "$visibility_attempts" ]; then
        echo "Draft Release remained absent from the authenticated Releases list after $visibility_attempts attempts" >&2
        exit "$lookup_exit"
      fi
      sleep "$visibility_delay_seconds"
      ;;
    *)
      exit "$lookup_exit"
      ;;
  esac
  attempt=$((attempt + 1))
done

echo "Draft visibility retry ended without a terminal decision" >&2
exit 1
