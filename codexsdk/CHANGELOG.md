# Changelog

## [Unreleased]

## [0.8.0] - 2026-09-07

### Added

- **Breaking (pre-v1):** add `StartThreadRunRequest.AdmitTurn` so Exact Run
  startup can inspect the decoded `ThreadStartResponse` and reject
  continuation fail-closed before `turn/start`. Rejection preserves the exact
  partial `StartedThreadRun` and reports `ErrTurnAdmissionRejected`.

### Changed

- **Breaking generated-surface change (pre-v1):** publish the `rust-v0.151.0`
  classified protocol surface. Stable `RawResponseCompletedNotification` is
  removed. `CodexErrorInfo`, `ThreadItem`, and `TurnError` gain stable
  members. `ClientRequest` and `TurnStartParams` remain mixed Generated
  Facades with added members. `Turns` becomes a mixed Generated Facade.

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
