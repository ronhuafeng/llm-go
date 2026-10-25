# Changelog

The repository is one Go module, `github.com/ronhuafeng/llm-go`. This file is
the only current release changelog. Package sections record user-visible
changes in `llmkit`, `codexsdk`, and `llmcaller/codex`.

This project follows Semantic Versioning. Before v1.0.0, breaking public API
changes may occur in minor releases.

## [Unreleased]

### codexsdk

#### Added

- Synchronize the App Server protocol with Codex `rust-v0.164.0`
  (`6aaac694803004a63c6f393d153c96d22521a6b9`), adding feedback ZIP saves,
  thread sampling and cancellation, turn metadata patches, application storage
  requirements, and expanded hook, plan and realtime fields.
  `ThreadItemImageGeneration.Result` changes from `string` to `*JSONValue`,
  alongside `FileID` and observed `InputImages`, to preserve the upstream open
  `anyOf` alternatives. See the SDK README for migration.

- Synchronize the App Server protocol with Codex `rust-v0.163.0`
  (`83086188ae0614bd882818ff1aa23f3194962012`), adding thread item reads,
  revision-guarded read-state updates and notifications, read-state projections,
  output retention, and expanded realtime voice options.

- Show native upstream goal progress in GitHub Actions log groups, report
  terminal failures as annotations, and link retained PR/CI results and evidence
  from the run summary without changing the single-goal acceptance flow.

- Add `NewContext` for caller-bounded App Server validation/launch/initialize,
  and `CloseContext` with separate owned-process and unresolved callback facts.
  Process cleanup no longer waits indefinitely for callbacks or a blocked writer.
  `New` now bounds the complete launch/initialize with 15 seconds; `Close` uses
  a five-second caller deadline and finite notification grace. Uncooperative Go
  callbacks stay reported as pending. Close/failure preserves the first cause,
  partial evidence, local observer cancellation and required raw terminal fences.

- Bound SDK-owned notification retention by configurable finite count and byte
  budgets (default 65,536 representation slots and 64 MiB), including callback
  delivery, attachment/replay, Exact Run history/projections and diagnostics.
  Exhaustion returns `ErrNotificationRetention` with partial facts and cannot
  become a later successful terminal. Histories remain readable and charged
  until unreachable; ordinary consumer progress releases callback/buffer space.
  This replaces previously unbounded history/attachment retention.

- Add `ClientOptions.Env` for copied, process-scoped environment replacement.
  Nil preserves inheritance; omitted keys are unset, `KEY=` is present-empty,
  and duplicate keys use the last value with native platform name matching.
  Invalid entries fail without disclosing environment values. Concurrent clients
  can use separate homes and credentials without mutating the parent environment.

- Opt-in required raw notification subscriptions with lossless JSON observation,
  source-schema validation for the explicitly enabled internal raw-item extension,
  ordered shared dispatch and Exact Run terminal fencing. Unsupported subscriptions,
  malformed required payloads, callback failures and queue overflow fail explicitly
  with secret-safe raw-observer diagnostics. Unsubscribed unknown events retain
  their existing ignore policy; fixture evidence does not claim runtime emission.

#### Fixed

- Protect the six handwritten JSON-RPC envelope exemptions against transitive
  referenced-schema drift and removal. Keep the existing strict transport
  acceptance contract; generated child-decoder success cannot certify an
  unchanged handwritten consumer.

- Stream source-derived protocol obligations through a separately compiled Go
  decoder and compare observations directly. Verify the current reachable graph
  for implementation changes too, retain attributable bounded failure evidence,
  and remove version-pinned corpus snapshots and per-probe test-event transport.

- Request only Contents write for Auto-forward's final fast-forward to an
  already-published exact head, retaining workflow scope for workflow-changing
  candidate publication and reporting secret-safe token failure categories.

- Ignore unrelated native goal broadcasts while preserving owned-goal identity
  and lifecycle checks. Accept internal exec retries only after a valid completed
  turn and zero process exit, retaining bounded intermediate-error diagnostics.

- Keep App Server response routing live while terminal notification callbacks
  synchronously issue typed RPCs on the same client. Ordered callback delivery
  still fences Exact Run completion, including pending attachment replay;
  later frames cannot rewrite an admitted terminal snapshot. Extend the existing
  persistent-thread live story with callback `Threads.Read` and release-barrier
  assertions, without adding model turns.

- Return JSON-RPC `-32601` for unavailable server-request methods and retain
  `-32602` for malformed known requests, without guessing run ownership or
  changing recovery behavior. Document tool-failure observation and the selected
  Codex 0.162.1 notification-based turn completion contract, including caller-local
  wait deadlines and no inline-terminal fallback.

