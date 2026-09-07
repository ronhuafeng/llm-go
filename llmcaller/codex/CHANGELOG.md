# Changelog

This project follows Semantic Versioning.

## [Unreleased]

### Changed

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
