# Verification

This document defines the repository verification contract.

Native code owns behavioral correctness. GitHub Actions projects those proofs
onto immutable Git identities. Pull-request acceptance and integration use one
candidate identity: the exact current PR head `H`.

The supported Go floor is the root [`go.mod`](../go.mod); see
[`SUPPORT.md`](../SUPPORT.md).

## Auto-forwardable PR

A pull request is **auto-forwardable** when its exact head `H` already contains
the current `main` head `B`, every required proof for H succeeds, and the
repository review policy for H is satisfied.

```text
B ∈ ancestors(H)

AND

Root source verification(H) == success

AND

Generated reproducibility(H) == success

AND

Codex protocol provenance(H,U) == success
```

For PRs that do not affect protocol provenance, the provenance context completes
successfully as not applicable.

Integration does not create a commit. It advances main only by a normal
non-force fast-forward:

```text
main: B ──fast-forward──> H
```

Therefore:

```text
tested commit
=
reviewed commit
=
integrated commit
=
new main commit
=
H
```

See [`auto-forward.md`](auto-forward.md).

## Proof identities

- **H (head)**: the exact current PR head and the only PR acceptance candidate.
- **B (base)**: the current main head at integration time. B is not a second
  candidate; it is an ancestry condition for forwarding H.
- **U (upstream)**: the exact ref/kind/commit recorded by H's checked-in Codex
  baseline metadata when protocol provenance is relevant.

GitHub synthetic merge revisions, merge-queue revisions, squash commits, and
merge-time rebase commits are not repository acceptance identities.

## Required PR checks

Branch protection requires three independent contexts:

- `Root source verification`
- `Codex generated reproducibility / Generated reproducibility`
- `Codex protocol provenance`

On `pull_request`, all three contexts refer to exact H.

### Root source verification(H)

The source proof runs from H:

```sh
go mod tidy -diff
go vet ./...
go test -race ./...
```

Workflow semantic validation is also run from the same H.

### Generated reproducibility(H)

The generated proof checks that H's checked-in generated inputs reproduce H's
checked-in outputs and that the regenerated package builds in isolation.

### Codex protocol provenance(H,U)

When relevant, provenance:

1. checks out exact H;
2. reads U from H;
3. reconstructs the protocol from U;
4. compares accepted semantic artifacts with H;
5. remains read-only.

For unrelated PRs the same required context completes successfully as not
applicable.

## Normalize before verification

Formatting is construction, not acceptance.

Repository-owned producers that write Go source normalize it before commit or
proposal sealing. CI verifies the resulting immutable H.

For manual development:

```sh
gofmt -w <changed-go-files>
```

`go mod tidy -diff` remains a proof because dependency closure is committed
semantic state and must not be silently rewritten by CI.

## Staleness and invalidation

If H changes, all H-bound proof obligations are new.

If main advances from B to B2 and B2 is not an ancestor of H, H becomes stale.
The integrator does not repair it. The PR must be rebased onto B2, producing a
new H, and required proofs/review policy are evaluated again.

No successful verdict is carried across H.

## Concurrency

Workflow execution order and `concurrency` groups can reduce redundant work,
but they are not correctness mechanisms.

The final concurrency gate is Git itself:

```sh
git push origin H:refs/heads/main
```

This must be a normal non-force push. If main moved such that the update is no
longer fast-forward, Git rejects it. The integrator stops without rebase,
merge, force, or any candidate mutation.

## Auto-forward integration

The trusted `Auto-forward PR` workflow accepts a PR number and:

1. resolves the PR's mutable head from its Git ref;
2. resolves current main B;
3. requires B to be an ancestor of H;
4. requires current-H required checks;
5. requires repository review policy to have no unsatisfied review state;
6. revalidates H/B/checks immediately before the effect;
7. uses a separate repository-scoped integration App;
8. advances main only by `git push H:refs/heads/main` without force;
9. reads back `main == H`;
10. after GitHub reports the PR merged, deletes the head ref if it still points at H.

The integration job must not run PR code with write credentials.

## Open PR head authority

For an open mutable PR branch, its Git ref owns H.

The PR API owns PR-object state such as number, open/draft state, head
repository/ref, base ref, review state, title, and body. Its `head.sha` is a
derived projection and is not a second mutable-head authority.

For protocol-sync publication, the same rule applies: force-with-lease and
remote-ref readback own the branch head; PR metadata is reconciled separately.

## Non-PR events

On `push` to main or manual native verification, workflows verify the
triggering `github.sha`. Main push checks are post-integration health evidence
for the same commit that was forwarded.

There is no merge-group acceptance identity in the auto-forward model.

## Local and narrow proofs

Use the smallest proof that covers the changed owner:

| Scope | Local proof | GitHub proof |
| --- | --- | --- |
| `llmkit` | `go vet ./llmkit/...`, `go test -race ./llmkit/...` | `Verify llmkit` |
| `codexsdk` | `go vet ./codexsdk/...`, `go test -race ./codexsdk/...`, generated check | `Verify codexsdk` |
| `llmcaller/codex` | `go vet ./llmcaller/codex/...`, `go test -race ./llmcaller/codex/...` | `Verify Codex adapter` |
| generated protocol | generated check + isolated build | `Verify generated protocol artifacts` |
| repository | root commands above | `Root source verification` |
| upstream provenance | exact reconstruction | `Codex protocol provenance` |

Narrow proofs are development evidence. They do not replace the three required
current-H contexts.

## Workflow specification boundary

Repository workflow tests protect:

- source/generated/provenance all bind to exact PR H;
- non-PR verification binds to the triggering SHA;
- provenance is read-only;
- open PR Git refs own mutable head identity;
- auto-forward uses a non-force ref update only;
- auto-forward rechecks current H and current main before the write effect;
- write credentials are isolated from PR code execution;
- no merge/rebase/squash/cherry-pick/commit path exists in the integrator;
- protocol publication and auto-forward integration use separate authority
  boundaries;
- failures do not self-repair or bypass required proof.

Do not freeze incidental job count or step names when they protect no invariant.

## Protocol upgrades

Use [`protocol-sync.md`](protocol-sync.md).

A protocol-sync PR becomes auto-forwardable only after its current H has source,
generated, and fresh H/U provenance proofs. If main moves first, protocol-sync
must rebuild/update the pending proposal from current main and obtain a new H.

## Non-gating evidence

Portability, fuzzing, vulnerability scans, and live provider smoke remain
additional evidence unless repository policy explicitly promotes them to
required checks.
