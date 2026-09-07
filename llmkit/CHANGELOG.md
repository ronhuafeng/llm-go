# Changelog

This project follows Semantic Versioning. Before v1.0.0, breaking public API
changes may occur in minor releases.

## [Unreleased]

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
