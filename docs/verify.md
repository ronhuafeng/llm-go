# Repository verification

Pull-request verification is owned by `repoctl` in `internal/tools`. GitHub
Actions only wires jobs.

`repoctl affected` maps changed paths to the module registry and closes over
`go.mod` requirements. A root change affects every public module. A tools-only
change stays in `internal/tools`.

For each affected public module, CI runs `repoctl verify-module` three times
with `GOWORK=off`:

- `minimum` — tests on the module's declared Go version
- `current` — format, metadata, vet, tests, API inventory, generator drift
- `race` — race detector

`repoctl verify-checkout` then runs the repository boundary contract, the
three-layer canary, and one isolated consumer per affected public module.
Consumers replace only the module under test. Upstream versions stay those in
that module's `go.mod`.

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
database records, or module zip identity. Those belong to
[`docs/release.md`](release.md).

The required GitHub check is `PR verification`.
