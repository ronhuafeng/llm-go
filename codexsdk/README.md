# codexsdk-go

Go client and generated protocol types for the Codex app-server JSON-RPC
protocol.

> [!IMPORTANT]
> This module path is frozen at `v0.5.1` and receives no further feature or
> security updates. Development moves to
> `github.com/ronhuafeng/llm-go/codexsdk`, beginning with `v0.6.0`. See the
> [migration guide](docs/llm-go-migration.md) for exact import mappings. Treat
> the replacement as available only after that tag resolves through the public
> Go proxy.

This project is unofficial and experimental. It is not an OpenAI product, is
not supported by OpenAI, and may lag or diverge from the Codex CLI/app-server
implementation. Use it when you want a small Go SDK that talks to a locally
launched Codex app-server over stdio.

## Status

- License: MIT for this repository.
- Upstream protocol source: OpenAI Codex, Apache-2.0, generated from the
  app-server schema baseline recorded in
  `codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json`.
- API stability: pre-1.0. Public APIs are intended to be useful and reviewed,
  but breaking changes can happen before v1.0.
- Final legacy release: `v0.5.1`; this repository is frozen pending archival
  after the replacement module is verified.
- Runtime requirement: the SDK launches an external `codex app-server` command.
  Unit tests and CI do not require a local Codex binary.

The pre-v1 API uses a concrete root client, exported concrete opaque generated
facades, consumer-owned narrow interfaces, and manifest-classified stable
versus experimental generated compatibility.
See [Pre-v1 Public API Boundary](docs/public-api-boundary.md).
For the concrete generated-facade migration, see the
[v0.4 migration guide](docs/v0.4-migration.md).
For the unified malformed lifecycle partial-evidence contract, see the
[v0.5 migration guide](docs/v0.5-migration.md).

## Packages

- `codexsdk`: stdio client, generated typed facades, exact `ThreadRunner`, exact
  notification streaming, and generated server-request handling.
- `codexsdk/protocolv2`: generated app-server v2 params, responses,
  notifications, enums, unions, JSON helpers, and method registry.
- `codexsdk/internal/protocolgen`: generator internals for the checked-in schema
  baseline.
- `codexsdk/internal/protocolschema/appserver/v2`: reviewed schema baseline,
  classified manifest, coverage matrix, drift report, and provenance metadata.

## Installation

```sh
go get github.com/ronhuafeng/codexsdk-go@v0.5.1
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

	"github.com/ronhuafeng/codexsdk-go/codexsdk"
	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
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
closing the Client. See the [v0.5 migration guide](docs/v0.5-migration.md).

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/ronhuafeng/codexsdk-go/codexsdk"
	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
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
live in `codexsdk/examples_test.go`.

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

The removed v0.1 lifecycle and copied protocol models have no compatibility
aliases. See [the migration mapping](docs/v0.2-migration.md#removed-v01-mapping)
for exact replacements.

## Real App-Server Smoke Test

The real smoke test is opt-in because it launches Codex, uses a configured
model, and may create or consume account state.

```sh
CODEXSDK_REAL_APP_SERVER_SMOKE=1 \
CODEXSDK_REAL_APP_SERVER_MODEL=gpt-5-mini \
go test ./codexsdk -run TestRealAppServerSmokeStartResumeFork -count=1
```

Optional command override:

```sh
CODEXSDK_REAL_APP_SERVER_COMMAND='codex app-server --listen stdio://' \
CODEXSDK_REAL_APP_SERVER_SMOKE=1 \
CODEXSDK_REAL_APP_SERVER_MODEL=gpt-5-mini \
go test ./codexsdk -run TestRealAppServerSmokeStartResumeFork -count=1
```

Normal CI does not run this test.

## Protocol V2 Schema Strategy

`protocolv2` code is generated from a checked-in Codex app-server v2 schema
baseline, not by shelling out to Codex during normal builds. The baseline is
tracked with:

- `baseline_metadata.json`: upstream tag/ref name, target kind, peeled commit,
  Codex version, generation command, source license, file count, and schema
  bundle checksum.
- `manifest.json`: classified method and exported generated Go surface,
  request/notification direction, response schema mapping, facade target,
  explicit generated/deferred facade status, and mechanically derived stable,
  experimental, or mixed marking. A deferred facade is valid only while a
  generated method constant or protocol type prerequisite is absent.
- `coverage_matrix.json`: reviewed support status for methods, types, and key
  fields.
- `drift_report.json` and `matrix_update_skeleton.json`: last clean comparison
  artifacts and the shape of follow-up review work when upstream changes.

Regenerate Go code from the checked-in baseline:

```sh
go run ./codexsdk/internal/cmd/protocolv2gen
```

Check generated code reproducibility without modifying the tree:

```sh
go run ./codexsdk/internal/cmd/protocolv2gen -stdout method-registry |
  diff -u codexsdk/protocolv2/method_registry.gen.go -
