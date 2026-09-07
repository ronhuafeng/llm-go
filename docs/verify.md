# Repository verification

Pull-request verification is owned by `repoctl` in `internal/tools`. GitHub
Actions only wires jobs.

`repoctl affected` maps changed paths to the module registry and closes over
`go.mod` requirements. A root change affects every public module. A tools-only
change stays in `internal/tools`.

For each affected public module, CI runs `repoctl verify-module` three times
with `GOWORK=off`:

- `minimum` — tests on the module's declared Go version
- `current` — format, metadata, vet, tests, derived public API, generator drift
- `race` — race detector

`repoctl verify-checkout` then runs the repository boundary contract, tool
tests under `cmd` and `internal`, the three-layer canary, and one isolated
consumer per affected public module. Consumers replace only the module under
test. Upstream versions stay those in that module's `go.mod`. When llmkit is
affected, checkout also compiles current `example_test.go` files against the
README install version with `GOWORK=off` and no `replace`. That is
consumer-facing documentation evidence, not proxy zip identity.
Integration tests belong to the workspace canary, not the GOWORK=off
tool-test sweep.

The canary copies `go.work` ephemerally and must not write `go.work.sum`.
Minimum-Go jobs build `repoctl` with current Go, then switch the module to its
minimum.

The ordinary checkout gate uses the fast canary. Extended transport cases:

```sh
LLMGO_FULL_CANARY=1 \
go test ./internal/tools/integration -run '^TestThreeLayerCanaryFull$' -count=1 -v
```

## Evidence boundary

JSON evidence is `module_source` or `checkout_source`. Checkout evidence is
not published-artifact proof: it does not establish proxy identity, checksum
database records, module zip identity, README self-version truth, or
documented-example compilation against the published zip. Those belong to
[`docs/release.md`](release.md).

The required GitHub check is `PR verification`.
