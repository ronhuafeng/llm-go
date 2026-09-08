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

## Optional real Codex smoke

Real provider availability is a third, optional layer. It is not a required PR
check because credentials, service availability, quotas, CLI versions, and
model behavior are external observations rather than deterministic semantic
proofs.

Locally, install and authenticate the Codex CLI, then run:

```sh
LLMGO_LIVE_CODEX=1 \
go test ./internal/tools/integration -run '^TestLiveCodexSmoke$' -count=1 -v
```

Set `LLMGO_LIVE_CODEX_MODEL` only when you intentionally want to pin a model;
otherwise the app-server uses its configured default.

GitHub Actions exposes the same test through the manually dispatched
`Live Codex smoke` workflow. The workflow reuses the repository's existing
`AZURE_OPENAI_API_KEY` and `CODEX_RESPONSES_API_ENDPOINT` through a local
`codex-responses-api-proxy`, matching the authentication path used by upstream
protocol sync. Those secrets are scoped only to proxy startup. The real
`codex app-server` runs in a later step with an isolated `CODEX_HOME` whose
custom Responses provider points at localhost, so the app-server process does
not inherit either credential.

No additional live-smoke Environment or secret is required. The workflow is
manual, runs only from `main`, and never becomes a required pull-request gate.
