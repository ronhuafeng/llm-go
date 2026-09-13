# Codex adapter

Use `codexsdk` behind the provider-neutral `llmkit` caller API.

Requires Go 1.23 or newer.

```sh
go get github.com/ronhuafeng/llm-go/llmcaller/codex@latest
```

## Main API

`Call` implements `llmadapter.Caller`. `CallDetailed` and `CallStream` keep
Codex-specific lifecycle details available when the neutral API is not enough.

The application must provide `Options.Defaults.AdmitTurn`. The callback receives
the decoded thread-start/resume result and the pending `turn/start` request
before model execution continues. The adapter forwards that decision; it does
not define a safe approval/sandbox/permission profile.

Neutral fields are populated only from information Codex actually provides.
Requested settings, adapter identity, or model names are not used to invent
provider/runtime facts. Exact Codex details remain available through adapter
details and the direct SDK APIs.

## Output schema conversion

`StrictOutputSchemaFromJSON` converts a caller schema only when Codex can
represent it without changing the accepted JSON values. Unsupported optional
properties, references, dialects, or other required rewrites fail before runner
invocation with `*SchemaPolicyError`.

Serialization may change JSON formatting; semantic equivalence, not byte
identity, is the contract.

## Example and verification

[`example_test.go`](example_test.go) composes `llmkit`, this adapter, and a fake
`codexsdk` runner without credentials.

For repository current source:

```sh
go test ./...
```

Published dependency closure is checked separately during release. See
[`../../docs/verify.md`](../../docs/verify.md) and
[`../../docs/release.md`](../../docs/release.md).
