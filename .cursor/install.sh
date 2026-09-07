#!/usr/bin/env bash
# Refresh independently publishable Go module caches for Cloud Agents.
# Matches CI: GOWORK=off. Does not write go.work.sum.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

modules=(
	llmkit
	codexsdk
	llmcaller/codex
	internal/tools
)

for dir in "${modules[@]}"; do
	echo "downloading modules in ${dir}"
	(
		cd "$dir"
		GOWORK=off go mod download
	)
done

echo "building repository tool"
(
	cd internal/tools
	GOWORK=off go build -o /tmp/repoctl ./cmd/repoctl
)

echo "install complete: $(go version)"
