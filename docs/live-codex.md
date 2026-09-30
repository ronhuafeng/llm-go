# Live Codex integration

This document owns the accepted real-runtime testing design. It describes what
we test against installed Codex, not a test suite for Codex Core itself.

## Implementation transition

The design is accepted; its implementation remains tracked by
[#376](https://github.com/ronhuafeng/llm-go/issues/376) and
[#377](https://github.com/ronhuafeng/llm-go/issues/377).

At the inspected `main` revision
`5e6b45fc4ad17e7ec090eeed19ba1d1cd4b53d36`, the
[legacy live workflow](../.github/workflows/live-codex-smoke.yml) installs latest
Codex, uses a local Responses proxy and `gpt-5.6-luna/medium`, and runs only the
[composed smoke](../internal/tools/integration/live_codex_smoke_test.go).
The [direct smoke](../codexsdk/real_appserver_smoke_test.go) is separately opt-in
and stops after Fork returns an ID. The
[auto-forward workflow](../.github/workflows/auto-forward.yml) checks only the
three existing native proof contexts.

That is a dated implementation observation, not the desired policy below.
Documentation does not activate a gate or prove credentials work. Update this
transition when the implementation and real acceptance evidence exist.

## Runtime and ownership

The production boundary is:

```text
application -> llm-go -> installed official codex
            -> App Server / Codex Core -> Mini -> real model
```

The SDK assumes Codex is installed. A CI job may install an official release to
prepare the fixture, but neither the SDK nor this integration suite builds,
embeds, or modifies Codex Core. Source builds used by protocol-provenance
verification remain a separate concern.

Go tests own the integration assertions. Codex owns its session internals,
model interaction, and native request headers. Mini owns ingress authorization,
credential binding, and credit policy. This suite does not independently
certify Mini or every official OpenAI endpoint.

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
llmadapter -> llmcaller/codex -> codexsdk -> installed Codex -> Mini -> real model
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

Configure native Codex directly to Mini. The inputs are a Mini API **base URL**
and a Mini key; the base URL is not a full `/responses` request endpoint. The
configuration shape is:

```toml
model = "gpt-6-sol"
model_reasoning_effort = "high"
model_provider = "mini"

[model_providers.mini]
name = "Mini"
base_url = "<Mini API base URL, including its API prefix>"
env_key = "MINI_CODEX_API_KEY"
wire_api = "responses"
```

`MINI_CODEX_API_KEY` names an environment variable, not a literal secret in this
file. The planned setup accepts `MINI_CODEX_BASE_URL` and
`MINI_CODEX_API_KEY` and produces this configuration in an isolated `CODEX_HOME`.
The implementation must keep CI and local scenario setup aligned without
creating a general configuration framework. These names do not assert that
corresponding repository secrets already exist.

Do not install a standalone Responses proxy, inject imitation Codex headers, or
silently fall back to ambient user authentication or a different provider.
Codex supplies its own version-dependent headers. Mini integration needs a valid
Mini credential, not a separately provisioned OpenAI API key in the test process.

The Go suite runs once with uncached test execution and no automatic retry or
repeat-until-green wrapper. Do not build an llm-go provider retry controller;
Mini/Codex retain their own provider-side behavior. Do not equate one test
attempt with a guarantee about every downstream HTTP attempt.

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
Mini credential is intentionally available to native Codex.

## What remains outside the first suite

Deterministic Go tests continue to cover ordering, races, framing, error
propagation, cancellation mechanics, and schema fidelity. Live tests do not
replace them, and an Agent's success message does not replace assertions.

The first live suite does not add approval/tool execution, MCP, interrupt,
WebSocket transports, a platform/model/version matrix, or upstream Rust-test
mirroring. New real use can justify new stories later. The implementation must
publish a single explicit local/CI command for the active suite rather than
leaving overlapping opt-in smoke entry points as competing authorities.
