# Verification

Use standard Go commands. Supported Go/platform versions are documented in
[`../SUPPORT.md`](../SUPPORT.md) and enforced by each module's `go.mod` and CI.

## Local module checks

For a module whose committed dependencies are already published:

```sh
GOWORK=off go mod tidy -diff
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
```

Run those commands from the affected public module.

For repository integration:

```sh
go test ./internal/tools/integration
```

A pre-v1 source cohort may temporarily name a not-yet-published repository
module version. Required PR verification handles that case with an uncommitted
temporary modfile pointing at current repository source. Public `go.mod` files
remain unchanged.

Current-source composition and published dependency closure are different
checks. The latter is enforced during release; see [`release.md`](release.md).

## Required PR verification

`PR verification` is the merge gate. It covers:

- workflow syntax and Go formatting/whitespace;
- public modules at their supported minimum Go versions;
- owner-local module tests;
- current-source adapter/repository composition without changing committed
  manifests;
- current-toolchain tidy/vet/race/integration checks;
- checked-in `codexsdk` generated-source reproducibility.

GitHub Actions orchestrates these checks; Go owns the module/repository logic.

## Protocol upgrades

Use [`protocol-sync.md`](protocol-sync.md).

## Non-gating checks

Advisory portability, fuzzing, vulnerability scans, post-release resolution
smoke, and real Codex smoke provide additional signals but are not required PR
merge gates unless repository policy explicitly changes.

The live Codex smoke checks current CLI/provider availability and cannot replace
deterministic source verification.
