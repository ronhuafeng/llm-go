# Changelog

This project follows Semantic Versioning. Before v1.0.0, breaking public API
changes may occur in minor releases.

## [Unreleased]

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
