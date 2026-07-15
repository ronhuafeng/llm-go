# Three-layer canary

The canary exercises `llmkit -> llmcaller/codex -> codexsdk` through
the SDK's real subprocess JSON-RPC transport and a deterministic fake
app-server. It does not use a workspace, replacement, or local module source.

The committed `go.mod` is the sole owner of the exact upstream versions used
by standalone module tests. Repository checkout verification separately uses
the committed workspace to compose the same three public package identities.

`TestThreeLayerCanaryFast` is the normal CI subset. The full invariant suite is
`LLMCALLER_FULL_CANARY=1 go test . -run '^TestThreeLayerCanary'`. Repository CI
runs the fast seam from the workspace and runs minimum-Go, current-Go, race,
and isolated source-consumer stages with `GOWORK=off` where applicable.

Public-proxy tuple and typed-consumer evidence belongs to the repository release
workflow after an immutable adapter tag exists. Module-local tests do not claim
published-artifact identity.