go run ./codexsdk/internal/cmd/protocolv2gen -stdout protocol-types |
  diff -u codexsdk/protocolv2/protocol_types.gen.go -
tmp="$(mktemp -d)/sdk_surface.gen.go"
python3 scripts/codexsdk_generate_sdk_surface.py --out "$tmp"
gofmt -w "$tmp"
diff -u codexsdk/sdk_surface.gen.go "$tmp"
```

## Maintenance

Use the upstream tracking script to generate review artifacts for a Codex
schema update. The script is read-only for the checked-in baseline unless a
maintainer copies reviewed files back into the SDK tree.

Check the target policy before generating drift artifacts. Scheduled automation
tracks stable `rust-vX.Y.Z` tags only when the current baseline is already on
that stable tag track; manual commits and track switches must be explicit.

```sh
python3 scripts/codexsdk_target_policy.py \
  --baseline codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json \
  --target-ref rust-v0.140.0 \
  --target-kind stable_rust_tag \
  --target-sha <peeled-target-commit> \
  --target-explicit true \
  --mode manual \
  --json
```

```sh
scripts/codexsdk_track_upstream.sh \
  --codex-repo /path/to/openai/codex \
  --commit <peeled-target-commit> \
  --source-ref rust-v0.140.0 \
  --source-ref-kind stable_rust_tag \
  --out /tmp/codexsdk-upstream
```

Then review the generated `reports/SUMMARY.md`, schema drift summary, and matrix
update skeleton before updating the baseline, manifest, coverage matrix, and
generated Go code. Keep handwritten SDK changes limited to reviewed public
surface or compatibility fixes. See `docs/release.md` for the release and
schema baseline checklists.

After committing a successful baseline sync, tag the codexsdk-go commit with an
annotated upstream sync tag. These tags intentionally live outside the Go module
release namespace.

```sh
python3 scripts/codexsdk_sync_tag.py --json
python3 scripts/codexsdk_sync_tag.py --create --push origin --json
```

Stable upstream Codex tags use `upstream-codex-rust-vX.Y.Z`. Existing upstream
sync tags are never moved; use `--next-suffix` to create
`upstream-codex-rust-vX.Y.Z-sync.N` for follow-up SDK fixes against the same
upstream tag. Manual upstream commits and refs intentionally do not get fallback
sync tags.

## Compatibility Policy

Before v1.0, minor releases may include breaking changes when the upstream
Codex app-server protocol changes or when the SDK corrects an unsafe public
API. Patch releases should be backwards compatible except for security or data
corruption fixes.

After v1.0, the project should follow SemVer for the public API in `codexsdk`
and `codexsdk/protocolv2`. Generated `protocolv2` additions are usually minor
changes. Removing or changing generated types, method constants, or facade
method signatures is a major change unless the upstream protocol removed the
surface and compatibility cannot be preserved safely.

## Security

Do not put API keys, account tokens, private workspaces, private schema dumps,
or local absolute paths into issues, tests, schema metadata, or generated
artifacts. The SDK starts a local app-server process and forwards requests over
stdio; callers are responsible for choosing an appropriate Codex command,
working directory, approval policy, and server request handler.

See `SECURITY.md` for vulnerability reporting guidance.
