# Changelog

All notable changes to this project should be documented in this file.

This project follows Semantic Versioning.

## [Unreleased]

## [0.6.0] - 2026-08-30

### Changed

- **Breaking semantic change (pre-v1):** require legacy tuple schemas to declare
  Draft 7 explicitly and default every unversioned schema to Draft 2020-12. See
  [Migrating to v0.6](UPGRADE.md).
- Require the published `llmkit v0.7.0` and `codexsdk v0.6.1` modules.

### Fixed

- Publish Provider details from one isolated Exact Run snapshot and omit
  unisolated reference evidence when snapshotting fails.

## [0.5.0] - 2026-07-15

### Changed

- **Breaking import-path change (pre-v1):** moved the Codex adapter from
  `github.com/ronhuafeng/llmcaller-codex-go/llmcaller/codex` to the flattened
  module-root package `github.com/ronhuafeng/llm-go/llmcaller/codex` and now
  requires the verified `llmkit v0.6.0` and `codexsdk v0.6.0` modules. Package
  names, exported identifiers, schema policy, effective-profile behavior,
  neutral evidence projection, exact-result escape hatches, and the Go 1.23
  minimum remain unchanged. See
  [Migrating to v0.5](UPGRADE.md).

## [0.4.2] - 2026-07-14

### Deprecated

- Froze `github.com/ronhuafeng/llmcaller-codex-go` at its final legacy release.
  The replacement module is
  `github.com/ronhuafeng/llm-go/llmcaller/codex`, beginning with `v0.5.0`; see
  the [repository migration guide](UPGRADE.md) for the exact
  adapter and upstream import mappings.
- Ended feature and security maintenance for the legacy module path. Existing
  immutable versions remain available through the public Go proxy until the
  repository is archived after replacement verification.

### Fixed

- Fixed named-profile verification for decoded partial thread starts that lack
  a required thread ID. Neutral, detailed, and streaming paths now retain the
  exact start evidence and expose both the SDK identity cause and
  `ErrEffectiveProfile` when applicable, without synthesizing a profile error
  for failures before any response is decoded.
- Bound the tag gate's reviewed compatibility contract to the copy shipped in
  the proxy-resolved caller module. The gate now requires identical checkout
  and module-artifact SHA-256 digests before it validates the exact version
  tuple, and retains both digests plus fail-closed caller origin/tag-commit
  provenance. The stable-tag path is also exposed as a bounded integration test
  for release validation.

## [0.4.1] - 2026-07-13

- Requires the published `llmkit-go v0.4.1` and `codexsdk-go v0.5.0` module
  tags. The caller module graph contains no replacements, excludes, workspace
  overrides, prereleases, or pseudo-versions.

- Strengthened the proxy-backed tag consumer to read the tagged compatibility
  contract, require the exact declared caller/llmkit/codexsdk version tuple, and
  retain the contract digest, declared/resolved versions, sums, and typed call
  evidence for release review.
- Removed stale API-inventory failure guidance that treated the historical v0.2
  proposal as normative; current review guidance points to the canonical
  inventory, active compatibility contract, release obligations, behavior
  tests, and clean consumer.

## [0.4.0] - 2026-07-13

- Requires the published `llmkit-go v0.4.0` and `codexsdk-go v0.4.0` module
  tags. The caller module graph contains no replacements, excludes, workspace
  overrides, or pseudo-versions.

- Added a `v*` tag-triggered, bounded-retry proxy consumer gate that resolves
  the exact caller tag from `proxy.golang.org`, rejects non-stable upstream
  versions and module overrides, records module sums, and runs a deterministic
  typed three-layer call from a clean temporary module.
- Replaced the historical v0.2 proposal byte-mirror gate with a machine-readable
  compatibility contract tied to resolved module tags, exported API inventory,
  schema matrix, clean consumer, and complete three-layer canary.
- Changed the handwritten API inventory to record only externally observable
  exported struct fields and methods, so private representation changes do not
  become compatibility obligations while public surface changes remain gated.
- **Breaking (pre-v1):** `CallStream` now returns an adapter-owned exact
  `*codexcaller.Stream` wrapper instead of
  `*codexsdk.Stream[codexsdk.StartedThreadRun]`. The wrapper applies the same
  effective read-only, never-approve, ephemeral postcondition as
  `CallDetailed` while preserving full SDK results, notifications, lifecycle
  operations, and a typed `SDKStream` escape hatch. SDK and
  `ErrEffectiveProfile` causes remain distinguishable through `errors.Is`.

## [0.3.0] - 2026-07-13

- Updated `llmkit-go` and `codexsdk-go` to their published `v0.3.0` stable
  module tags without changing caller API or behavior.
- Promoted the verified `v0.3.0-rc.2` caller surface unchanged, including the
  read-only ephemeral safety profile, schema policy, exact evidence paths, and
  notification-ordering guarantees.

## [0.3.0-rc.2] - 2026-07-13

- Updated `codexsdk-go` to `v0.3.0-rc.2` so pending notifications are
  delivered before live notifications while preserving per-source order.
- Retains `llmkit-go v0.3.0-rc.1` and all caller API, schema, safety, and
  evidence contracts from `v0.3.0-rc.1`.

## [0.3.0-rc.1] - 2026-07-13

- Defined the normative schema-equivalence and fail-closed contract, expanded
  the compatibility matrix with same-named public-boundary tests, and documented
  decoded-value and application-semantic limitations without promising byte
  identity.
- Replaced handwritten schema null-admission analysis with draft-compatible
  validator probes that preserve JSON values and fail closed before runner
  invocation when a property schema cannot be compiled or resolved.
- Enforced the named read-only ephemeral profile before every runner call by
  rejecting conflicting defaults, normalizing unset safety fields, and
  reapplying the requested policy while retaining effective-policy checks.
- Adapted the caller and complete three-layer canary to the concrete SDK root
  client while preserving the narrow consumer-owned `ThreadRunner` boundary.
- Strengthened canary coverage for accepted evidence after transport failure,
  pending and live notification ordering, attribution, shutdown, and first-cause
  preservation.
- Requires `llmkit-go v0.3.0-rc.1` and `codexsdk-go v0.3.0-rc.1` from their
  published module tags.

## [0.2.0] - 2026-07-11

- Replaced projected Codex options with exact `StartThreadRunRequest` defaults
  and a minimal consumer-owned `ThreadRunner` interface.
- Added exact detailed and streaming paths while keeping `Call` as a neutral
  projection with immutable provider details and partial-result preservation.
- Added effective-model reroute and total-token-usage projection.
- Made the read-only ephemeral profile enforce exact thread/turn sandbox,
  approval, and ephemeral facts.
- Changed structured schema handling to preserve unknown JSON values and reject
  optional non-nullable, external, unresolved, dynamic, and cyclic references.
- Added a canonical handwritten API allowlist, three-layer compiled example,
  migration guide, compatibility matrix, and cross-repository release gates.
- Requires `llmkit-go v0.2.0` and `codexsdk-go v0.2.0`.

## [0.1.0] - 2026-06-11

- Initial public release.
- Initial Codex caller adapter for `llmkit-go`.
- Added Codex structured output schema normalization before thread start.
- Added boundary tests for adapter dependencies.
- Added open-source project documentation for installation, quick start,
  failure semantics, security boundaries, compatibility, release, support, code
  of conduct, issue templates, pull request template, and third-party notices.
- Added GitHub Actions CI for `gofmt`, `go vet`, and `go test ./...`.
- Added Dependabot configuration for Go modules and GitHub Actions.
