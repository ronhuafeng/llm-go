# Codex adapter

A loss-aware join between provider-neutral inference and exact Codex facts.
Destination: [NORTHSTAR.md](../../NORTHSTAR.md). Language:
[CONTEXT.md](CONTEXT.md).

Requires Go 1.23 or newer. OS support and testing tiers are in
[SUPPORT.md](../../SUPPORT.md).

```sh
go get github.com/ronhuafeng/llm-go/llmcaller/codex@latest
```

## Executable example

[`example_test.go`](example_test.go) is the canonical three-layer consumer
example. It composes `llmkit`, this adapter, and `codexsdk` with a deterministic
fake `ThreadRunner`, so ordinary tests compile and execute the complete public
shape without credentials or provider availability.

```sh
GOWORK=off go test ./...
```

The optional repository-level live smoke is documented in
[`../../docs/verify.md`](../../docs/verify.md).

## Application-owned execution admission

`llmcaller/codex` does not define a safe execution profile. A neutral
`llmadapter.Caller` requires the application to provide
`Options.Defaults.AdmitTurn`.

The lifecycle is deliberately split:

1. the application chooses exact Codex request settings such as approval,
   sandbox, ephemeral mode, CWD, workspace roots, model, effort, and service
   tier;
2. `codexsdk` performs `thread/start` and decodes the effective Server
   Observation;
3. the application-owned `AdmitTurn` receives that observation before
   `turn/start`;
4. only an accepted observation may proceed to the model-directed turn.

The adapter forwards the callback and caller-controlled exact defaults. It does
not overwrite them with a named profile, classify a combination as safe, or
turn read-only/ephemeral settings into confidentiality or provider-retention
claims. Missing admission fails construction with `ErrMissingAdmission`.

The adapter still owns `Turn.ThreadID`, `Turn.Input`, and `Turn.OutputSchema`
because those fields are the mechanical projection of a neutral call. Effectful
or provider-specific operations remain available through explicit `codexsdk`
Exact Run / `ThreadRunner` surfaces under application authority.

## Evidence paths

- `Call` implements `llmadapter.Caller` and publishes final text plus neutral
  observations.
- `CallDetailed` returns the exact `codexsdk.StartedThreadRun`, including
  partial evidence on failure.
- `CallStream` preserves the exact streaming lifecycle and the same pre-turn
  application admission.

`Call` publishes `Execution.BackendName == "codex"` and an isolated
`codexcaller.Details` value through `BackendDetails` when the exact snapshot can
be isolated safely. Backend identity means execution used this Codex
adapter/runtime; it is not an upstream model-provider fact.

`Execution.ProviderName` stays unknown unless exact lower-layer evidence proves
the actual serving provider independently. Adapter identity, model names,
requested settings, credentials, and Codex thread configuration do not fill
provider identity. Effective model and usage evidence remain independently
projected from attributable exact observations. Observed zero counts remain
distinct from unreported counts.

## Schema admission

`StrictOutputSchemaFromJSON` is the Codex-specific representation-admission
boundary. It parses the exact caller JSON Schema, selects supported dialect
semantics, rejects unsupported references/vocabularies, and preserves unknown
keyword JSON values where the generated protocol representation can carry them.
Failures are typed `*SchemaPolicyError` values and occur before runner
invocation.

The current implementation still contains a separately tracked semantic
problem: some optional properties are promoted to required when their complete
schema admits `null`. That changes the accepted JSON instance language and is
not part of the execution-admission contract above. Issue #228 owns its removal;
this execution-policy change does not preserve or justify that narrowing.

Schemas without an explicit `$schema` use draft 2020-12. Draft 7 and draft
2020-12 are the explicitly supported dialects. External resources,
`$dynamicRef`, cyclic/unresolvable local references, unsupported dialects, and
`$vocabulary` declarations fail closed. Serialization is semantic JSON
serialization, not byte preservation.

Changelog: [CHANGELOG.md](CHANGELOG.md). Notices:
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). See repository
[CONTRIBUTING.md](../../CONTRIBUTING.md) and [SECURITY.md](../../SECURITY.md).
