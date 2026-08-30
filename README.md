# llm-go

Typed Go modules for structured LLM calls and exact Codex app-server control.

| Module | Use it to | Import path |
| --- | --- | --- |
| [`llmkit`](llmkit) | Generate JSON Schema from Go types, decode and validate structured output, and run bounded retries without a provider SDK. | `github.com/ronhuafeng/llm-go/llmkit` |
| [`codexsdk`](codexsdk) | Control a local Codex app-server through generated protocol types and typed thread/turn APIs. | `github.com/ronhuafeng/llm-go/codexsdk` |
| [`llmcaller/codex`](llmcaller/codex) | Use Codex as an `llmkit` caller while keeping the complete SDK result. | `github.com/ronhuafeng/llm-go/llmcaller/codex` |

Start with the README for the module matching your use case.
Upgrade from archived module paths: [`llmkit/UPGRADE.md`](llmkit/UPGRADE.md),
[`codexsdk/UPGRADE.md`](codexsdk/UPGRADE.md),
[`llmcaller/codex/UPGRADE.md`](llmcaller/codex/UPGRADE.md).

Destinations: [`NORTHSTAR.md`](NORTHSTAR.md).
Maintainers: [`AGENTS.md`](AGENTS.md), [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Adapter path

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

Follow the adapter README for exact defaults, result paths, and schema policy.
Applications must not import `internal/tools`.

## Security

Report vulnerabilities through this repository's private intake. See
[SECURITY.md](SECURITY.md).
