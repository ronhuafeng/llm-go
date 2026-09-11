# Changelog

This project follows Semantic Versioning. Before v1.0.0, breaking public API
changes may occur in minor releases.

## [Unreleased]

### Changed

- **Breaking (pre-v1):** separate execution-backend identity from actual
  model-provider identity. `Response.BackendDetails` replaces
  `ProviderDetails`; backend details expose `BackendName()`, and
  `ExecutionEvidence.BackendName` records that runtime/adapter identity.
  `ExecutionEvidence.ProviderName` is now a presence-aware `Observation` and
  may be populated only from attributable provider evidence. Adapter names,
  model identifiers, requested settings, endpoints, and heuristics do not
  establish provider identity. `ErrBackendIdentityMismatch` replaces the old
  provider-identity mismatch error for backend-details consistency.
- **Breaking (pre-v1):** make model-facing retry repair an explicit
  application-owned projection. A rejected attempt that can retry now requires
  `Step.Sanitizer`; with no projection configured it fails closed with
  `ErrMissingRepairProjection`. The toolkit still owns retry bounds, iteration
  stamping, and separation of validator judgment from later model input.
- Point readers at the repository authority-to-effect proof that continues
  past `llmstep` judgment. That example is workspace integration, not this
  module's `GOWORK=off` suite.

### Removed

- **Breaking (pre-v1):** remove `StrictRepairSanitizer`, `ErrUnsafeRepair`, and
  the built-in credential/URL/path/content inspection policy. Disclosure,
  redaction, secret detection, and repair-content policy are application-owned.

## [0.12.0] - 2026-09-07

### Changed

- Describe `llmadapter.Value` as the default typed-inference path and
  `ValueWithContract` as the explicit Contract-reuse path. Both return the
  same evidence-bearing result.

### Removed

- **Breaking (pre-v1):** remove `llmadapter.RequestFor`. Construct `Request`
  from prompt text and an owned `Contract.SchemaJSON()` when calling
  `Caller.Call` directly. `SchemaJSONFor` and one-shot `Decode` remain.

## [0.11.0] - 2026-09-07

### Added

- Add `llmschema.Contract` so one compiled typed schema owns the
  provider-neutral request JSON and the structural decode validator.
  `llmadapter.Value` compiles one contract per call. `llmstep.Run`
  reuses that contract across bounded attempts. The zero contract is
  uncompiled and does not guess a schema.

## [0.10.0] - 2026-09-07

### Changed

- **Breaking (pre-v1):** rename `ValueDetailed` to `Value` and
  `RunDetailed` to `Run`. Remove the obsolete `Detailed` suffix and do
  not keep aliases. Replace `ErrUnsettled` with `ErrExhausted` when no
  attempt produced an accepted judgment before `MaxIter`.

## [0.9.0] - 2026-09-07

### Changed

- **Breaking (pre-v1):** remove leftover `ExecutionEvidence.EffectiveModel`
  and scalar `TokenUsage` count fields. Neutral model and token facts are
  only `Observation` values: unknown stays unknown, and `Observed(0)` or
  an observed empty model stays distinct from absence.
- **Breaking (pre-v1):** reject an `llmstep.Step` with `Validate == nil`
  before `Render` or a provider call. The configuration error is
  `ErrNilValidate` and no attempt evidence is published. `ErrNoJudgment` is
  removed. Decode-only proposition production remains on `llmadapter`.

## [0.8.0] - 2026-09-07

### Added

- Add `llmadapter.Observation` so neutral execution facts can be unknown
  without collapsing into a Go zero value. Token measurements expose
  presence-aware `Input`, `CachedInput`, `Output`, and `ReasoningOutput`
  fields; effective model presence lives on `ExecutionEvidence.Model`.
  Requested settings, defaults, estimates, and inferred names cannot
  populate an observation. Existing scalar `TokenUsage` and
  `EffectiveModel` fields do not establish presence and remain only so
  unpublished adapters keep compiling until they migrate.
  `ExecutionEvidence.ObserveModel` and `TokenUsage.ObserveCounts` record
  presence through `Observed` and keep those leftover scalars in sync.

### Changed

- Define `llmadapter.Caller` as an inference capability. The neutral
  request remains prompt plus output schema and cannot grant effect
  authority. Prompt text and model output are not authority. An
  implementation may satisfy `Caller` only when reachable model-directed
  execution is already effect-free or independently authorized outside the
  model request.
- **Breaking (pre-v1):** make `llmadapter.ValueDetailed` and
  `llmstep.RunDetailed` the only public typed-inference result paths. They
  return the proposition or output together with available call and attempt
  evidence on success and failure.
- **Breaking (pre-v1):** replace the shared `Feedback` / `ValidationResult`
  model with distinct `Finding`, `Judgment`, and `Repair` types. Accepted
  step success requires an explicit deterministic judgment. A missing judge
  leaves `Attempt.Judgment` nil and returns `ErrNoJudgment`. Repair is
  projected only for a later render. `StrictFeedbackSanitizer` and
  `ErrUnsafeFeedback` are removed in favor of `StrictRepairSanitizer` and
  `ErrUnsafeRepair`.

### Removed

- **Breaking (pre-v1):** delete `llmadapter.Value` and `llmstep.Run`. Those
  wrappers returned only the typed value and discarded provider-neutral
  response and attempt evidence.
- **Breaking (pre-v1):** delete public `llmkit/settle`. Bounded inference
  termination is owned by `llmstep` (`ErrInvalidMaxIter`, `ErrUnsettled`).
  Callers that used `settle.Run` or `settle.RunDetailed` must own their retry
  loop or use `llmstep`.

## [0.7.0] - 2026-08-30

### Changed

- **Breaking semantic change (pre-v1):** require an explicit sanitizer before
  free-form retry feedback can be sent to a model. When `Step.Sanitizer` is
  nil, a non-empty `Feedback.Summary` on an attempt that can retry is a
  sanitize-stage error wrapping `llmstep.ErrUnsafeFeedback`.

### Fixed

- Stamp model-facing retry feedback with the framework-assigned iteration.
- Return context cancellation observed after successful retry phases while
  preserving partial evidence.
- Reject typed-nil step callers before prompt rendering begins.
