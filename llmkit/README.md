# llmkit

Provider-neutral typed structured output with stage-owned evidence and no
provider SDK. Destination: [NORTHSTAR.md](../NORTHSTAR.md). Language:
[CONTEXT.md](CONTEXT.md).

## Packages

| Package | Purpose |
| --- | --- |
| `llmschema` | Compile one typed structural contract, project its JSON Schema, validate responses, and decode Go values. |
| `llmadapter` | Execute one provider-neutral typed inference call while preserving provider-neutral evidence and typed backend details. |
| `llmstep` | Deterministically judge typed propositions and run bounded repair attempts. |

Requires Go 1.23 or newer. OS support and testing tiers are in
[SUPPORT.md](../SUPPORT.md).

```sh
go get github.com/ronhuafeng/llm-go/llmkit@latest
```

## Executable examples

Consumer examples are Go tests, not duplicated README programs. They compile
and run as part of the ordinary module test suite:

- [`llmschema/example_test.go`](llmschema/example_test.go) — compile a
  `Contract[T]` once and decode with its structural validator.
- [`llmadapter/example_test.go`](llmadapter/example_test.go) — inference-only
  `Caller`, presence-aware evidence, backend/provider identity separation, and
  isolated backend details.
- [`llmstep/example_test.go`](llmstep/example_test.go) — proposition →
  deterministic judgment → application-projected repair → accepted result.

The repository-level
[`example_authority_to_effect_test.go`](../internal/tools/integration/example_authority_to_effect_test.go)
continues from that accepted output through application-owned authority and
effect-execution evidence. It is a workspace integration proof, not part of
this module's `GOWORK=off` suite.

Run them with:

```sh
GOWORK=off go test ./...
```

## Core contracts

`llmadapter.Caller` is an inference capability. A request contains prompt text
and an output schema; neither grants mutation authority. Model output is a
proposition, not accepted fact or authority.

`llmadapter.Value` is the default typed-inference path. It compiles one
`llmschema.Contract[T]` for request schema and decode. `ValueWithContract` is
the explicit reuse path when the caller already owns a compiled contract. Both
return the same evidence-bearing `ValueResult[T]`.

Neutral execution facts use presence-aware observations: unknown stays unknown,
and an observed zero or empty string remains distinct from absence.
`ExecutionEvidence.BackendName` identifies the runtime/adapter when known;
`ProviderName` is a separate presence-aware model-provider observation and must
not be inferred from the backend or model identifier. Backend adapters own
isolated typed `BackendDetails`; generic decoded values use ordinary Go value
semantics.

`llmstep.Run` owns bounded inference adjudication. `Validate` is required before
execution starts. Validator `Judgment` and model-facing `Repair` are distinct
values. Raw findings never automatically reach a later model attempt: an
application-owned `Step.Sanitizer` projection is required when a rejected
attempt will retry. Exhaustion returns `ErrExhausted` while retaining the latest
proposition and attempt evidence.

Detailed semantics belong in package documentation and [CONTEXT.md](CONTEXT.md),
not in duplicate helper layers.

Changelog: [CHANGELOG.md](CHANGELOG.md). License: [LICENSE](LICENSE). Notices:
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
