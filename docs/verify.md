# Verification

This document owns repository acceptance. Native programs decide whether
behavior meets the contract; GitHub Actions executes those checks and reports
the result for the candidate being integrated.

## Accepted policy and implementation transition

`PR verification` executes the source/generated/provenance proofs and calls the
required `Live Codex integration / Live scenarios` context. Auto-forward checks
all four contexts for the current H, then rechecks before the write. It uses the
latest GitHub Actions result for each required name; an earlier success cannot
hide a later failure or cancellation. The native
`codexsdk/internal/checkacceptance` policy is shared by pending-sync observation
and both auto-forward checks. It reads every check page, requires exact H and
GitHub Actions origin, and records each required result as missing, pending,
failed, cancelled, or succeeded. The latest `pr-verification.yml` run/attempt
must also have succeeded; a queued attempt with no new check jobs cannot inherit
older successes. Any still-active attempt on H blocks acceptance. Incomplete,
changing, or failed API reads are unknown and stop the operation. A transient
read can be retried once; write responses are never blindly retried.

Cancellation is a failed or blocked observation, never acceptance. The native
goal may repair its candidate and request the same verification entrypoint again;
an unchanged candidate reuses its current CI observation rather than replaying
Live. A started cancelled Live job is not automatically rerun. Missing artifacts
or incomplete diagnostics remain explicit blockers.

[`Live Codex integration`](live-codex.md) owns environment setup and the common
command. #376 supplies the runtime/gate; #377 supplies the structured composed
call and continuous persistent Start/Resume/Fork story. Actual execution and
deployment evidence is recorded in the implementation PRs, not inferred from
this document.

## One acceptance candidate

`H` is the exact current PR head. `B` is current main at integration time.
`U` is the upstream identity recorded in H's Codex baseline. Source, generated,
protocol-provenance, and live results refer to the same H. Non-PR runs use the
triggering `github.sha`.

The accepted integration condition is:

```text
B is an ancestor of H
AND source verification(H) succeeds
AND generated reproducibility(H) succeeds
AND protocol provenance(H, U) succeeds or explicitly reports not applicable
AND live Codex integration(H, recorded run-resolved latest runtime) succeeds or explicitly reports not applicable
AND the existing repository review policy is satisfied
```

Not-applicable is a successful classifier result for unrelated changes, not a
permission to treat missing checks, skipped scenarios, failures, or cancellation
as success. Protocol relevance and live-runtime relevance are different
questions and must not be conflated.

The final integration effect creates no new candidate. Native convergence may
first create and independently verify a replacement exact H under the original
deadline; every new H needs fresh four-check acceptance. The effect remains a normal
non-force fast-forward of main to the verified H. Synthetic merge revisions,
merge-time rebases, squash commits, and merge-queue candidates are not the
acceptance identity. See [`auto-forward.md`](auto-forward.md).

## Proof responsibilities

### Root source verification

Run directly from the root module:

```sh
go mod tidy -diff
go vet ./...
go test -race ./...
```

Workflow semantic validation runs from the same candidate. These checks cover
Go behavior and repository invariants. Ordinary deterministic tests do not
require Codex installation or provider credentials; explicitly enabled live
execution is separate.

### Generated reproducibility

`Codex generated reproducibility / Generated reproducibility` verifies that
checked-in inputs reproduce the generated surface and that the regenerated
package builds in isolation. It is not a substitute for root tests or upstream
source reconstruction.

### Codex protocol provenance

`Codex protocol provenance` reads U from H, freshly reconstructs complete/stable
schema and derived artifacts from that exact source, and compares with H in
isolated state. It is read-only and has no Agent/publication authority.
The same gate also checks accepted-base behavior and target-schema contracts
against the frozen candidate. Contract-grounded checker changes belong to the
same reviewed proposal; source identity and publication controls stay fixed.
Current reachable contracts run even when source schemas are unchanged. An
empty selection or incomplete decoder observations cannot certify implementation
changes. Unrelated PRs complete the context successfully as not applicable.

See [`protocol-sync.md`](protocol-sync.md) for source, generation, and publication
boundaries. A source build for this proof is not the installed runtime fixture
used by the live suite.

### Live Codex integration

The required live context executes the active scenario suite against an official
installed latest release resolved once for the execution, using the canonical Linux/native
provider/real model configuration in [`live-codex.md`](live-codex.md).

The relevance classifier covers code and shared dependencies that can affect the
composed or direct Codex path, live-test selection/configuration, and relevant
workflow/integration controls. All protocol-sync PRs qualify. Unrelated changes
complete the same context successfully as not applicable without model calls.
Use a conservative decision when impact is uncertain, not a narrow directory
list that overlooks shared `llmkit` behavior.

