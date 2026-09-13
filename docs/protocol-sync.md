# Codex protocol synchronization

Status: target architecture

Authority: [`NORTHSTAR.md`](../NORTHSTAR.md) P8 and [`DESIGN.md`](../DESIGN.md)
I8-I10.

## Goal

Keep the Go `codexsdk` compatible with a selected Codex App Server protocol
version. The protocol-upgrade path exists to answer three questions:

1. Did the upstream App Server protocol change?
2. If it changed, what `codexsdk` source/generated/test changes are required?
3. Does deterministic verification accept the resulting SDK?

Nothing in this procedure needs a general historical CI attestation system.

## Owners

- **Upstream Codex source / generated App Server schema** owns the protocol facts.
- **`codexsdk` generator and source** own the Go representation of those facts.
- **An implementation agent** may propose compatibility changes when mechanical
  generation is not enough. The agent does not decide whether its proposal is
  accepted.
- **Go tests and deterministic generated/schema checks** decide whether the
  resulting SDK is acceptable.
- **GitHub Actions** owns orchestration and the protected PR effect. It is not a
  fourth semantic owner.

The normal relationship is therefore:

```text
Codex protocol facts
        |
        v
mechanical generation / diff
        |
        v
optional Agent proposal
        |
        v
deterministic Go-owned verification
        |
        v
protected protocol-sync PR
```

## One linear workflow

Protocol synchronization is one workflow run.

```text
resolve selected upstream target
        |
        v
generate candidate App Server schemas
        |
        v
compare candidate with checked-in baseline
        |
   no drift? ---------------- yes --> verify current generated Go --> success
        |
        no
        v
apply deterministic mechanical update
        |
        v
run deterministic protocol verification
        |
   passes? ------------------ yes --> publish protocol-sync PR
        |
        no, and failure is protocol compatibility work
        v
Agent edits the same worktree using the concrete drift/test failures
        |
        v
run the same deterministic protocol verification again
        |
   passes? ------------------ yes --> publish protocol-sync PR
        |
        no
        v
workflow fails
```

Setup, network, checkout, upstream availability, or other infrastructure
failures fail the run directly. They are not prompts for an implementation
agent.

The Agent works in the same current worktree that will be verified afterward.
It may update generated-owner code, handwritten SDK compatibility code, focused
tests, and documentation justified by the observed protocol drift. It must not
publish, merge, tag, or certify its own work.

## Failure and retry

A failed protocol-sync run is finished. Retrying starts a new run that resolves
the selected upstream target again and regenerates the candidate from canonical
upstream facts.

The ordinary path therefore has no need for:

```text
failed_run_id
run_attempt repair identity
cross-run admission.json
historical repair-input bundles
failed-run log harvesting as an authorization contract
attempt-addressed proof artifacts
proved-tree publication attestations
separate metadata-sync versus repair-sync workflows
```

Ordinary GitHub logs are useful diagnostics, but they are not a semantic
continuation contract. If a future product requirement genuinely needs to
continue from an irreproducible failed external operation, design that
continuation separately rather than burdening every protocol upgrade with it.

## Native implementation boundary

Keep the control plane intentionally small:

```text
GitHub Actions YAML = ordering, permissions, environment, Agent invocation, PR effect
Rust / Codex        = generate upstream App Server schema facts
Go                  = generate/check Go protocol artifacts and run SDK verification
Shell / Python      = temporary mechanical glue only
```

Prefer direct `go test`, `go vet`, `gofmt`, and owner-local Go generator/check
commands over repository-specific verification frameworks. A generated-artifact
checker may report precise mismatches; it does not need to attest the workflow
run, Git tree, or publication lineage.

Delete helper scripts, JSON evidence formats, workflow states, and artifacts once
the linear workflow no longer consumes them. Do not keep compatibility paths
for a retired CI architecture.

## Deterministic acceptance

After any mechanical or Agent change, the same acceptance set runs. At minimum:

1. checked-in protocol baseline matches the generated candidate for the selected
   upstream target;
2. generated Go protocol files and generated SDK surface reproduce exactly from
   the checked-in baseline;
3. `gofmt` / whitespace checks pass;
4. `GOWORK=off go vet ./...` passes in `codexsdk`;
5. `GOWORK=off go test ./...` passes in `codexsdk`;
6. focused tests added for actual protocol drift pass;
7. repository PR verification remains green for cross-module/current-source
   composition.

A model response, a generated diff, or a clean schema comparison cannot replace
these deterministic checks.

## Publication

Publication is deliberately boring. Once the final worktree passes the
acceptance set, commit that worktree to a protocol-sync branch and create or
update a protected PR. Branch protection and ordinary PR verification remain the
merge authority.

Do not mutate the validated worktree after verification merely to satisfy a
publication helper. If the base branch moved enough that the PR must be
recomposed, fail and rerun the linear workflow against the new base. No separate
proof ledger is required.

The PR body may report the selected upstream ref/commit and whether an Agent was
needed. Those are useful provenance/diagnostic facts, not alternate acceptance
states.

## Final acceptance evidence for this architecture

The simplification is complete only when **both observations refer to the same
final PR head `H`**:

1. required `PR verification` for `H` is fully green; and
2. one manual `Codex Upstream Protocol Sync` run for `H`, dispatched with the
   current checked-in stable baseline as `upstream_ref`, `force_compare=true`,
   and `validation_only=true`, completes successfully.

That final validation-only run must visibly demonstrate:

```text
selected upstream ref/commit resolved
a fresh candidate schema was generated
candidate-versus-baseline comparison completed
generated Go / SDK surface check ran
codexsdk deterministic tests ran
no Agent was invoked for the already-current clean baseline
no commit, PR, merge, or tag effect occurred
```

Structural/unit tests must separately prove the drift branch has this shape:

```text
drift -> mechanical change -> deterministic verification
     -> Agent only if protocol compatibility still fails
     -> the same deterministic verification after Agent
     -> publication only after success
```

Do not deliberately invent a production protocol failure or spend provider
secrets merely to exercise the Agent branch for final acceptance.
