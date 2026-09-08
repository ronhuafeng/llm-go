# Verification

The repository has one ordinary pre-merge command:

```sh
./scripts/verify.sh
```

Run it locally before opening a pull request. `PR verification` first runs a
plain public-module test pass on Go 1.23, then runs the same script on the
current Go toolchain.

The script provides two deterministic proof layers:

1. **Semantic and architecture tests** — repository ownership/import rules,
   package unit tests, vet, race tests, and the exact provider/neutral evidence
   invariants owned by each module.
2. **Executable composition examples** — Go `Example...` functions are normal
   package tests. The three-layer fake canary composes `llmkit`, the Codex
   adapter, and `codexsdk` from current source without requiring credentials or
   network access.

Public modules are also tested with `GOWORK=off`, so module tests do not pass
only because the repository workspace repairs dependency resolution. The
workspace canary is separate because its purpose is current-source composition.

This verification deliberately does **not** model release state, mirror public
API inventories, compile README Markdown, probe unpublished module versions, or
produce custom evidence/authorization artifacts. Git source, Go tests, module
`go.mod` files, and immutable tags are the authorities for those facts.

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
