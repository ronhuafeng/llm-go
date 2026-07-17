# Integrating `llm-go`

This repository-level guide helps a coding agent choose the semantic owner for
an integration and shows the three-module adapter path. The linked module
documentation is authoritative for current APIs, behavior, policy, testing,
and compatibility.

## Choose the owning module

| Goal | Start with | Authoritative guide |
| --- | --- | --- |
| Generate JSON Schema, decode typed JSON, validate output, or retry with validation feedback for any provider | `llmkit` | [`llmkit` README](../llmkit/README.md) |
| Control exact Codex threads, turns, streaming, notifications, or generated protocol operations | `codexsdk` | [`codexsdk` README](../codexsdk/README.md) |
| Make structured Codex calls while retaining the complete SDK result | `llmcaller/codex` with `llmkit` and `codexsdk` | [adapter README](../llmcaller/codex/README.md) |
| Verify or release this repository | `internal/tools` | [repository verification](verification.md) and [release operation](releasing.md) |

Applications must not import `internal/tools`. Do not add a repository-root
facade, a shared runtime module, or copied types or policy between the three
public modules.

## Cross-module quickstart

Use the adapter path when an application wants a typed value from a local Codex
app-server. This example is navigation across the three public modules; follow
the adapter's [exact defaults](../llmcaller/codex/README.md#exact-defaults),
[result paths](../llmcaller/codex/README.md#result-paths), and
[schema policy](../llmcaller/codex/README.md#schema-policy) for the owning
contracts.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	codexcaller "github.com/ronhuafeng/llm-go/llmcaller/codex"
	"github.com/ronhuafeng/llm-go/llmkit/llmadapter"
)

type Answer struct {
	Summary string `json:"summary"`
}

func main() {
	ctx := context.Background()
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	client, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     workspace,
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	options := codexcaller.ReadOnlyEphemeralOptions(client.ThreadRunner())
	options.Defaults.Thread.Model = protocolv2.Value("gpt-5")
	options.Defaults.Thread.CWD = protocolv2.Value(workspace)
	caller, err := codexcaller.New(options)
	if err != nil {
		log.Fatal(err)
	}

	answer, err := llmadapter.Value[Answer](ctx, caller, "Summarize this repository.")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer.Summary)
}
```

## Continue with the owner

- For provider-neutral schemas, detailed results, validation retries, and
  settling, use the [`llmkit` package guide](../llmkit/README.md#packages) and
  its [testing guidance](../llmkit/README.md#testing).
- For exact lifecycle, streaming, generated protocol access, and app-server
  maintenance, use the [`codexsdk` quickstarts](../codexsdk/README.md#quick-start-typed-client)
  and [compatibility policy](../codexsdk/README.md#compatibility-policy).
- For Codex safety defaults, exact-result retention, schema acceptance, and
  adapter verification, use the [adapter README](../llmcaller/codex/README.md).
- For ownership and dependency direction, use the repository
  [context map](../CONTEXT-MAP.md).

After integration, run the consumer project's normal `go test ./...` and
`go vet ./...` checks. Module-specific release and compatibility obligations
remain in the owning module documentation.
