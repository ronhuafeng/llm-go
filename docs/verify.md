# Verification

Use standard Go commands. The repository intentionally has no general task
runner.

## Local module checks

Published modules support Go 1.23. For a module whose committed dependencies are
already published:

```sh
GOWORK=off go mod tidy -diff
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
```

Run those commands from `llmkit`, `codexsdk`, or `llmcaller/codex` as
appropriate.

The adapter may temporarily name an unpublished next `llmkit`/`codexsdk` version
during a pre-v1 source cohort. Required PR verification handles that case with a
temporary modfile pointing at repository current source. Committed public module
manifests stay unchanged and contain no `replace` or `exclude` directives.

Repository tools require Go 1.25. From the repository root:

```sh
go test ./internal/tools/integration
```

## Required PR verification

`PR verification` is the merge gate. It checks:

- workflow syntax and Go formatting/whitespace;
- each public module at its minimum Go version;
- current-source adapter/repository composition without modifying committed
  manifests;
- current-toolchain `tidy`, `vet`, race tests, and repository integration;
- checked-in `codexsdk` generated protocol/source reproducibility.

GitHub Actions orchestrates these checks; Go owns the repository/module logic.

Current-source composition does **not** prove that a dependency version is
published. Published dependency closure is checked during release; see
[`release.md`](release.md).

## Protocol upgrades

Codex App Server protocol upgrades follow [`protocol-sync.md`](protocol-sync.md).
The final architecture is one run from upstream comparison through deterministic
Go checks to a protected PR. A failed run is retried from upstream source, not
reconstructed from historical CI state.

## Non-gating checks

The repository also runs advisory portability, fuzzing, vulnerability scans,
post-release resolution smoke, and a real Codex smoke. These provide additional
signals but are not required PR merge gates unless a repository rule explicitly
changes that policy.

The live Codex smoke exercises current provider/CLI availability and therefore
cannot replace deterministic source tests. It runs only on trusted repository
paths with provider credentials isolated from checked-out development code.
