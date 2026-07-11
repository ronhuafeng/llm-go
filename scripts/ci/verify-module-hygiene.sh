#!/usr/bin/env bash
set -euo pipefail

base_ref="${1:-}"

if find . -name go.work -not -path './.git/*' -print -quit | grep -q .; then
  echo "committed go.work files are not allowed" >&2
  exit 1
fi

if grep -Eq '^[[:space:]]*replace([[:space:]]|\()' go.mod; then
  echo "replace directives are not allowed in go.mod" >&2
  exit 1
fi

GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum

if [[ -n "$base_ref" ]] && git rev-parse --verify "$base_ref" >/dev/null 2>&1; then
  if ! git diff --quiet "$base_ref"...HEAD -- go.mod go.sum &&
    git diff --quiet "$base_ref"...HEAD -- THIRD_PARTY_NOTICES.md; then
    echo "dependency metadata changed without reviewing THIRD_PARTY_NOTICES.md" >&2
    exit 1
  fi
fi

test -s THIRD_PARTY_NOTICES.md
