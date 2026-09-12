# Changelog

## [Unreleased]

### Added

- Expose generated-baseline provenance and initialize runtime identity as
  separate facts. Runtime compatibility stays unknown: `initialize` reports
  identity, not whole-surface compatibility, and a successful turn does not
  change that.

### Fixed

- Preserve caller-context cancellation on synchronous Exact Run `Start` and
  `Resume`. A canceled or deadline-exceeded drain returns the caller cause with
  the latest partial evidence while the shared run is still non-terminal;
  `Stream.Next` remains cursor-local.
- Match app-server completion-summary semantics for Exact Run `FinalResponse`:
  an explicit `final_answer` still wins, and a phase-absent completed agent
  message remains a present legacy completion rather than being dropped.
- Fail closed on resume when `ThreadResumeResponse.Thread.ID` is empty instead
  of substituting the requested `ThreadResumeParams.ThreadID`.

### Changed

- **Breaking generated-surface change (pre-v1):** publish the `rust-v0.154.0`
  classified protocol surface. MCP elicitation keeps the `openai/userVerification`
  variant with its schema-defined payload. Optional `account/rateLimits/read`
  params, thread `environments`, and experimental `userVerification/*` methods
  are generated from the same classified baseline. Experimental
  `UserVerificationRpcError` and the closed `UserVerificationErrorDetails`
  tagged union (`invalidRequest`, `unavailable`, `cancelled`, `failed`) are
  generated from `v2/UserVerificationRpcError.json` instead of remaining omitted
  while coverage claimed they were generated.
- **Breaking generated-surface change (pre-v1):** protocol generation preserves
  shared object properties together with `oneOf` payloads. MCP elicitation
  request params now retain every schema-defined variant, and
  `ServerNotification.emittedAtMs` is no longer dropped from the tagged union.
- Experimental member/variant admission now follows classified generated
  protocol surface facts instead of a handwritten field inventory. Stable
  members such as `excludeTurns` are no longer rejected.
- Exact Run notification attribution now uses present correlation, including
  optional `threadId` and nested `thread.id`. `thread/started` is preserved
  across the thread/start-to-attach window.
- Attaching Exact Runs no longer treat an unpublished turn ID as a wildcard
  for same-thread turn-scoped notifications. Proven `turn/start` identity is
  armed before later frames are routed, so resume restored usage for a
  historical turn is not attached to the new turn.
- **Breaking (pre-v1):** `AdmitTurn` now receives the exact decoded
  `ThreadStartResponse` and the exact pending `TurnStartParams`, including the
  composition-owned thread ID that will be sent. The SDK still does not merge
  requested turn overrides into observed thread facts.
- **Breaking (pre-v1):** resumed Exact Runs expose the same two-input admission
  seam after `thread/resume` and before `turn/start`. Missing observed resume
  thread identity still fails closed before admission.

- **Breaking (pre-v1):** stop synthesizing application decisions and environment
  facts for exact server requests. A missing `ServerRequestHandler` now fails
  with a typed exact server-request cause and JSON-RPC error; callback shutdown
  likewise returns a protocol error instead of implicit decline, empty answers,
  empty permissions, or a locally generated current-time value.
- Preserve exact final-answer presence separately from its text value. An
  observed empty `final_answer` remains present, and a server-reported
  `completed` turn is no longer converted into an SDK failure merely because a
  non-empty convenience final response is unavailable.
- **Breaking generated-surface change (pre-v1):** publish the `rust-v0.153.4`
  classified protocol surface. Stable `ThreadItem` can carry
  `AsyncUserInputQuestion` lists. `plugin/reconcile` and
  `modelProvider/authRecovery*` methods are generated. `GetAccountRateLimitsResponse`
  preserves backend-owned `rateLimitUpsell` as a JSON value.

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
