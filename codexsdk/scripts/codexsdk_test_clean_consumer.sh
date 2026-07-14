#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
consumer="$(mktemp -d)"
trap 'rm -rf "${consumer}"' EXIT

cd "${consumer}"
go mod init example.com/codexsdk-clean-consumer >/dev/null
if [[ -z "${CODEXSDK_CONSUMER_REF:-}" ]]; then
	go mod edit -replace "github.com/ronhuafeng/codexsdk-go=${repo_root}"
fi

cat > client_test.go <<'EOF'
package consumer

import (
	"context"
	"testing"

	"github.com/ronhuafeng/codexsdk-go/codexsdk"
	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

type runStarter interface {
	Start(context.Context, codexsdk.StartThreadRunRequest) (codexsdk.StartedThreadRun, error)
}

type modelLister interface {
	List(context.Context, protocolv2.ModelListParams) (protocolv2.ModelListResponse, error)
}

type fakeModelLister struct{}

func (fakeModelLister) List(context.Context, protocolv2.ModelListParams) (protocolv2.ModelListResponse, error) {
	return protocolv2.ModelListResponse{}, nil
}

var _ modelLister = fakeModelLister{}

func useRoot(root *codexsdk.Client) error {
	var _ runStarter = root.ThreadRunner()
	var _ modelLister = root.Models()
	return root.Close()
}

func TestZeroValueLifecycle(t *testing.T) {
	var root codexsdk.Client
	if err := useRoot(&root); err != nil {
		t.Fatal(err)
	}
}
EOF

if [[ -n "${CODEXSDK_CONSUMER_REF:-}" ]]; then
	GOWORK=off go get "github.com/ronhuafeng/codexsdk-go@${CODEXSDK_CONSUMER_REF}"
fi
GOWORK=off go mod tidy
GOWORK=off go test ./...
