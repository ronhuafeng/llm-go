# Verification

Ordinary repository verification intentionally has no repository-specific task
runner. Semantic proofs live in Go tests and executable examples; GitHub Actions
runs standard Go tools and a version-pinned Actions workflow validator.

Published modules and repository tooling intentionally have different minimum
Go versions. `llmkit`, `codexsdk`, and `llmcaller/codex` support Go 1.23. The
root workspace and `internal/tools` require Go 1.25. That repository-tooling
baseline is not a stronger requirement for consumers of the published modules.

For a public module whose committed dependencies are already published, the
standalone local pattern is:

```sh
cd llmkit # or codexsdk or llmcaller/codex
GOWORK=off go mod tidy -diff
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
```

A pre-v1 source cohort may intentionally commit a downstream `go.mod` that names
the next upstream module version before that tag exists. During that source
cohort, ordinary PR verification proves the downstream against repository
current source with a temporary, uncommitted `-modfile` replacement. The
committed module manifest remains unchanged and contains no `replace` or
`exclude`. This is a current-source proof only; it does not claim the committed
dependency version is already published or independently resolvable.

For the current Codex adapter cohort, the canonical verification shape is the
same one used by `PR verification`: copy `go.mod`/`go.sum` to temporary verify
files, add only the required repository-source replacement to the temporary
modfile, and run Go commands with `GOWORK=off` plus that `-modfile`. Do not add a
committed replacement merely to make an unpublished dependency resolve.

Repository tools require Go 1.25 and use the same standalone pattern from
`internal/tools`. To prove repository-minimum current-source composition through
the workspace, run from the repository root with Go 1.25 or newer:

```sh
go test ./internal/tools/integration
```

Current-stable verification additionally runs the integration package with
`-race`.

Required pull-request verification runs on Linux only. The scheduled or
manual `Advisory OS portability` workflow additionally runs `go vet ./...`
and `go test ./...` for each public module with `GOWORK=off`, plus
current-source `go test ./internal/tools/integration`, on Linux, macOS, and
Windows. That matrix is advisory: it does not run `-race` or `go mod tidy
-diff`, and it is not a required merge gate. See
[`SUPPORT.md`](../SUPPORT.md).

The `Post-release module resolution smoke` workflow is also not a merge
gate and must never be treated as a pre-tag publication check. See
[`docs/release.md`](release.md).

`PR verification` makes the complete ordinary gate explicit in its workflow:

1. validate GitHub Actions workflow syntax and context usage with a
   version-pinned `actionlint` binary;
2. test `llmkit` and `codexsdk` at Go 1.23 with `GOWORK=off` against their
   committed standalone manifests;
3. test `llmcaller/codex` at Go 1.23 against repository current `llmkit` source
   through the temporary verify modfile required by the active pre-v1 source
   cohort, without modifying its committed manifest;
4. test `internal/tools` standalone and current-source workspace composition
   with Go 1.25;
5. require tracked Go files to be `gofmt`-clean and reject whitespace errors;
6. on the current Go toolchain, run `go mod tidy -diff`, `go vet ./...`, and
   `go test -race ./...` for independently resolvable modules; run the Codex
   adapter's `go vet` and `go test -race` through its temporary current-source
   modfile; then run repository integration against current source.

The adapter's temporary source replacement and repository integration proof
answer only whether the checked-out source cohort composes. They do not answer
whether a clean external consumer can resolve a future adapter tag. Published
closure is a release-time proof owned by [`docs/release.md`](release.md), and a
dependent module must not be tagged until every committed dependency version
exists and resolves with `GOWORK=off`.

Go `Example...` functions and the three-layer fake canary are ordinary tests;
they are not invoked again through a second verification framework. Codex SDK
checked-in protocol artifacts, generated facade semantics, baseline provenance,
and baseline hygiene are likewise protected by owner-local Go tests.

The Python and shell programs under `codexsdk/scripts` belong to the exceptional
upstream synchronization control plane. They may acquire or classify upstream
schemas, construct sync candidates, validate a requested upstream target, and
publish a sync PR. They are not an ordinary correctness gate for unrelated
library changes. The upstream-sync workflow is mechanical-first: resolve the
target, generate and apply the schema/protocol surface, and run owner-local Go
proofs before any implementation agent. The agent is invoked only when that
path writes explicit escalation evidence.

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
public API inventories, compile README Markdown, or treat an unpublished module
version as published merely because repository source can replace it. It does
not produce custom evidence/authorization artifacts or wrap standard Go
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

The live smoke installs `@openai/codex@latest` as the system under test so
compatibility stays current. The credential-handling
`@openai/codex-responses-api-proxy` is a separate trust decision: both this
workflow and the Codex runner action pin an explicit npm version in source.
Bump that version in those two install sites after reviewing the proxy
release; do not float `@latest` on the process that receives provider
credentials.

No additional live-smoke Environment or secret is required.
