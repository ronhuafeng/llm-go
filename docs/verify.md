# Verification

Ordinary repository verification intentionally has no repository-specific task
runner. Semantic proofs live in Go tests and executable examples; GitHub Actions
shows the small amount of orchestration needed to run standard Go tools.

For a public module, the local pattern is:

```sh
cd llmkit # or codexsdk or llmcaller/codex
GOWORK=off go mod tidy -diff
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
```

Repository tools use the same standalone pattern from `internal/tools`. To prove
current-source composition through the workspace, run from the repository root:

```sh
go test -race ./internal/tools/integration
```

Required pull-request verification runs on Linux only. The scheduled or
manual `Advisory OS portability` workflow additionally runs the same
standalone public-module `go vet`/`go test` commands on macOS and Windows,
plus current-source integration tests. That matrix is not a required merge
gate. See [`SUPPORT.md`](../SUPPORT.md).

`PR verification` makes the complete ordinary gate explicit in its workflow:

1. test each public module with Go 1.23 and `GOWORK=off`;
2. require tracked Go files to be `gofmt`-clean and reject whitespace errors;
3. on the current Go toolchain, run `go mod tidy -diff`, `go vet ./...`, and
   `go test -race ./...` for `llmkit`, `codexsdk`, `llmcaller/codex`, and
   `internal/tools`, with `GOWORK=off`; and
4. run the repository integration package with the workspace enabled so the
   three semantic owners are composed from current source.

Go `Example...` functions and the three-layer fake canary are ordinary tests;
they are not invoked again through a second verification framework. Codex SDK
checked-in protocol artifacts, generated facade semantics, baseline provenance,
and baseline hygiene are likewise protected by owner-local Go tests.

The Python and shell programs under `codexsdk/scripts` belong to the exceptional
upstream synchronization control plane. They may acquire or classify upstream
schemas, construct sync candidates, validate a requested upstream target, and
publish a sync PR. They are not an ordinary correctness gate for unrelated
library changes. The upstream-sync workflow remains responsible for exercising
that tooling when it performs a protocol synchronization.

Scheduled Dependabot updates and the manual/scheduled `Go vulnerability scan`
workflow surface dependency and Action maintenance. They are not required
pull-request checks and do not own deterministic source correctness.
Workflows that receive write credentials or provider/release secrets pin
third-party Actions to immutable commit SHAs. Read-only workflows without
those secrets keep moving major-version tags so Dependabot can update them
without secret-bearing pin churn.

Owner-local Go fuzz targets exercise schema and protocol parser boundaries.
Ordinary `go test` runs only their seed corpus. The scheduled/manual `Fuzz`
workflow may run bounded `go test -fuzz=... -fuzztime=...` steps; it is not a
required pull-request check.

Ordinary verification deliberately does **not** model release state, mirror
public API inventories, compile README Markdown, probe unpublished module
versions, produce custom evidence/authorization artifacts, or wrap standard Go
commands in another repository task runner. Git source, owner-local Go tests,
module `go.mod` files, and immutable tags are the authorities for those facts.

## Real Codex smoke

Real provider availability is a third, non-gating layer. It is not a required
pull-request check because credentials, service availability, quotas, CLI
versions, and model behavior are external observations rather than
deterministic semantic proofs.

Locally, install and authenticate the Codex CLI, then run:

```sh
LLMGO_LIVE_CODEX=1 \
go test ./internal/tools/integration -run '^TestLiveCodexSmoke$' -count=1 -v
```

Local runs may set `LLMGO_LIVE_CODEX_MODEL` when a specific model is desired.
That local choice is independent of the hosted smoke policy.

GitHub Actions keeps `Live Codex smoke` continuously active in three ways:

- after every push to `main`;
- after every successful `PR verification` for a same-repository PR head whose
  triggering actor is the repository owner; and
- through manual `workflow_dispatch` from `main`.

The development path is chained from the deterministic PR check rather than
running a secret-bearing workflow directly from an arbitrary branch. The live
workflow is defined on the default branch, checks out the exact verified PR
head SHA, uses read-only repository permissions, disables dependency caching,
and does not run for fork/untrusted PR heads. Repeated pushes on the same source
branch cancel the superseded smoke so cost remains bounded.

The hosted smoke is intentionally fixed to `gpt-5.6-luna` with `medium`
reasoning. Those settings exist only in `live-codex-smoke.yml`; they do not
change upstream protocol-sync Codex settings or any library/runtime default.

The workflow reuses the repository's existing `AZURE_OPENAI_API_KEY` and
`CODEX_RESPONSES_API_ENDPOINT` through a local `codex-responses-api-proxy`,
matching the authentication path used by upstream protocol sync. Those secrets
are scoped only to proxy startup. The real `codex app-server` runs later with
an isolated `CODEX_HOME` whose custom Responses provider points at localhost,
so checked-out development code and the app-server process do not inherit
either credential.

No additional live-smoke Environment or secret is required.
