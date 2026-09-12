# Changelog

This project follows Semantic Versioning.

## [Unreleased]

### Changed

- Neutral token usage is one new-thread/one-turn adapter attempt. The Codex
  projection publishes `ThreadTokenUsage.Total` under that lifecycle and does
  not relabel `Last` or per-upstream-response counts as attempt-scoped evidence.
- Stop publishing thread-start or `model/rerouted` identifiers as attempt-wide
  served-model evidence. Those exact facts remain in `BackendDetails`;
  `ExecutionEvidence.Model` stays unknown unless serving is proven at attempt
  scope.
- **Breaking (pre-v1):** project exact `FinalResponsePresent` into neutral
  `Observation[string]`. Present-empty stays observed; absence stays unknown.
- **Breaking (pre-v1):** application-owned `AdmitTurn` now receives both the
  exact thread-start observation and the exact pending `turn/start` request.
  The adapter still does not merge those values or choose an execution policy.

- **Breaking (pre-v1):** stop rewriting caller-owned JSON Schemas by promoting
  optional properties to required. Codex schema admission now preserves the
  accepted JSON instance language or fails closed with
  `optional_property_unsupported`; nullable/Go-decoding equivalence is no
  longer used to justify semantic narrowing.
- **Breaking (pre-v1):** remove adapter-owned named execution safety profiles,
  including `ReadOnlyEphemeralOptions`. Neutral `Caller` construction now
  requires application-owned `Options.Defaults.AdmitTurn`; approval, sandbox,
  ephemeral, permission, CWD, workspace, and related exact settings remain
  caller-owned rather than being rewritten by the adapter.
- **Breaking (pre-v1):** publish `"codex"` as execution-backend identity, not
  model-provider identity. Exact Codex details now use `BackendDetails` /
  `BackendName`; actual provider identity stays unknown unless exact lower-layer
  serving evidence establishes it independently. Effective model and usage
  observations remain independent.

## [0.8.1] - 2026-09-07

### Changed

- Require the published `llmkit v0.10.0` module so `GOWORK=off` consumers
  compile against `Value`, `Run`, and `ErrExhausted`.
- Require the published `llmkit v0.11.0` module so `GOWORK=off` consumers
  compile against `llmschema.Contract` and `Compile`.
- Require the published `llmkit v0.12.0` module so `GOWORK=off` consumers
  compile against the `RequestFor` removal and documented `Value` /
  `ValueWithContract` paths.

## [0.8.0] - 2026-09-07

### Changed

- **Breaking (pre-v1):** remove `Caller.IsolatesNeutralFacts`. Loss-aware
  neutral projection remains the published `Call` behavior; compatibility
  evidence is the module version and exported contract, not a capability
  boolean.
- Require the published `llmkit v0.9.0` module so `GOWORK=off` consumers
  compile against Observation-only execution evidence.

## [0.7.0] - 2026-09-07

### Fixed

- **Breaking (pre-v1):** project each provider-neutral fact from independently
  isolated Codex evidence. Failure to isolate exact Provider details no longer
  erases attributable effective-model or usage evidence. Observed token zeros
  stay distinct from unreported dimensions.

### Changed

- **Breaking (pre-v1):** construct a provider-neutral `Caller` only with a
  named effect-safe profile. Unrestricted `New(Options{Runner})` no longer
  implements inference over effect-capable turns. Effective read-only,
  never-approve, and ephemeral facts are admitted from decoded thread-start
  observation before `turn/start`. Effectful Codex use stays on explicit
  Exact Run / `ThreadRunner` surfaces.
- Require the published `llmkit v0.8.0` and `codexsdk v0.8.0` modules and
  attach `AdmitTurn` / record `Observation` through those typed APIs.
- Document the evidence-bearing `llmadapter.ValueDetailed` path in the
  module example and README.

## [0.6.0] - 2026-08-30

### Changed

- **Breaking semantic change (pre-v1):** tuple-form `items` without `$schema`
  fail closed under Draft 2020-12. Dialect selection depends only on the root
  `$schema` declaration.
- Require the published `llmkit v0.7.0` and `codexsdk v0.6.1` modules.

### Fixed

- Publish Provider details from one isolated Exact Run snapshot and omit
  unisolated reference evidence when snapshotting fails.
