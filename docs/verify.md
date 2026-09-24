# Verification

This document is the current authority for repository verification entrypoints.

Verification is layered. A narrow owner-local proof should be available before a
broad repository or external composition proof. GitHub workflows project these
same checks into a remote environment; they do not become a second behavioral
authority.

The supported Go floor is the root [`go.mod`](../go.mod); see
[`SUPPORT.md`](../SUPPORT.md).

## Proof levels

Use the smallest proof that covers the changed owner:

| Scope | Local proof | GitHub proof | Meaning |
| --- | --- | --- | --- |
| `llmkit` | `go vet ./llmkit/...` and `go test -race ./llmkit/...` | `Verify llmkit` | provider-neutral typed inference |
| `codexsdk` | `go vet ./codexsdk/...`, `go test -race ./codexsdk/...`, generated check | `Verify codexsdk` | SDK/runtime plus generated protocol ownership |
| `llmcaller/codex` | `go vet ./llmcaller/codex/...` and `go test -race ./llmcaller/codex/...` | `Verify Codex adapter` | adapter/schema translation |
| generated protocol | generated check | `Verify generated protocol artifacts` | deterministic generated-source reproducibility |
| repository | root commands below | `PR verification` | required source acceptance |
| upstream Codex target | protocol sync validation | `Codex Upstream Protocol Sync` | exact upstream generation/comparison |
| portability | platform vet/tests | `Advisory OS portability` | advisory OS evidence |
| fuzzing | bounded fuzz targets | `Fuzz` | advisory parser/schema robustness |
| vulnerabilities | `govulncheck` | `Go vulnerability scan` | advisory dependency evidence |
| real provider | live integration test | `Live Codex smoke` | external Codex/provider composition |

Owner-local workflows are read-only development proofs. They are intentionally
not additional merge authorities.

## Root local proof

From the repository root:

```sh
go mod tidy -diff
go vet ./...
go test -race ./...
```

Also require formatting and workflow syntax when repository/workflow files may
have changed:

```sh
test -z "$(git ls-files -z -- '*.go' | xargs -0 gofmt -l)"
git diff --check
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
```

Generated protocol reproducibility is separate:

```sh
go run ./codexsdk/internal/cmd/generatedcheck -module-root ./codexsdk
```

The generated check regenerates owned protocol/SDK files, compares them with the
checked-in outputs, validates baseline identity/path safety, and exits non-zero
on mismatch.

## Remote proof without a local environment

Push the exact commit to a repository branch and dispatch the smallest matching
native workflow on that branch. Manual native workflows check out
`${{ github.sha }}`, so the selected workflow run proves the event's exact
revision rather than re-resolving a moving branch during execution.

Use:

- `Verify llmkit` for `llmkit`-only work;
- `Verify codexsdk` for SDK/protocol/runtime work;
- `Verify Codex adapter` for `llmcaller/codex` work;
- `Verify generated protocol artifacts` when only deterministic generated
  reproducibility is needed;
- `PR verification` for the complete repository proof.

A green narrow workflow is development evidence, not permission to skip the
required merge checks.

## Required PR verification

Branch protection currently requires these check contexts:

- `Root source verification`;
- `Codex generated reproducibility / Generated reproducibility`.

Keep those names stable unless branch protection is deliberately migrated in
the same change.

`PR verification` runs on pull requests, merge queue candidates, pushes to
`main`, and manual dispatch. On `pull_request`, required jobs validate
GitHub's synthetic merge candidate (`github.sha`), not the isolated PR branch
head. `merge_group`, `main` push, and manual runs verify their triggering
revision.

Required source verification covers workflow syntax, Go formatting/whitespace,
`go mod tidy -diff`, `go vet ./...`, and `go test -race ./...`.
Generated reproducibility is a separate required outcome and uses the native
generated checker.

GitHub Actions orchestrates these checks; Go and the checked-in source/schema
own their meaning.

## Workflow specification boundary

Treat workflow structure as executable evidence only where structure protects a
current invariant.

Repository workflow tests should protect properties such as:

- required verification checks out the triggering `github.sha`;
- read-only native verifiers have only `contents: read` permission and consume
  the root `go.mod`;
- generated verification invokes the native generated checker;
- secret/write workflows pin reviewed third-party Actions as required;
- the protocol Agent receives no repository-write token and is invoked at most
  once per run;
- protocol publication requires prior deterministic success and is impossible
  in validation-only mode;
- failures do not publish.

Do not freeze job count, incidental step names/order, or retired workflow
topology when those details protect no independent correctness boundary.

## Protocol upgrades

Use [`protocol-sync.md`](protocol-sync.md).

A protocol PR needs both the normal required repository checks and a fresh
validation-only protocol-sync proof on the same final head. Candidate generation
or an Agent completion message cannot replace either deterministic proof.

## Non-gating evidence

Advisory portability, fuzzing, vulnerability scans, and real Codex smoke provide
additional signals but are not required merge gates unless repository policy is
explicitly changed.

The live Codex smoke proves a small real-provider composition on Linux. It does
not define supported OS scope and does not prove compatibility with every App
Server version.
