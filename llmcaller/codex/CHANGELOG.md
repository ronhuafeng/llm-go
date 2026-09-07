# Changelog

This project follows Semantic Versioning.

## [Unreleased]

### Changed

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
