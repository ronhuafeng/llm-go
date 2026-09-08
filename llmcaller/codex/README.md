# Codex adapter

A loss-aware join between provider-neutral inference and exact Codex facts.
Destination: [NORTHSTAR.md](../../NORTHSTAR.md). Language:
[CONTEXT.md](CONTEXT.md).

Requires Go 1.23 or newer.

```sh
go get github.com/ronhuafeng/llm-go/llmcaller/codex@latest
```

## Executable example

[`example_test.go`](example_test.go) is the canonical three-layer consumer
example. It composes `llmkit`, this adapter, and `codexsdk` with a deterministic
fake `ThreadRunner`, so the example is compiled and executed by ordinary tests
without credentials or provider availability.

```sh
GOWORK=off go test ./...
```

The optional repository-level live smoke runs the same public composition
against a real local Codex app-server; see
[`../../docs/verify.md`](../../docs/verify.md).

## Effect-safe neutral caller

A provider-neutral `llmadapter.Caller` can be constructed only through the
named read-only, never-approve, ephemeral profile. The adapter applies that
profile at thread and turn scope and admits the decoded effective thread-start
facts before `turn/start`. Unknown or mismatched required facts reject
continuation before the model-directed turn can execute.

That named profile is **effect-safe, not disclosure-safe**. **Read-only is
not confidential.** An allowed read can still expose workspace or prompt
input to model and provider processing. Preventing sensitive disclosure is
application-owned unless separately proven. CWD, workspace roots, and input
selection remain part of the confidentiality boundary. **Ephemeral is not a
provider-retention guarantee.** Provider data handling and retention are not
established by this adapter profile.

Effectful or provider-specific Codex operations use `codexsdk` Exact Run /
`ThreadRunner` surfaces directly; they are not neutral `llmadapter.Caller`
operations.

`Options.Defaults` is the exact `codexsdk.StartThreadRunRequest`. The adapter
owns `Turn.ThreadID`, `Turn.Input`, `Turn.OutputSchema`, `AdmitTurn`, and the
named profile fields. Other generated defaults such as model, CWD, reasoning
effort, service tier, and workspace roots remain caller-controlled.

## Evidence paths

- `Call` implements `llmadapter.Caller` and publishes final text plus sound
  provider-neutral observations.
- `CallDetailed` returns the exact `codexsdk.StartedThreadRun`, including
  partial evidence on failure.
- `CallStream` preserves the exact streaming lifecycle and applies the same
  pre-turn admission.

`Call` publishes an isolated `codexcaller.Details` snapshot when it can do so
safely. Failure to isolate exact details omits `ProviderDetails` and returns the
isolation error, but independently isolated neutral model/usage observations
remain available. Requested/default model values never fill an unknown
observation, and observed zero token counts remain distinct from unreported
counts.

## Schema Policy

`StrictOutputSchemaFromJSON` converts neutral JSON Schema to the generated
Codex `OutputSchema`. It recursively visits supported subschema positions,
resolves local references, and preserves unknown keywords by JSON value
semantics.

### Normative schema-equivalence contract

The words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** below are normative.

Provider-neutral Go type projection and response decoding remain owned by
`llmkit`. This adapter MUST apply only Codex-specific schema policy to the
exact JSON Schema it receives; it MUST NOT infer a broader schema from Go type
shape or add transformations not described here.

At the JSON Schema instance-language layer, the adapter MUST preserve every
constraint except the following intentional narrowing: each object property
that is absent from `required` is promoted to required, but only after the
property's complete schema is proven to accept the JSON instance `null` under
the selected draft. Thus the transformed language no longer admits omission of
that property, while its admitted explicit JSON values and all other supported
constraints remain unchanged. This is not universal JSON Schema language
equivalence.

At the ordinary Go decoded-value layer, that narrowing is intended only for
fields where omission and explicit `null` decode to the same value using normal
`encoding/json` pointer, slice, map, or value behavior. The adapter does not
decode results and MUST NOT claim this equivalence for custom
`UnmarshalJSON` implementations, `json.RawMessage`, or domain types that attach
meaning to presence. It makes no guarantee of arbitrary application semantic
equivalence.

Unknown annotation and assertion keyword values retained by the generated SDK
parser/serializer MUST survive by decoded JSON value semantics. The adapter
does not promise that an unsupported assertion keyword is enforced. Supported
local `$ref` values are URI fragments containing JSON Pointer references; their
target meaning and applicable sibling constraints MUST be preserved according
to the selected draft. External resources, `$dynamicRef`, recursive or cyclic
graphs, unresolvable references, unsupported draft identifiers, and any schema
whose null admission cannot be proven MUST fail closed. `$vocabulary`
declarations also fail closed. A `$dynamicAnchor` value without `$dynamicRef`
is retained, but dynamic-resolution semantics are not supported or guaranteed;
schemas that rely on those semantics are outside this contract.

