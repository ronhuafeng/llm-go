# llmkit

Provider-neutral typed model calls, validation, and bounded retries.

| Package | Purpose |
| --- | --- |
| `llmschema` | Compile a Go output type to JSON Schema, validate JSON, and decode it. |
| `llmadapter` | Provider-neutral typed call contracts and response decoding. |
| `llmstep` | Run bounded calls with deterministic caller validation and optional retry feedback. |

Requires Go 1.23 or newer.

```sh
go get github.com/ronhuafeng/llm-go/llmkit@latest
```

## Main API

`llmschema.Contract[T]` compiles one typed output contract for reuse.
`llmadapter.Value` performs a typed call through a `Caller` and decodes the
response. Backend-specific adapters own transport and typed backend details.

`llmstep.Run` adds bounded retries around typed calls. Caller validation decides
whether an output is accepted; rejected attempts do not populate the successful
`Result.Output`. Applications decide what validation feedback, if any, is sent
to a later model attempt.

The toolkit does not own provider transport, application authorization, or
external side effects.

## Examples and verification

Compile-checked examples live with the packages:

- [`llmschema/example_test.go`](llmschema/example_test.go)
- [`llmadapter/example_test.go`](llmadapter/example_test.go)
- [`llmstep/example_test.go`](llmstep/example_test.go)

```sh
GOWORK=off go test ./...
```

See [`../docs/verify.md`](../docs/verify.md) for repository verification and
[`CHANGELOG.md`](CHANGELOG.md) for releases.
