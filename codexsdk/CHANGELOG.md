# Changelog

## [Unreleased]

## [0.7.0] - 2026-08-30

### Changed

- **Breaking generated-surface change (pre-v1):** publish the `rust-v0.150.1`
  classified protocol surface. Stable Amazon Bedrock credential-source facts
  and several stable metadata fields are removed. `Accounts`, `MCPServers`,
  and `Plugins` become mixed Generated Facades.
  `protocolv2.ThreadMetadataUpdateParams` becomes a mixed classified params
  type.

### Fixed

- Keep Exact Run History Cursor cancellation caller-local so `Next` cannot
  terminate a shared Exact Run.