A relevant execution must select and run the active stories, without cached
success, automatic retries, fallback models, zero-test success, or prerequisite
skips. A service failure blocks acceptance even when its cause is external.
Report the cause separately from the gate verdict.

Live failure logs retain only bounded facts. A native error's fixed
`message_signature` reports an observed official message literal or prefix;
it does not prove a Rust error variant, provider attribution, or root cause.
Unrecognized text, oversized messages, and messages containing ASCII control
characters stay `unknown`. Raw messages, URLs, response bodies, and credentials
are not published as diagnostics.

The actual required-context consumer, including Auto-forward, must require this
result. A green legacy smoke, successful parent workflow, or post-merge run alone
is not proof that this gate is installed. Reuse existing GitHub checks; do not
add a new approval or attestation system for this single-author workflow.

## Local development

The Go floor is in [`go.mod`](../go.mod); platform scope is in
[`SUPPORT.md`](../SUPPORT.md). Use narrow tests while developing, then obtain the
full applicable acceptance proofs.

```sh
go vet ./codexsdk/...
go test -race ./codexsdk/...
go vet ./llmcaller/codex/...
go test -race ./llmcaller/codex/...
```

For `llmkit`, run its affected package tests and `go test -race ./llmkit/...`.
The generated checker is owned by `codexsdk/internal/cmd/generatedcheck`.
Live setup and the active suite command are owned by `live-codex.md`.

Producers run `gofmt` before committing or sealing changed Go. Formatting is
construction, not an acceptance proof. `go mod tidy -diff` stays a proof because
CI must not silently rewrite committed dependency closure.

## Failure, staleness, and effects

Changing H invalidates H-bound acceptance. If main moves so it is no longer an
ancestor of H, Auto-forward may reconcile an eligible bound proposal through the
linear rebase and isolated fixed-target repair policy in `auto-forward.md`, within
the original deadline. Every replacement H requires immutable baseline proofs
and fresh four-check acceptance before the normal non-force integration effect.
Unsupported or unprovable conflicts remain blocked.

Concurrency settings may reduce redundant runs but do not establish correctness.
Git rejects non-fast-forward updates. Repository-write credentials remain with
the trusted publication/integration effect, never with proposed code or live
model tests. The live Codex process may receive its scoped provider key as
specified in `.github/AGENTS.md`; that is not repository-write authority.

Keep verification evidence in native test output and workflow results. Distinguish
selected baseline, observed runtime, configured model, scenario result, and
unknown compatibility; do not build a second durable compatibility registry.

## Workflow specification tests

Protect the meaningful invariants: same-candidate verification, exact runtime
selection, direct native provider configuration, relevance classification,
real scenario execution, failure/not-applicable handling, required live-result
consumption, read-only verification, and existing non-force integration/credential boundaries.
Do not freeze incidental job topology, step wording, or the permanent number of
scenario functions.

## Other evidence and releases

[`LLM readiness`](../.github/workflows/llm-readiness.yml) probes the configured
Responses API on manual dispatch and every Wednesday at 03:41 UTC. It directly
uses `AZURE_OPENAI_API_KEY` and `CODEX_RESPONSES_API_ENDPOINT`, requiring an
HTTP(S) API base URL including its API prefix, without `/responses`, with
`gpt-6-sol/high`. Readiness
requires HTTP success and a streamed `response.completed` event without an
error, failed, or incomplete event. Logs contain only bounded status facts;
the response body is temporary. This is provider readiness evidence. Installed
Codex scenarios own the SDK integration guarantee.
Both this probe and the live-suite runner remove surrounding key whitespace and
reject whitespace inside the token before making a request.

Existing portability, fuzzing, and vulnerability checks remain useful additional
evidence unless separately promoted. The accepted live gate is no longer
optional evidence for Codex-affecting changes.

Source acceptance is not a tag or package release. Follow
[`release.md`](release.md), including disclosure of intentional live-scenario
retirement.

## Single-goal verification entrypoint

The native goal invokes only the fixed `verify_upstream` tool. The shared
`protocolupgrade verify` implementation runs local source/generated/offline
provenance checks without host credentials. CI invokes the same gate composition
through `protocolupgrade verify-ci-gate -gate <name>`, adding the existing real
Live environment only in its existing job. Source checks additionally require
actual sandbox and candidate-checker fixtures to execute and pass; a skipped or
zero-test result fails. Full diagnostics are bounded complete JSONL files; tool
responses contain only the path. Exceeding the 16 MiB bound is incomplete, never
a truncated complete result.
