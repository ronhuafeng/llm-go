#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

unformatted="$(find llmkit codexsdk llmcaller internal/tools -name '*.go' -type f -print0 | xargs -0 gofmt -l)"
if [[ -n "$unformatted" ]]; then
  echo "gofmt required:" >&2
  echo "$unformatted" >&2
  exit 1
fi

for module in llmkit codexsdk llmcaller/codex internal/tools; do
  echo "==> ${module}: module metadata"
  (cd "$module" && GOWORK=off GOTOOLCHAIN=local go mod verify)
  (cd "$module" && GOWORK=off GOTOOLCHAIN=local go mod tidy -diff)
  echo "==> ${module}: vet"
  (cd "$module" && GOWORK=off GOTOOLCHAIN=local go vet ./...)
  echo "==> ${module}: tests + race"
  (cd "$module" && GOWORK=off GOTOOLCHAIN=local go test -race ./...)
done

echo '==> codexsdk generated protocol proof'
(cd codexsdk && ./scripts/codexsdk_validate_sync.sh)

echo '==> current-source three-layer canary'
go test ./internal/tools/integration -run '^TestThreeLayerCanaryFast$' -count=1 -v

echo 'verification passed'
