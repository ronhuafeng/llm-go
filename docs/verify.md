# Verification

Use standard Go commands from the repository root. The supported Go floor is
the root [`go.mod`](../go.mod); see [`../SUPPORT.md`](../SUPPORT.md).

```sh
go mod tidy -diff
go vet ./...
go test -race ./...
```

Generated protocol reproducibility is a separate deterministic check:

```sh
go run ./codexsdk/internal/cmd/generatedproof -module-root ./codexsdk
```

## Required PR verification

`PR verification` is the merge gate. It has two outcomes:

- root source verification: workflow syntax, Go formatting/whitespace,
  `go mod tidy -diff`, `go vet ./...`, and `go test -race ./...`;
- checked-in `codexsdk` generated-source reproducibility.

On `pull_request`, both outcomes validate GitHub's synthetic merge candidate
(`github.sha`), not the isolated PR branch head. `merge_group` and `main` push
runs verify the same triggering revision.

GitHub Actions orchestrates these checks; Go owns the module/repository logic.

## Protocol upgrades

Use [`protocol-sync.md`](protocol-sync.md).

## Non-gating checks

Advisory portability, fuzzing, vulnerability scans, post-release resolution
smoke, and real Codex smoke provide additional signals but are not required PR
merge gates unless repository policy explicitly changes.

The live Codex smoke checks current CLI/provider availability and cannot replace
deterministic source verification.