- Deliver direct typed-facade turn notifications during normal operation without
  waiting for an Exact Run attachment. Bind pending Exact observations to the
  requesting run and reject contradictory thread/turn attribution before it can
  change run history, usage, or terminal results.

- Align protocol verification with strict standalone public record decoding,
  including empty records, while preserving tolerant server observations,
  action-bearing validation, and explicitly open maps and union alternatives.

- Keep protocol Agent execution logs and the final message in runner temporary
  storage so a successful Agent proposal can pass the repository scope check.

- Reuse unchanged pending protocol PRs without regenerating or repeating Agent
  work, and conditionally update owned PRs when the target or base advances.

- Detect candidate incompatibility with handwritten SDK source and tests before
  applying protocol upgrades, allowing the bounded repair pass to handle it.

- Bind protocol Agent resume to a verified copy of the original complete
  upstream candidate, reject proposals that change sync acceptance or
  publication controls, and publish proven changes from a separate runner.
- Escalate only typed protocol representation and mapping incompatibilities to
  the optional Agent; source and environment errors fail without that pass.
- Classify `thread/items/list`, `thread/revert`, and `thread/turns/list` from
  the exact upstream stable schema, so runtime stability gates and generated
  experimental field metadata reflect their current wire status.
- Stop `Client.Close()` from waiting forever when notification dispatch is
  blocked on Exact Run evidence after a pre-turn application-level request
  failure.

### Repository

- Use `gpt-6.1-sol/high` for formal upstream repair. Resolve and record the
  official latest Codex CLI per execution for repair and Live, independently
  of the exact schema baseline; the Live model remains `gpt-6-sol/high`.

- Use `AZURE_OPENAI_API_KEY` and `CODEX_RESPONSES_API_ENDPOINT` directly for live
  tests and readiness, with a fixture-owned provider label and shared local/CI
  direct API base configuration. Preserve the existing secrets, model, transport,
  integration stories, and required checks.
- Add manual and weekly `LLM readiness` probes for the configured Responses
  API using the existing repository secrets and `gpt-6-sol/high`.
- Require the real Codex migration suite alongside source/generated/provenance
  checks. Select the official runtime from the tested baseline, configure a
  direct native provider connection using isolated state, and share one
  uncached local/CI command.
  Reuse existing provider secrets and reject missing, skipped, cancelled, or
  failed live results.
- Normalize surrounding provider credential whitespace for the live suite and
  readiness probe, reject embedded whitespace before transport, and explicitly
  configure Bearer authentication with HTTP/SSE transport.
- Replace the composed migration smoke and separately configured direct smoke
  with one canonical live suite: validate schema-constrained integer `number=7`,
  then prove persistent Start, a new completed Resume turn, a distinct Fork,
  and completed new work on the returned fork identity. Preserve the prior
  identity/usable-response assertions and general absent/present-empty SDK
  semantics; archive owned persistent threads on success and failure.

- Document the accepted scenario-based live Codex contract: official runtime
  selected by the synced baseline, direct native provider configuration, Linux,
  `gpt-6-sol/high`, and a relevant-change hard gate. Define the initial
  structured-call and thread-continuation stories and explicit scenario
  retirement disclosure. This entry records design/documentation work;
  runtime/gate and scenario implementation are recorded above.
- Correct the unreleased verification description below to match the existing
  exact-PR-head source and workflow contract, not a synthetic merge candidate.
- Verify protocol PR baselines by rebuilding schema, classification, and
  generated Go from the exact upstream commit in an isolated validation run.
- Make a failed Live Codex smoke report native, secret-safe turn-failure
  facts already present on typed errors and Exact Run observations,
  without logging raw turn error messages.
- Pin the Live Codex smoke `thread/start` to one workflow provider
  identity and reject another observed provider before turn execution.

- Keep GitHub Actions scoped to repository source correctness. Version
  selection, tagging, and public distribution are outside the repository
  Actions control plane.
- Collapse Codex protocol sync onto one Go-native owner for target resolution,
  policy, generate, compare, apply, and check.
- Replace generated protocol proof artifacts with a native regenerate-and-compare
  check.
- Required PR verification checks the exact current PR head instead of
  GitHub's synthetic merge candidate.
- Raise the root Go floor to 1.26.8 and require `golang.org/x/mod v0.41.0`.
- Align GitHub repository description, tag-creation authority, and security
  reporting with the one root-module `vX.Y.Z` release.

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
