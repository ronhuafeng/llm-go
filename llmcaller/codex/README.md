# Codex adapter

Use `codexsdk` behind the provider-neutral `llmkit` caller API.

```sh
go get github.com/ronhuafeng/llm-go/llmcaller/codex@latest
```

## Main API

`Call` implements `llmadapter.Caller`. `CallDetailed` and `CallStream` keep
Codex-specific lifecycle details available when the neutral API is not enough.

The application must provide `Options.Defaults.AdmitTurn`. The adapter forwards
that decision before model execution continues; it does not define approval,
sandbox, permission, disclosure, or side-effect policy.

Neutral fields are populated only from information Codex actually provides.
Requested settings, adapter identity, or model names are not used to invent
provider/runtime facts. Exact Codex details remain available through adapter
details and the direct SDK APIs.

## Output schema conversion

`StrictOutputSchemaFromJSON` converts a caller schema only when Codex can
represent it without changing the accepted JSON values. Unsupported rewrites
fail before runner invocation with `*SchemaPolicyError`.

Serialization may change JSON formatting; semantic equivalence, not byte
identity, is the contract.

## Example

[`example_test.go`](example_test.go) composes `llmkit`, this adapter, and a fake
`codexsdk` runner without credentials.

Supported Go/platform versions: [`../../SUPPORT.md`](../../SUPPORT.md).
Repository verification: [`../../docs/verify.md`](../../docs/verify.md).
Release ordering/closure: [`../../docs/release.md`](../../docs/release.md).
