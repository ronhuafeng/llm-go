# Verification

This document defines the repository verification contract.

Verification is layered. Native Go code owns behavioral correctness. GitHub
Actions projects those proofs onto immutable Git identities and combines the
results through required checks. A workflow must not invent a second behavioral
authority.

The supported Go floor is the root [`go.mod`](../go.mod); see
[`SUPPORT.md`](../SUPPORT.md).

## Proof identities

For pull requests, keep these identities distinct:

- **H (head)** is the exact current PR head commit.
- **I (integration revision)** is the GitHub revision used to test the PR with
  its current base. On `pull_request`, this is `github.sha`; on
  `merge_group`, it is the queue candidate.
- **U (upstream)** is the exact ref/kind/commit recorded by H's checked-in Codex
  baseline metadata.

A protocol PR is acceptable only when the repository is correct at I, generated
source reproduces at I, and H's protocol provenance is freshly reproducible from
U. None of these proofs substitutes for another.

## Proof levels

Use the smallest native proof that covers the changed owner:

| Scope | Local proof | GitHub proof | Meaning |
| --- | --- | --- | --- |
| `llmkit` | `go vet ./llmkit/...` and `go test -race ./llmkit/...` | `Verify llmkit` | provider-neutral typed inference |
| `codexsdk` | `go vet ./codexsdk/...`, `go test -race ./codexsdk/...`, generated check | `Verify codexsdk` | SDK/runtime plus generated protocol ownership |
| `llmcaller/codex` | `go vet ./llmcaller/codex/...` and `go test -race ./llmcaller/codex/...` | `Verify Codex adapter` | adapter/schema translation |
| generated protocol | generated check and isolated package build | `Verify generated protocol artifacts` | deterministic generated-source reproducibility |
| repository | root commands below | `Root source verification` | source correctness at I |
| protocol provenance | exact upstream reconstruction | `Codex protocol provenance` | H is reproducible from U |
| portability | platform vet/tests | `Advisory OS portability` | advisory OS evidence |
| fuzzing | bounded fuzz targets | `Fuzz` | advisory parser/schema robustness |
| vulnerabilities | `govulncheck` | `Go vulnerability scan` | advisory dependency evidence |
| real provider | live integration test | `Live Codex smoke` | external provider composition |

Owner-local workflows are development proofs. They are not extra merge
authorities.

## Normalize before verification

Formatting is construction, not an acceptance proof. Any repository-owned
producer that writes Go source must run `gofmt` before it seals or commits the
candidate. Generators should prefer `go/format` when they own the source bytes.

For manual development, normalize before commit:

```sh
gofmt -w <changed-go-files>
```

CI verifies the committed candidate. It does not repair formatting and then
claim that the original commit passed.

## Root source proof

From the repository root:

```sh
go mod tidy -diff
go vet ./...
go test -race ./...
```

When workflow files change, also validate workflow semantics with:

```sh
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
```

`go mod tidy -diff` remains a proof because dependency closure is committed
state. CI must not silently rewrite it.

Generated protocol reproducibility is separate:

```sh
go run ./codexsdk/internal/cmd/generatedcheck -module-root ./codexsdk
```

The generated check proves that checked-in source inputs reproduce checked-in
generated outputs and that the regenerated protocol package builds in isolation.
It does not prove that those checked-in inputs came from the declared upstream
revision.

## Required PR checks

The intended branch protection contract has three independent required
contexts:

- `Root source verification`;
- `Codex generated reproducibility / Generated reproducibility`;
- `Codex protocol provenance`.

The first two prove GitHub integration revision I. The provenance check proves
H against U when protocol provenance can be affected.

`Codex protocol provenance` always reports a check result. On a pull request it
runs fresh exact reconstruction when the change touches `codexsdk`, the root
Go module identity, or the workflows that define protocol verification. For an
unrelated pull request it completes successfully as not applicable. On
`merge_group`, `push`, and manual PR-verification runs it also reports not
applicable because those events do not define a new PR-head provenance claim.

This shape lets GitHub perform the final logical AND directly. Do not hide one
proof inside another required job.

## Retry and invalidation

Use GitHub's native job retry boundary:

- a transient failure on an unchanged H/I reruns only the failed independent
  job and its native dependants;
- a new PR head creates a new H and therefore a new provenance obligation;
- a base change creates a new I and therefore invalidates integration source and
  generated proofs;
- build/dependency caches may accelerate a proof but never replace its result.

Do not create a proof database or carry a successful verdict across identities.

## Remote proof without a local environment

Push the exact commit to a repository branch and dispatch the smallest matching
native workflow on that branch. Manual native workflows check out
`${{ github.sha }}`, so they prove the selected event revision rather than
re-resolving a moving branch.

A green narrow workflow is development evidence, not permission to skip the
required merge checks.

## Workflow specification boundary

Repository workflow tests protect invariants, not incidental topology. Protect:

- integration checks use the triggering `github.sha`;
- provenance checkout is the exact `github.event.pull_request.head.sha`;
- provenance is read-only and invokes the same native validation-only verifier;
- source, generated, and provenance proofs remain independent;
- repository-owned protocol publication normalizes changed Go before sealing;
- secret/write workflows pin reviewed third-party Actions;
- Agent execution has no repository-write token;
- publication requires deterministic read-only proof and cannot occur from
  validation-only mode;
- failures do not publish.

Do not freeze job count or incidental step order unless the order protects a
real authority or identity boundary.

## Protocol upgrades

Use [`protocol-sync.md`](protocol-sync.md).

The automatic provenance check derives U from H's checked-in baseline, freshly
generates complete/stable schemas and the exact upstream mapping, reconstructs
accepted semantic artifacts in isolation, and compares them with H. Only
`baseline_metadata.generated_at` is excluded as observation time.

The manual `validation_only` protocol-sync entrypoint remains a diagnostic
projection of the same native verifier. It performs the same exact reconstruction
for the selected repository revision but is not a merge authority.

## Non-gating evidence

Advisory portability, fuzzing, vulnerability scans, and live provider smoke add
evidence but are not required merge gates unless repository policy explicitly
changes.
