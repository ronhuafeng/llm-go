# Live Codex integration

This document owns the accepted real-runtime testing design. It describes what
we test against installed Codex, not a test suite for Codex Core itself.

## Current implementation

The runtime fixture and required-check consumer from
[#376](https://github.com/ronhuafeng/llm-go/issues/376) were integrated through
[PR #381](https://github.com/ronhuafeng/llm-go/pull/381):
[PR verification](../.github/workflows/pr-verification.yml) calls the
[live workflow](../.github/workflows/live-codex-smoke.yml), and
[Auto-forward](../.github/workflows/auto-forward.yml) requires
`Live Codex integration / Live scenarios` alongside the three native contexts.
Actual current-head workflow results and integration readback belong to the
implementation PR; this source description alone is not deployment evidence.

The active [scenario suite](../internal/tools/integration/live_codex_test.go)
implements both stories below for
[#377](https://github.com/ronhuafeng/llm-go/issues/377). It validates the composed
integer result and completes work after persistent Start, Resume, and Fork.
The old composed migration smoke and independently configured direct smoke are
consolidated into this suite, preserving their identity and usable-response
assertions. Actual execution and integration evidence belongs to the
implementation PR; this source description alone does not prove a live result.

## Run the current suite

From the repository root, supply `AZURE_OPENAI_API_KEY` and
`CODEX_RESPONSES_API_ENDPOINT` in the environment, then:

```sh
npm install --global "@openai/codex@$(go run ./internal/tools/cmd/livecodex version)"
go run ./internal/tools/cmd/livecodex run
```

The command selects the baseline release, runs Go tests once without caching,
and rejects zero selected tests, skips, failure, or incomplete execution. Tests
verify the installed version and create temporary Codex home/workspace state.
Normal deterministic tests remain credential-free and do not enable live work.
The enabled suite runs one structured call and three turns in the continuous
thread story. It uses separate temporary fixture state for each story and
archives owned persistent threads before closing their client, including
failure paths. Forked work uses the public Resume composition with the returned
fork identity; the fork is persistent so that this continuation and cleanup
are supported. No separate opt-in runtime/model configuration remains.

CI consumes the existing repository secrets with those same names directly.
Local and CI setup use the shared fixture with the same inputs. The endpoint
must be an HTTP(S) API base URL including its API prefix, without `/responses`.
The fixture uses that supplied base directly, trimming surrounding whitespace
and trailing base slashes. URLs with credentials, query parameters, or fragments
fail setup. Neither path prints the key or endpoint. Configuration never persists
the key.

The shared runner trims surrounding whitespace from the API key before passing
it to the test process. A blank key or whitespace inside the token fails setup;
the fixture accepts only the normalized credential. The readiness probe applies
the same policy before transport. This handles accidental paste padding without
changing the credential itself or persisting it.

A manual not-applicable classifier check may dispatch `live-codex-smoke.yml`
with `base_sha` equal to the dispatched revision. This proves classification
without executing the model; it does not prove either live story.

## Runtime and ownership

The production boundary is:

```text
application -> llm-go -> installed official codex
            -> App Server / Codex Core -> configured Responses-compatible endpoint
            -> real model
```

The SDK assumes Codex is installed. A CI job may install an official release to
prepare the fixture, but neither the SDK nor this integration suite builds,
embeds, or modifies Codex Core. Source builds used by protocol-provenance
verification remain a separate concern.

Go tests own the integration assertions. Codex owns its session internals,
model interaction, and native request headers. The configured provider owns
authorization, credential binding, and billing policy. This suite proves only
the active integration scenarios in their configured environment.

## Scenario-based guarantee

The active live scenarios and their assertions are the source of truth for
live-tested behavior. Tests are integration stories, not a one-RPC/one-test
catalogue. Generated protocol availability does not imply exhaustive live
coverage, and a declared but skipped or failing scenario supplies no positive
evidence.

Adding a scenario expands the live guarantee. Retiring or materially weakening
one reduces it and must be explicitly explained in the PR and recorded in the
changelog/release notes. Consolidating tests without losing asserted behavior is
not retirement. Do not maintain a second capability registry, upstream-test
mapping, compatibility ledger, or model matrix. Existing protocol-generator
manifests and coverage files serve wire generation, not this live guarantee.

The initial suite contains the following two stories. This is not a permanent
restriction against adding future scenarios.

## Story A: structured composed call

Exercise the recommended public path:

```text
llmadapter -> llmcaller/codex -> codexsdk -> installed Codex
           -> configured Responses-compatible endpoint -> real model
```

Ask a simple closed task such as `What is 3 + 4? Return the result according to
the output schema.` Use a schema with this meaning:

```json
{
  "type": "object",
  "properties": {"number": {"type": "integer"}},
  "required": ["number"],
  "additionalProperties": false
}
```

The story must observe a completed new-thread/new-turn call, validate the
schema-constrained output, successfully decode the typed result, and assert
`number == 7`. Compare the parsed value, not JSON whitespace or key formatting.
Do not weaken this to non-empty text or struct decoding that ignores violated
schema constraints. A single positive case demonstrates this scenario, not
universal model adherence to all schemas.

This already traverses ordinary Start. Do not add a separate live story solely
to repeat direct Start.

## Story B: persistent thread continuation

Use direct `codexsdk` in one continuous integration story:

```text
Start persistent thread + completed turn
  -> Resume the same thread + completed new turn
  -> Fork the thread
  -> completed new turn on the forked thread
```

Check observed thread/turn identities, successful completion, and a usable
response for each requested piece of new work. Resume must continue the intended
thread; Fork must yield a distinct thread, and subsequent work must actually
use that returned identity. A Fork response containing only an ID is not enough.

Independent small arithmetic prompts are suitable. Do not depend on recalling
an earlier token, inherited conversation content, branch-memory fidelity, or
particular natural-language phrasing. Those are not the SDK integration
invariant. Explicit structured-output steps may validate their schema/result;
otherwise do not impose exact prose comparisons.

Response assertions belong to the scenario. They must not change the SDK's
general distinction between absent final output and observed present-empty
output. Use existing public operations rather than inventing a test-only SDK
lifecycle API.

## One canonical environment

The runtime version comes from the tested revision's
[`baseline_metadata.json`](../codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json).
Install the corresponding exact official release and check the observed CLI
version. A protocol-sync PR uses its candidate baseline, not main's older
baseline. Do not use `@latest`, silently substitute a different release, or
maintain another known-good version file. If the baseline cannot identify an
installable matching release, fail the setup rather than testing another one.

Use Linux and the single configured model `gpt-6-sol` with reasoning effort
`high`. The model can be explicitly changed later when operational needs
justify it; there is no automatic latest-model selection or model matrix.
Configured identity is not proof of the model that served every operation.

Configure native Codex directly to the Responses-compatible endpoint. The only
provider inputs are `CODEX_RESPONSES_API_ENDPOINT` and `AZURE_OPENAI_API_KEY`.
The shared fixture writes the supplied API **base URL**, including its API
prefix, into native configuration. Its provider label belongs to the fixture:

```toml
model = "gpt-6-sol"
model_reasoning_effort = "high"
model_provider = "llm-go-live"

[model_providers.llm-go-live]
name = "llm-go live provider"
base_url = "<API base URL, including its API prefix, without /responses>"
env_key = "AZURE_OPENAI_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

`env_key` references `AZURE_OPENAI_API_KEY`; the credential stays in the child
environment. The shared Go fixture produces this configuration in an isolated
`CODEX_HOME` for both CI and local scenario setup.

The provider configuration uses a Bearer API key and HTTP/SSE Responses
transport. The fixture explicitly disables OpenAI/ChatGPT login authentication
and Responses WebSocket transport for this provider.

Do not install a standalone Responses proxy, inject imitation Codex headers, or
silently fall back to ambient user authentication or a different provider.
Codex supplies its own version-dependent headers and reads the configured
credential environment key.

The Go suite runs once with uncached test execution and no automatic retry or
repeat-until-green wrapper. Do not build an llm-go provider retry controller;
Codex and the configured provider retain their own transport behavior. Do not
equate one test attempt with a guarantee about every downstream HTTP attempt.

## Required gate

[Verification](verify.md) owns PR acceptance. The accepted live check is a hard
gate for changes that can affect the Codex path: SDK/runtime/protocol code,
adapter code, the neutral schema/call code used by the composed story, live tests
and fixtures, shared module dependencies, and relevant workflow/acceptance
configuration. Protocol-sync PRs are included even when only runtime provenance
advances. Pure unrelated documentation or other unrelated changes should not
spend a model call; uncertain relevance should be treated conservatively.

A relevant PR must actually execute the active scenarios successfully. Missing
Codex, wrong version, missing credentials, timeout, failed assertions, cancelled
runs, unexpected skips, and zero selected tests are not PASS. Unrelated PRs
complete the same required context successfully with an explicit not-applicable
result. Missing or skipped workflow execution is not a substitute for that
classification.

Use the repository's existing current-head checks and integration mechanism.
The single-author controlled same-repository workflow includes the protocol-sync
bot; no new fork approval system or parallel proof authority is needed. The
integrator must require the live result before advancing main. A successful
post-merge run is useful evidence, but cannot serve as a pre-merge gate.

A provider outage is still a blocked gate, not necessarily an SDK bug. Classify
the observed failure honestly without converting it to success or silently
retiring the scenario. Human reruns remain explicit operations; the test runner
does not decide to retry.

## Fixtures and evidence

Use isolated temporary `CODEX_HOME` and task workspace state. Apply existing
application-owned test policy; do not operate on the developer's real threads,
config, or repository worktree as the model's task. Clean up owned threads,
clients, and temporary state, including failure paths.

Keep normal Go test output and CI summaries sufficient to identify the scenario,
stage, tested repository revision, selected baseline, observed CLI/runtime
identity, configured model/reasoning, and typed failure facts. Keep requested
and observed facts separate. `RuntimeCompatibilityUnknown` remains unchanged;
a passing scenario does not certify every method or model combination.

Never print keys, authorization headers, auth files, full environment dumps, or
raw private transcripts. Do not upload an entire `CODEX_HOME`. Basic hygiene
and repository-write credential isolation remain necessary even though the
provider credential is intentionally available to native Codex.

## What remains outside the first suite

Deterministic Go tests continue to cover ordering, races, framing, error
propagation, cancellation mechanics, and schema fidelity. Live tests do not
replace them, and an Agent's success message does not replace assertions.

The first live suite does not add approval/tool execution, MCP, interrupt,
WebSocket transports, a platform/model/version matrix, or upstream Rust-test
mirroring. New real use can justify new stories later. The shared command above
owns required execution of both stories.
