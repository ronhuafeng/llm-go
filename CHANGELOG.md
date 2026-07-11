# Changelog

All notable changes to this project will be documented in this file.

This project follows Semantic Versioning. Before v1.0.0, breaking public API
changes may occur in minor releases, but they must be documented here.

## [Unreleased]

### Removed

- Removed the v0.2-deprecated `llmschema.DecodeString`,
  `llmadapter.Options`, `llmadapter.Op`, `llmadapter.NewOp`, `settle.Bind`, and
  `settle.Runner` helper surface for v0.3.
- Removed `llmadapter.ErrNilRender`, which was used only by the removed legacy
  adapter operation. `llmstep.ErrNilRender` remains available for `llmstep`.

See [Migrating to v0.3](docs/v0.3-migration.md) for replacements. These are
intentional pre-v1 breaking changes.

## [0.2.0] - 2026-07-11

### Added

- Detailed provider-neutral call results with execution evidence, typed
  provider details, explicit request/call/decode errors, and partial response
  preservation.
- Schema-enforced structured-output decoding with stable typed violations.
- Detailed `settle` and `llmstep` results that preserve candidate and failure
  history through validation errors and retry exhaustion.
- A canonical `go/types` allowlist for the handwritten public API.

### Changed

- `Value`, `settle.Run`, and `llmstep.Run` are projections of their detailed
  counterparts and return the same error while retaining available output.
- `llmstep` records request, call, decode, validation, and sanitizer failures
  without retaining rendered prompts or raw model output in validation errors.

### Deprecated

- `llmschema.DecodeString`; use `Decode`.
- `llmadapter.Options`, `Op`, and `NewOp`; use `llmstep` or implement
  `settle.Op` directly.
- `settle.Bind` and `Runner`; call `Run` or `RunDetailed` directly.

## [0.1.0] - 2026-06-11

### Added

- Initial public release.
- `settle` bounded stable loop primitive.
- `llmschema` Go type to structured output JSON Schema projection and decode.
- `llmadapter` provider-neutral typed request and value helpers.
- Open-source project documentation, including contribution, security, support,
  release, code of conduct, issue templates, pull request template, and
  third-party dependency provenance guidance.
- GitHub Actions CI for formatting, vet, and tests.
- Dependabot configuration for Go modules and GitHub Actions.
