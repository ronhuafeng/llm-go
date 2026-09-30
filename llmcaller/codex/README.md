# Codex adapter

Use an installed Codex runtime behind the provider-neutral `llmkit` caller API.
The adapter connects `llmkit` to `codexsdk`; it does not install Codex or introduce
another model client.

```sh
go get github.com/ronhuafeng/llm-go@latest
```

## Public behavior

`Call` implements `llmadapter.Caller`. `CallDetailed` and `CallStream` retain
Codex-specific lifecycle details when the neutral API is insufficient. Use the
direct SDK for lifecycle operations that are not part of the neutral call.

The application must supply `Options.Defaults.AdmitTurn`. The adapter presents
the exact thread-start observation and pending turn request before continuation.
It does not define approval, sandbox, permission, disclosure, or side-effect
policy and must not turn a test fixture's settings into application defaults.

Neutral facts come only from available evidence. Requested model/provider
configuration, adapter identity, or a successful live run cannot establish an
attempt-wide serving identity. Exact Codex observations remain in backend
details. Absent output and observed empty output remain distinct.

## Structured output

`StrictOutputSchemaFromJSON` converts a caller schema only when Codex can
represent the same accepted JSON values. Unsupported transformations fail before
runner invocation with `*SchemaPolicyError`. Formatting may change; schema
meaning must not. Decoding into a Go struct alone is not proof of every schema
constraint.

The accepted live structured scenario exercises:

```text
llmadapter -> llmcaller/codex -> codexsdk -> installed Codex -> Mini -> real model
```

It asks a small arithmetic question under an integer `number` output schema,
validates the result, and checks the expected integer. This covers ordinary
Start through the composed path; it is not duplicated solely as a direct-Start
live test. The separate direct-SDK story covers Resume/Fork continuation.

The live scenarios are the runtime guarantee's source of truth, not a catalogue
of every generated method. See [`Live Codex integration`](../../docs/live-codex.md)
for the accepted design and the outstanding implementation transition.

## Examples and checks

[`example_test.go`](example_test.go) composes the public packages with a fake
runner and needs no credentials. Owner-local deterministic checks are:

```sh
go vet ./llmcaller/codex/...
go test -race ./llmcaller/codex/...
```

The real suite supplements these tests; it does not replace schema-conversion,
validation, observation, or error-mechanics tests. Removing or materially
weakening a live story requires an explicit retirement note.

See [`Verification`](../../docs/verify.md), [`Support`](../../SUPPORT.md), and
[`Changelog`](../../CHANGELOG.md).
