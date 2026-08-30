# codexsdk

Exact control of one local Codex app-server. Destination:
[NORTHSTAR.md](../NORTHSTAR.md). Language: [CONTEXT.md](CONTEXT.md).
Upgrade notes: [UPGRADE.md](UPGRADE.md).

This project is unofficial and experimental. It is not an OpenAI product.
Use it to talk to a locally launched Codex app-server over stdio.

## Packages

- `codexsdk`: stdio client, generated typed facades, exact `ThreadRunner`, exact
  notification streaming, and generated server-request handling.
- `protocolv2`: generated app-server v2 params, responses,
  notifications, enums, unions, JSON helpers, and method registry.
- `internal/protocolgen`: generator internals for the checked-in schema
  baseline.
- `internal/protocolschema/appserver/v2`: reviewed schema baseline,
  classified manifest, coverage matrix, drift report, and provenance metadata.

Inbound app-server JSON-RPC frames are limited to 16 MiB, including the newline
delimiter. Oversized or unterminated frames fail the Root Client with sanitized
byte-count and hash diagnostics; outbound messages are not subject to this
internal transport limit.

## Installation

Install the verified replacement release with:

```sh
go get github.com/ronhuafeng/llm-go/codexsdk@v0.7.0
```

The module targets Go 1.23 or newer.

To run against a real app-server, install Codex CLI separately and make sure
`codex` is on `PATH`:

```sh
codex --version
```

## Quick Start: Typed Client

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

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

	resp, err := client.Models().List(ctx, protocolv2.ModelListParams{})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("models: %d", len(resp.Data))
}
```

## Quick Start: Exact ThreadRunner

`ThreadRunner` transparently composes exact generated `thread/start` and
`turn/start` params. The result retains the exact start response, terminal turn,
usage, and every attributable generated notification.

A successfully decoded lifecycle response remains observable as partial
evidence even when a required thread or turn identity is missing. In that case,
the simple operation returns the decoded facts with `ErrMissingThreadID` or
`ErrMissingTurnID`; the streaming operation returns a non-nil terminal stream
whose `Wait`, `Result`, and `Err` expose the same facts and cause. Identity
failure prevents later lifecycle requests or live run registration without
closing the Client.

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func main() {
	ctx := context.Background()
	workspace, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	model := os.Getenv("CODEXSDK_EXAMPLE_MODEL")
	if model == "" {
		log.Fatal("set CODEXSDK_EXAMPLE_MODEL")
	}

	root, err := codexsdk.New(codexsdk.ClientOptions{
		CWD:     workspace,
		Command: []string{"codex", "app-server", "--listen", "stdio://"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer root.Close()

	result, err := root.ThreadRunner().Start(ctx, codexsdk.StartThreadRunRequest{
		Thread: protocolv2.ThreadStartParams{
			Ephemeral: protocolv2.Value(true),
			Model:     protocolv2.Value(model),
		},
		Turn: protocolv2.TurnStartParams{
			Input: []protocolv2.UserInput{
				protocolv2.NewUserInputText(protocolv2.UserInputText{
					Text: "Reply with a short confirmation.",
				}),
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(result.Run.FinalResponse)
}
```

`StartStream` and `ResumeStream` expose every exact
`protocolv2.ServerNotification`; `Result` remains available on failures and
contains the latest immutable partial snapshot. More compile-checked examples
live in `examples_test.go`.

Call `Stream.Wait` when multiple consumers need to observe the same run without
coordinating ownership of `Next`. Any number of waiters can block independently
and each receives an immutable result snapshot plus the run's stable terminal
error. A waiter's context bounds only that call: cancellation returns the latest
partial snapshot with `ctx.Err()` without canceling the run or changing
`Stream.Err`. Use `Stream.Close` for explicit shared run cancellation. `Next`
uses a cursor over the same immutable ordered history retained by `Result`, so
`Wait` does not need to consume notifications and cannot cause per-run
backpressure. `Next` context cancellation retains its shared-run cancellation
semantics. The separately configurable global notification-handler queue
remains bounded.

For one thread, only one Exact Run may be waiting for `turn/start` to return its
turn identity. An overlapping start fails before sending another `turn/start`.
Once the first turn identity is attached, later turns on the same thread may
start without waiting for the earlier live turn to finish.

Configure `ServerRequestHandler` when the application can provide generated
response data. With no handler, the
SDK immediately returns a generated fail-closed response for requests that
have a safe denial or empty-answer form. Requests requiring application data,
including authentication refresh, dynamic tool output, and attestation, return
a JSON-RPC error and fail the exact run with `ErrExactServerRequest`; partial
notifications and run evidence remain available in the result.

Callback admission is atomic with client shutdown. Once `Close` or failure
shutdown closes admission, no new server-request or notification handler is
started. Normal close cancels exact server-request handler contexts and joins
every callback accepted before that boundary before transport teardown;
failure shutdown cancels accepted callbacks immediately while preserving the
first failure cause and partial run evidence. Handlers must return when their
context is canceled and must not call `Close` reentrantly.

## Testing

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

Protocol sync uses
[`codexsdk-sync-upstream`](../.agents/skills/codexsdk-sync-upstream/SKILL.md).
See repository [CONTRIBUTING.md](../CONTRIBUTING.md) and
[SECURITY.md](../SECURITY.md). Changelog: [CHANGELOG.md](CHANGELOG.md).
