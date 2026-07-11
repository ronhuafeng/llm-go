#!/usr/bin/env bash
set -euo pipefail

mode="${1:?usage: verify-clean-consumer.sh <local|version> <source> [directory]}"
source="${2:?usage: verify-clean-consumer.sh <local|version> <source> [directory]}"
consumer_dir="${3:-$(mktemp -d)}"
module=github.com/ronhuafeng/llmkit-go

rm -rf "$consumer_dir"
mkdir -p "$consumer_dir"
cd "$consumer_dir"

go mod init example.com/llmkit-clean-consumer
case "$mode" in
  local)
    go mod edit -replace "$module=$source"
    go get "$module"
    ;;
  version)
    go get "$module@$source"
    ;;
  *)
    echo "unsupported mode: $mode" >&2
    exit 2
    ;;
esac

cat > main.go <<'EOF'
package main

import (
	"context"
	"fmt"

	"github.com/ronhuafeng/llmkit-go/llmadapter"
	"github.com/ronhuafeng/llmkit-go/llmstep"
	"github.com/ronhuafeng/llmkit-go/settle"
)

type caller struct{}

func (caller) Call(context.Context, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{FinalResponse: `{"value":"ok"}`}, nil
}

type output struct {
	Value string `json:"value"`
}

type operation struct{}

func (operation) Run(context.Context, string) (string, error) { return "settled", nil }
func (operation) Validate(context.Context, string, string) (bool, error) { return true, nil }

func main() {
	ctx := context.Background()
	value, err := llmadapter.ValueDetailed[output](ctx, caller{}, "return ok")
	if err != nil || value.Value.Value != "ok" {
		panic(fmt.Sprintf("ValueDetailed: value=%+v err=%v", value, err))
	}

	settled, err := settle.RunDetailed(ctx, operation{}, "input", 1)
	if err != nil || !settled.HasOutput || len(settled.Attempts) != 1 {
		panic(fmt.Sprintf("settle.RunDetailed: result=%+v err=%v", settled, err))
	}

	stepped, err := llmstep.RunDetailed(ctx, llmstep.Step[string, output]{
		Caller: caller{},
		Render: func(context.Context, string, []llmstep.Feedback) (string, error) {
			return "return ok", nil
		},
		MaxIter: 1,
	}, "input")
	if err != nil || !stepped.HasOutput || len(stepped.Attempts) != 1 {
		panic(fmt.Sprintf("llmstep.RunDetailed: result=%+v err=%v", stepped, err))
	}
}
EOF

go mod tidy
go test ./...
go run .

while read -r dependency _; do
  case "$dependency" in
    example.com/llmkit-clean-consumer | \
      github.com/ronhuafeng/llmkit-go | \
      github.com/dlclark/regexp2 | \
      github.com/google/go-cmp | \
      github.com/google/jsonschema-go | \
      github.com/santhosh-tekuri/jsonschema/v6 | \
      golang.org/x/mod | \
      golang.org/x/sys | \
      golang.org/x/tools | \
      golang.org/x/text)
      ;;
    *)
      echo "unreviewed dependency in clean consumer module graph: $dependency" >&2
      exit 1
      ;;
  esac
done < <(go list -m all)

if [[ "$mode" == version ]]; then
  go list -m -json "$module" > module.json
  resolved="$(go list -m -f '{{.Version}}' "$module")"
  if [[ "$resolved" != "$source" ]]; then
    echo "resolved $module@$resolved, expected $source" >&2
    exit 1
  fi
fi
