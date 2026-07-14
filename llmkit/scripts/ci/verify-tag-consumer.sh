#!/usr/bin/env bash
set -euo pipefail

tag="${1:?usage: verify-tag-consumer.sh <tag> <commit> <workdir>}"
commit="${2:?usage: verify-tag-consumer.sh <tag> <commit> <workdir>}"
workdir="${3:?usage: verify-tag-consumer.sh <tag> <commit> <workdir>}"
repository="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ "$workdir" == "/" ]]; then
  echo "refusing to use filesystem root as tag consumer workdir" >&2
  exit 2
fi
rm -rf "$workdir"
mkdir -p "$workdir"

# Isolate the helper build as well as the consumer it launches. In particular,
# setup-go's cache and the runner's GOPATH cannot satisfy this verification.
export GO111MODULE=on
export GOCACHE="$workdir/buildcache"
export GOENV=off
export GOFLAGS=
export GOMODCACHE="$workdir/modcache"
export GONOPROXY=
export GONOSUMDB=
export GOPATH="$workdir/gopath"
export GOPRIVATE=
export GOPROXY=https://proxy.golang.org
export GOSUMDB=sum.golang.org
export GOTOOLCHAIN=local
export GOVCS='*:off'
export GOWORK=off

go run ./internal/cmd/tagconsumer \
  -tag "$tag" \
  -commit "$commit" \
  -repository "$repository" \
  -workdir "$workdir" \
  -evidence "$workdir/tag-evidence.json" \
  -timeout 10m \
  -retry-interval 15s \
  -command-timeout 10m
