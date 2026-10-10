# Bounded protocol-sync lifecycle obligation

`SyncLifecycle.tla` is a focused requirements model, not a refinement proof of
Go or GitHub. The governing contracts are `docs/protocol-sync.md`,
`docs/verify.md`, and `docs/auto-forward.md`. It covers the repair-required path:
retained candidate, model proposal, independent verifier feedback, publication,
exact-head checks, and the final acceptance effect.

## Scope and assumptions

- Two values each distinguish accepted/replaced base, upstream target, and head.
  `B2` denotes an incompatible base, not an arbitrary compatible advance.
  Candidate fields stand for the retained digest-bound bytes and exact identity.
- Two repair rounds per invocation expose feedback, cancellation, exhaustion,
  and recovery. At most one explicit external authorization grants invocation 2,
  resetting its finite round budget while saving invocation 1's consumed count.
  No automatic action resets this budget.
  Two workflow attempts expose old green checks superseded by a newer attempt.
  The four required contexts are reduced to their conjunction: one unsuccessful
  context is sufficient to block. `trustedApp = FALSE` and `checkHead = H2`
  represent wrong-origin/wrong-head observations.
- Environment actions can invalidate identity between model output, verification,
  publication, and acceptance. It may cancel or never deliver a response.
  Explicit stuttering represents indefinite non-response and quiescent terminal
  states. No fairness or liveness claim is made; model/provider/GitHub response
  and useful repair progress are not guaranteed.
- `NewAttempt` is an external/manual rerun. `RecoverChecks` is the separately
  authorized automatic cancellation recovery; it cannot replay started live
  execution. The accepted effect is terminal, so later external changes do not
  retroactively invalidate historical acceptance.
- Git's atomic non-force update, App permissions, API timestamp/attempt ordering,
  pagination, checksum collision resistance, filesystem atomicity, auth, secret
  handling, real model behavior, and actual review policy are external or
  separate implementation obligations. `Accept` abstracts a successful final
  identity/check/review revalidation and effect; it is not a proof of GitHub's
  read-to-write atomicity.
- There are no state/action constraints, symmetry reductions, or overrides.
  The explicit stuttering successor is intentional; terminal quiescence and an
  absent environment response are not claimed as deadlocks.

## Checked properties and regression

The positive configuration checks type closure, exact candidate identity at
acceptance, independent-program ownership of acceptance, latest exact-head
trusted proof, finite repair rounds, feedback preceding a subsequent round, and
one cancellation recovery. Model completion alone reaches `verify`, never
`ready` or `accepted`.

`OldGreenRegression.cfg` enables only the faulty old-green acceptance guard.
Its expected violation is `LatestExactProof`, with this shortest witness:

1. Construct the retained candidate and receive a model reply.
2. Independent verification succeeds; publish H1.
3. Attempt 1 succeeds on H1 from the trusted App.
4. Attempt 2 starts and is pending, before any new check job exists.
5. The faulty consumer accepts from attempt 1's old green result.

The corrected guard blocks step 5. This is an executable discriminability
witness, not an assertion that production GitHub exhibited this whole trace.

## Reproduce

Use the pinned `model-with-tla` checker, with `TLA_CHECK` pointing to its bundled
`.tools/tla-check`. Its manifest and checksum are the toolchain authority.

```sh
"$TLA_CHECK" check --module "$PWD/SyncLifecycle.tla" \
  --config "$PWD/SyncLifecycle.cfg" --expect success
"$TLA_CHECK" check --module "$PWD/SyncLifecycle.tla" \
  --config "$PWD/OldGreenRegression.cfg" --expect invariant:LatestExactProof
```

Run from this directory. The checker runs SANY before exhaustive TLC, validates
the expected result, and removes temporary state outside the repository.
In restricted environments, point uv's cache, Python install, and project
virtual environment directories outside the repository to writable temp paths.

Recorded 2026-10-10: both checker invocations matched their expectations; SANY
passed. Positive: 26,586 generated / 8,110 distinct states, depth 21. Negative:
963 generated / 409 distinct states, counterexample depth 9. Pinned TLC 1.8.0
prerelease, revision `0894c3407f4717fec7cc18bde3bf3c857fa47333`, SHA-256
`ab323b79802aedc3203b3f9af37c6aca3ed43f4e0225b36f2aa77b26de46c05f`,
Java 21.0.12.1. These are finite scoped results, not parameterized proofs.

## Implementation trace mapping

- `ModelReply → VerifyBad → ModelReply → VerifyGood` maps to
  `TestRepairSequentialUnknownShapesAndValidationFeedback`. Model completion
  supplies a proposal; the independent oracle supplies readiness.
- `CancelRepair/AuthorizeRecovery/ResumeRepair`, retained identity, and `RejectStale` map to
  `TestRepairBlocksAndRetainsCheckpoint` and
  `TestRepairCheckpointRecoveryRejectsStaleAndTamperedState`.
- `CheckResult` with wrong head/App and unsuccessful conclusions maps to
  `TestExactHeadLatestActionsPolicy`.
- Old-green supersession maps to
  `TestPaginationAndWorkflowAttemptBarrier` and
  `TestOlderWorkflowRerunIsLatestAttempt`.
- `RecoverChecks` and its non-replay/budget guard map to
  `TestCancellationRecoveryBoundAndLiveNoReplay`.
- The workflow consumers are exercised by
  `TestLiveResultIsRequiredBeforeEachIntegrationEffect` and
  `TestCancellationRecoveryHasSeparateTrustedAuthority`.

These mappings identify controlled test seams; passing TLC is not evidence that
those tests or a production/live run passed. Round budget in this model is
per invocation. An explicit `recovery_run_id` workflow dispatch is new external
authorization for a fresh finite budget on the same candidate, with no proof
reused. The retained checkpoint records consumed prior rounds for diagnostics.
There is no background model retry; the finite instance includes only one
additional authorized invocation. Unbounded future user requests are outside
this finite check.
