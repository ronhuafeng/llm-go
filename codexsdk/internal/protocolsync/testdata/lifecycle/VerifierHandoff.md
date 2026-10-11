# Verifier handoff: one origin chain

This independent requirements model adds the reviewed-verifier handoff to the
existing lifecycle model. It does not modify or inherit that older model's
explicitly authorized recovery-invocation reset. Here all automatic continuation
belongs to one original task, with six total proposal rounds and one unchanged
absolute deadline.

## Scope and assumptions

- `rounds` is the implementation ledger; `spent` is the history count of actual
  upgrade and verifier proposal constructions. Both roles charge the same ledger.
  Deterministic rechecking does not construct a proposal or consume another round.
- `I0` stands for the exact origin, upstream, retained candidate, upgrade proposal
  and fixture identities. `I1` differs in at least one component. Two verifier
  heads distinguish reviewed/proved/current/merged identities. Cryptographic
  collision resistance and native provenance validation are external obligations.
- Time is a finite ordering abstraction, not a number of seconds. Deadline 3 and
  ticks through 4 expose early, boundary and late delivery. No action refreshes
  the original deadline, including review, integration or continuation.
- `ExecuteProof` is a fixed runner's observed execution, not model prose. Review
  has separate identity, exact-head/input binding and approval. All required CI
  checks are abstracted as their exact-head conjunction. A failed or missing
  check cannot merge. This model does not judge the semantic quality of fixtures
  or reviews; owner tests and independent review retain that obligation.
- Integration may finish after the repair deadline. That does not extend the
  original chain: its subsequent late continuation event is rejected.
- `DeliverMergedEvent` is an atomic authorization boundary under the existing
  workflow concurrency group `codex-upstream-protocol-sync`, with
  `cancel-in-progress: false`. The consumed flag abstracts durable native run
  observation under that serialization. API completeness/consistency and the
  implementation's read-to-effect ordering remain separate obligations; this is
  not a proof of exactly-once GitHub dispatch in an unserialized environment.
- A successful continuation creates a fresh controller epoch at the exact merged
  head. Its first action is rechecking the retained proposal. It does not reuse
  acceptance from the previous controller. Changed main/source and duplicate
  deliveries cannot grant a fresh authorization.
- Explicit stuttering allows arbitrary missing responses or rejected events.
  There is no fairness, eventual merge, successful repair or liveness claim.
  There are no state constraints or symmetry reductions hiding invalid states.
  The finite model does not enumerate JSON schemas or prove code refinement.

## Properties and deliberate counterexamples

`VerifierHandoff.cfg` checks type closure, the six-round shared ledger, immutable
deadline, at most one handoff, independently reviewed executed proof before
publication, at most one authorization, exact fresh continuation and no late
continuation. Historical authorization snapshots are checked; a later external
change does not retroactively erase an earlier correctly authorized event.

Each regression configuration enables exactly one defective transition:

- `ResetBudget`: after a successful handoff, reset `rounds` while `spent` retains
  the earlier proposal count. Expected `SharedBudget` violation immediately.
- `RefreshDeadline`: tick before constructing the verifier proposal, then assign
  a new deadline. Expected `DeadlineNeverRefreshes` violation.
- `StaleHead`: deliver a different head after merging the reviewed head. Expected
  `ExactFreshContinuation` violation.
- `Duplicate`: deliver the same successful event twice. Expected
  `OneAuthorization` violation.
- `SelfReview`: let the author approve its own passing proposal and publish it.
  Expected `IndependentPublication` violation.
- `RepeatHandoff`: continue, fail deterministic recheck, construct another upgrade
  proposal and then a second verifier proposal. Expected `OneHandoff` violation.
- `ReuseController`: continue using the original controller epoch/base. Expected
  `ExactFreshContinuation` violation.
- `LateEvent`: advance time to the original deadline before delivering an otherwise
  valid merged event. Expected `NoLateAuthorization` violation.
- `StaleInputs`: change retained/source identity after merge, then deliver the
  original event. Expected `ExactFreshContinuation` violation.

These are designed counterexample traces, not reported TLC executions.

## Reproduce and execution status

Use the same pinned official checker described in the adjacent lifecycle README,
with `TLA_CHECK` set to its existing `.tools/tla-check`. Run from this directory:

```sh
"$TLA_CHECK" check --module "$PWD/VerifierHandoff.tla" \
  --config "$PWD/VerifierHandoff.cfg" --expect success
for pair in \
  ResetBudget:SharedBudget \
  RefreshDeadline:DeadlineNeverRefreshes \
  StaleHead:ExactFreshContinuation \
  Duplicate:OneAuthorization \
  SelfReview:IndependentPublication \
  RepeatHandoff:OneHandoff \
  ReuseController:ExactFreshContinuation \
  LateEvent:NoLateAuthorization \
  StaleInputs:ExactFreshContinuation
do
  name=${pair%:*}; invariant=${pair#*:}
  "$TLA_CHECK" check --module "$PWD/VerifierHandoff.tla" \
    --config "$PWD/VerifierHandoff${name}Regression.cfg" \
    --expect "invariant:${invariant}"
done
```

Recorded 2026-10-11: Java is present, but no existing TLC jar/checker was found in
the inspected tool/workspace locations. SANY and TLC have **not** run for this
new model; there are no reported state counts, verified counterexamples or
finite-check success claims. No replacement tool was downloaded. Execution must
use the pinned checker's normal SANY-first, bounded-output, external-state-dir
workflow before upgrading that evidence status.

## Implementation seams

- `proposeVerifier`, the shared repair checkpoint and continued repair entry own
  proposal charging, one-handoff consumption and the original deadline.
- `runVerifierProposalProof` supplies observed proof; `validateVerifierReview`
  and `ValidateVerifierEvidence` bind it to separate reviewed identity and bytes.
- `PublishVerifier` and `ResolveVerifier` own publication and exact native facts;
  normal auto-forward retains the repository's CI/review/integration policy.
- `priorVerifierContinuation` and `ContinueVerifier`, inside the serialized sync
  workflow, own duplicate native-run observation, exact integrated head, source
  revalidation and deadline checks. The continuation controller must be built
  from that exact accepted head before fresh deterministic rechecking.

These are refinement obligations to inspect and test, not a claim that the model
itself has verified those Go functions or any real workflow execution.
