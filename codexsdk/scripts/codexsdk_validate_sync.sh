#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/codexsdk_validate_sync.sh [options]

Options:
  --candidate <path>   Candidate schema directory that must match the baseline.
  --target-sha <sha>   Expected baseline source_commit.

Runs the validation gate required after an upstream protocol baseline sync.
EOF
}

candidate=""
target_sha=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --candidate)
      candidate="$2"
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

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
baseline="codexsdk/internal/protocolschema/appserver/v2"

cd "${repo_root}"

unformatted_go="$(git ls-files -z -- '*.go' ':!:vendor/**' | xargs -0 gofmt -l)"
if [[ -n "${unformatted_go}" ]]; then
  echo "tracked Go files are not gofmt-formatted:" >&2
  printf '%s\n' "${unformatted_go}" >&2
  exit 1
fi
GOWORK=off go vet ./...
GOWORK=off go test ./...

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
GOWORK=off go run ./codexsdk/internal/cmd/protocolv2gen -out "${tmp}"
diff -u codexsdk/protocolv2/method_registry.gen.go "${tmp}/method_registry.gen.go"
diff -u codexsdk/protocolv2/protocol_types.gen.go "${tmp}/protocol_types.gen.go"
python3 scripts/codexsdk_generate_sdk_surface.py --out "${tmp}/sdk_surface.gen.go"
gofmt -w "${tmp}/sdk_surface.gen.go"
diff -u codexsdk/sdk_surface.gen.go "${tmp}/sdk_surface.gen.go"

git diff --check

sync_state_args=(
  --baseline "${baseline}"
)
if [[ -n "${candidate}" ]]; then
  sync_state_args+=(--candidate "${candidate}")
fi
if [[ -n "${target_sha}" ]]; then
  sync_state_args+=(--target-sha "${target_sha}")
fi
python3 scripts/codexsdk_sync_state.py "${sync_state_args[@]}"

path_scan_pattern='/Users/|/home/|[.]cache/codexsdk-upstream|[.]cache/openai-codex'
if command -v rg >/dev/null 2>&1; then
  path_scan=(rg -n "${path_scan_pattern}" "${baseline}")
else
  path_scan=(grep -RInE "${path_scan_pattern}" "${baseline}")
fi
if "${path_scan[@]}"; then
  echo "checked-in protocol baseline contains local or cache paths" >&2
  exit 1
fi
