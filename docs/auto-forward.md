# Auto-forward integration

Auto-forward advances main to a candidate that has already been verified. It
does not create, rewrite, or repair a candidate.

## Acceptance contract

Let H be the current same-repository PR head and B be current main. The
integration is permitted only when B is an ancestor of H, all applicable checks
for H have succeeded, and existing repository review policy is satisfied.

```text
tested H == reviewed H == integrated H == new main
```

[`verify.md`](verify.md) owns the required proofs. The accepted policy includes
real Codex integration for relevant changes, with explicit successful
not-applicable classification for unrelated changes. A successful source or
protocol workflow does not stand in for a missing live result.

The required consumer includes `Live Codex integration / Live scenarios`.
PR verification calls the live workflow directly, so its successful completion
cannot dispatch auto-forward before the required live child has succeeded.
Current-head results and production integration readback remain the evidence of
actual deployment; source text alone is insufficient.

## Trusted workflow

`Auto-forward PR` is dispatched from trusted main with a PR number. The existing
`Dispatch auto-forward` route also dispatches eligible protocol-sync bot PRs
following verification. Its notification/dispatch is not independent
acceptance authority.

The read-only phase resolves the PR identity, H from its Git ref, and current B.
It checks ancestry, the required current-head results, and review state. The
effect phase rechecks these conditions before minting the dedicated integration
App token. If an independently completed live job is part of verification,
ordering must not allow an earlier source success to bypass it or leave its
completion disconnected from the existing dispatch path.

The only integration write is:

```sh
git push origin H:refs/heads/main
```

It must be a normal non-force fast-forward. Do not merge, rebase, squash,
cherry-pick, create a commit, force-push, or retarget a stale PR during this step.
Read back main after the effect. Lost/ambiguous outcomes require observation
before repeating an effect.

After readback, head deletion is allowed only when GitHub reports this PR merged,
the ref still points at integrated H, main contains H, and no other open PR uses
that ref. GitHub's automatic branch deletion does not perform this external
fast-forward cleanup.

## Stale or incomplete candidates

A changed H, missing/failed/cancelled required result, unsatisfied review state,
or non-fast-forward update stops integration. A provider outage remains a failed
live gate, not a reason to bypass it. A skipped workflow is not the explicit
not-applicable result required for unrelated changes.

If main advances past H's base, development must rebase/reconstruct the proposal
and obtain results for the new H. The integrator never does that repair itself.
Workflow concurrency is an optimization; Git ancestry and the non-force push
remain authoritative.

## Existing authority setup

The integration App uses repository variable `AUTO_FORWARD_APP_CLIENT_ID` and
secret `AUTO_FORWARD_APP_PRIVATE_KEY`, scoped to this repository with Contents
write for the final effect. Keep this separate from the protocol-publication
App and from model-test provider credentials.

The documented main ruleset restricts main updates/deletion to this App; required
check evaluation and non-force behavior belong to the trusted workflow. Do not
assume adding a named live job automatically changes either enforcement point.
Inspect actual repository policy when changing enforcement.

This remains the controlled same-repository model used by the single author and
protocol-sync bot. Live-test work does not add fork trust negotiation, another
approval service, or an attestation ledger.

## Acceptance of enforcement changes

Changes to the required-check consumer need tests that show a relevant live
failure, missing result, or cancellation prevents eligibility, while genuine
not-applicable completion does not block an unrelated PR. Confirm a real
relevant live success using the exact installed runtime and canonical fixture.

A repository-policy deployment is not proven by YAML or documentation alone.
Use the existing authorized integration route for production acceptance when
that deployment is performed, observe the final main ref and GitHub PR state,
and do not claim an integration that has not been read back. No policy change
implicitly authorizes bypassing checks for its own installation.