Fail-closed errors MUST be `*SchemaPolicyError` with the stable `Kind` and JSON
Pointer `Path` documented in the matrix. A failure returned by `Caller` MUST
occur before invoking the Codex runner. Whole-document compilation failures use
`invalid_schema` at the root; property null-analysis failures use the exact
property pointer.

Serialization MAY change object-key order, whitespace, number spelling, escape
spelling, and the order of the normalized `required` array. Byte identity is
not promised.

### Normative compatibility matrix

Each row ID is backed by the same-named subtest in
`TestStrictOutputSchemaCompatibilityMatrix`. “Preserved” means accepted with
preserved ordinary decoded-value semantics; “limitation” means accepted with
the stated explicit limitation; “fail-closed” means rejected before execution
with the listed stable error.

| Normative row / Go or schema shape | Required result |
| --- | --- |
| `required-scalar-preserved` — required scalar | Preserved; no presence normalization |
| `optional-pointer-preserved` — `*T,omitempty` whose exact schema admits null | Preserved; property promoted to required |
| `optional-scalar-fails-closed` — non-nullable scalar with `omitempty` | Fail-closed: `optional_non_nullable` at `/properties/score` |
| `nested-optional-pointer-preserved` — nested nullable pointer | Preserved; the same rule applies at the nested pointer |
| `optional-map-fails-closed` — `map[string]T,omitempty` projected as non-nullable by the resolved `llmkit` module | Fail-closed: `optional_non_nullable` at `/properties/labels`; no map-specific widening |
| `optional-slice-preserved` — `[]T,omitempty` whose exact schema admits null | Preserved under ordinary nil-slice decoding |
| `optional-pointer-to-slice-preserved` — `*[]T,omitempty` | Preserved under ordinary nil-pointer decoding |
| `optional-raw-message-has-decoding-limitation` — `json.RawMessage,omitempty` | Limitation: accepted, but absence decodes to nil while explicit null is retained as `"null"` |
| `custom-unmarshaler-has-decoding-limitation` — nullable schema for a custom unmarshaler | Limitation: accepted schema; decoded-value or application equivalence is not guaranteed |
| `local-ref-preserved` — supported local JSON Pointer `$ref` | Preserved after resolving and validating the complete referenced schema |
| `nested-ref-with-sibling-constraint-fails-closed` — nested references whose applicable sibling rejects null | Fail-closed: `optional_non_nullable` at `/properties/value` |
| `boolean-schema-has-codex-limitation` — `true` or `false` schema | Limitation: accepted unchanged by this policy; no object normalization or Codex acceptance guarantee |
| `draft-2020-12-preserved` — explicit draft 2020-12 | Preserved using draft 2020-12 semantics |
| `draft-7-ref-sibling-limitation` — explicit draft-07 `$ref` with siblings | Limitation: accepted using draft-07 semantics, where `$ref` siblings are ignored |
| `unversioned-tuple-fails-closed` — tuple-form `items` without `$schema` | Fail-closed: `invalid_schema` under the default draft 2020-12 semantics |
| `annotation-data-does-not-select-draft-7` — annotation payload containing tuple-form `items` without `$schema` | Fail-closed: `optional_non_nullable` at `/properties/value`; annotation data MUST NOT select Draft 7 |
| `unsupported-draft-fails-closed` — unknown explicit draft identifier | Fail-closed: `invalid_schema` at the root path |
| `unknown-annotation-preserved` — unknown annotation keyword | Preserved by decoded JSON value semantics |
| `unknown-assertion-has-validation-limitation` — unknown assertion keyword | Limitation: value preserved, enforcement not guaranteed |
| `dynamic-anchor-has-resolution-limitation` — `$dynamicAnchor` without `$dynamicRef` | Limitation: value preserved, dynamic-resolution semantics not guaranteed |
| `vocabulary-fails-closed` — `$vocabulary` declaration | Fail-closed: `invalid_schema` at the root path |
| `additional-properties-schema-preserved` — object schema in `additionalProperties` | Preserved; nested object properties undergo the same required/null rule |
| `conditional-null-fails-closed` — conditional schema that rejects null | Fail-closed: `optional_non_nullable` at `/properties/value` |
| `cyclic-ref-fails-closed` — cyclic local reference graph | Fail-closed: `cyclic_ref` at the reference pointer |
| `external-ref-fails-closed` — external `$ref` | Fail-closed: `external_ref` at `/$ref` |
| `unresolvable-ref-fails-closed` — missing local target | Fail-closed: `unresolvable_ref` at `/$ref` |
| `dynamic-ref-fails-closed` — `$dynamicRef` | Fail-closed: `unsupported_dynamic_ref` at `/$dynamicRef` |

Schemas without an explicit `$schema` always use draft 2020-12. Tuple-form
`items` without `$schema` fail closed. Explicit dialects other than draft-07
and draft 2020-12 fail closed until added to this contract and matrix.

Changelog: [CHANGELOG.md](CHANGELOG.md). Notices:
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). See repository
[CONTRIBUTING.md](../../CONTRIBUTING.md) and [SECURITY.md](../../SECURITY.md).
