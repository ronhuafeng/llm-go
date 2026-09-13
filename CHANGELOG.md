# Changelog

The repository is one Go module, `github.com/ronhuafeng/llm-go`. This file is
the only current release changelog. Package sections record user-visible
changes in `llmkit`, `codexsdk`, and `llmcaller/codex`.

This project follows Semantic Versioning. Before v1.0.0, breaking public API
changes may occur in minor releases.

## [Unreleased]

## [0.1.0] - 2026-09-13

### Repository

- Collapse the repository to one root module and one version. Consumers resolve
  `github.com/ronhuafeng/llm-go@vX.Y.Z` and keep the existing import paths for
  `llmkit`, `codexsdk`, and `llmcaller/codex`. The Go floor is `1.25.0`.

### codexsdk

#### Added

- Expose generated-baseline provenance and initialize runtime identity as
  separate facts. Runtime compatibility stays unknown: `initialize` reports
  identity, not whole-surface compatibility, and a successful turn does not
  change that.

#### Fixed

- Keep one application/server-request failure at Exact Run scope. A missing
  handler, handler error/panic, or invalid typed response still sends a JSON-RPC
  error and no semantic result. When the decoded request carries thread/turn
  identity, that cause finishes the matching Exact Run. It does not fail the
  whole `Client` or unrelated Exact Runs. Uncorrelated requests are not guessed
  onto a run. Transport and response-write failures remain client-global.
- Preserve JSON number tokens across JSON-RPC routing. Integer request IDs above
  `2^53`, generated integers, dynamic `JSONValue` numbers, and `ProtocolError.Data`
  no longer round through default `map[string]any` `float64` decoding.
- Preserve caller-context cancellation on synchronous Exact Run `Start` and
  `Resume`. A canceled or deadline-exceeded drain returns the caller cause with
  the latest partial evidence while the shared run is still non-terminal;
  `Stream.Next` remains cursor-local.
- Match app-server completion-summary semantics for Exact Run `FinalResponse`:
  an explicit `final_answer` still wins, and a phase-absent completed agent
  message remains a present legacy completion rather than being dropped.
- Fail closed on resume when `ThreadResumeResponse.Thread.ID` is empty instead
  of substituting the requested `ThreadResumeParams.ThreadID`.

#### Changed

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

### llmcaller/codex

#### Changed

- Neutral token usage is one new-thread/one-turn adapter attempt. The Codex
  projection publishes `ThreadTokenUsage.Total` under that lifecycle and does
  not relabel `Last` or per-upstream-response counts as attempt-scoped evidence.
- Stop publishing thread-start or `model/rerouted` identifiers as attempt-wide
  served-model evidence. Those exact facts remain in `BackendDetails`;
  `ExecutionEvidence.Model` stays unknown unless serving is proven at attempt
  scope.
- **Breaking (pre-v1):** project exact `FinalResponsePresent` into neutral
  `Observation[string]`. Present-empty stays observed; absence stays unknown.
- Delete unused `responseFromRun`; `Call` still projects through
  `projectNeutralResponse`.
- **Breaking (pre-v1):** application-owned `AdmitTurn` now receives both the
  exact thread-start observation and the exact pending `turn/start` request.
  The adapter still does not merge those values or choose an execution policy.
- **Breaking (pre-v1):** stop rewriting caller-owned JSON Schemas by promoting
  optional properties to required. Codex schema admission now preserves the
  accepted JSON instance language or fails closed with
  `optional_property_unsupported`; nullable/Go-decoding equivalence is no
  longer used to justify semantic narrowing.
- **Breaking (pre-v1):** remove adapter-owned named execution safety profiles,
  including `ReadOnlyEphemeralOptions`. Neutral `Caller` construction now
  requires application-owned `Options.Defaults.AdmitTurn`; approval, sandbox,
  ephemeral, permission, CWD, workspace, and related exact settings remain
  caller-owned rather than being rewritten by the adapter.
- **Breaking (pre-v1):** publish `"codex"` as execution-backend identity, not
  model-provider identity. Exact Codex details now use `BackendDetails` /
  `BackendName`; actual provider identity stays unknown unless exact lower-layer
  serving evidence establishes it independently. Effective model and usage
  observations remain independent.
