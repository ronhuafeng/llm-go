# Reviewed verifier repair continuation

Status: implementation candidate for the bounded protocol repair flow. Hosted
isolation, live runtime and end-to-end target continuation remain acceptance
obligations; local tests and this document alone do not establish deployment.
This is a process change, not a preinstalled fix for the current target.

## Outcome

A protocol upgrade may reveal either an implementation defect or a verifier that
misstates an already accepted contract. The running Agent must be able to propose
a verifier repair without making its own candidate the acceptance authority.
Upgrade and verifier proposals therefore have separate worktrees and scopes.
Verifier changes become authority only after their own reviewed PR is integrated.

## Smallest sequence

1. The existing trusted controller selects the exact source and retains its
   candidate, then runs the bounded upgrade session and independent verification.
2. On concrete verification failure, a separate verifier-repair session receives
   immutable source, accepted public contracts, the failing proposal, and actual
   diagnostics. It may propose changes only to the inner verifier and focused
   regressions. It cannot change the upgrade proposal or outer controls.
3. No verifier patch means no new authority. Its bounded explanation can inform
   the upgrade session; unresolved or rejected work retains evidence and stops
   under the existing finite limits. Prose never authorizes broader file scope.
4. A verifier patch must contain a reproducible old-verifier false rejection and
   regressions that reject actual defective implementations. A trusted runner
   executes the focused tests against old and proposed verifier trees in isolated
   sandboxes. A fresh independent review inspects the source/contract basis and
   test quality; the author cannot certify its own assertions. Review is necessary
   but does not replace execution, fixed scope checks, or the normal CI gates.
5. A separate publisher creates a verifier-only PR from the accepted base. It does
   not update the schema-sync PR, baseline identity, or tombstone state. No updated
   verifier is loaded into the original repair run.
6. Completion of normal PR verification may enter the existing auto-forward
   workflow only for a native-verified verifier proposal. The resolver checks
   origin, creator, scope, exact head/tree, bound review and executed test evidence.
   The existing latest-attempt four checks, repository review policy, ancestry,
   normal fast-forward and readback are still mandatory.
7. The merged verifier PR event can continue the original target with a fresh
   accepted-main controller. It validates the trusted original run/artifact,
   actual integrated PR, unchanged upstream identity, candidate/proposal bytes,
   and remaining budget. It first rechecks the retained proposal without a model
   turn; every deterministic proof is freshly executed. If implementation work
   remains and budget permits, the upgrade session continues from that evidence.

## Authority and bounds

- Native run/artifact provenance and Git refs are authority. An editable PR marker
  may locate the original run; it cannot grant a target, budget, or review verdict.
- The verifier scope excludes controller, source identity, authorization,
  publisher, workflow, acceptance, checkpoint, and budget implementations.
  Upgrade proposals still cannot change verifier code.
- Every upgrade or verifier construction attempt consumes the original finite
  round allowance (default six) before it starts, including empty, failed or
  cancelled attempts. Continuation carries consumption forward. One
  verifier-PR handoff per chain is the initial bound; it is not recursive repair
  of the repair process. The original absolute 90-minute repair deadline is
  retained across the verifier PR, including CI/queue time; it is not restarted.
  Explicit author format/fixture/proof failures feed the same verifier session
  within the remaining shared allowance. Scope, identity, sandbox setup, unknown
  failures and unknown/refused independent review stop the chain.
- Unknown review, failed tests/checks, stale main/source, changed patch, or missing
  provenance retains a blocked result. It does not silently retry or reset limits.
- Origin run, target, verifier PR head, and integrated main identify continuation.
  Within the existing serialized workflow concurrency, native workflow-run
  observation (excluding the current run) suppresses duplicate merged events or
  attempts before starting work; an uncoordinated check-then-start is insufficient.
  Existing exact-base `recovery_run_id` remains an explicitly authorized new finite
  invocation, never an automatic fallback or same-chain budget reset.
- A historical exhausted run does not acquire more rounds from this feature.
  A separately authorized fresh task is represented honestly as a new chain.
- Workflow-event resolution and provenance validation run from trusted default-branch
  code in a read-only job. Unverified PR/artifact data cannot enter an App-token
  effect job or be executed by it.
- No new credentials, permission grants, database, dispatcher service, or bypass
  of normal integration is introduced. Models remain outside write-credential jobs.

## Evaluation

The three retained failures from sync 38116364228 are an evaluation input, not
handwritten fixes installed into the new flow:

- Optional null-only RPC params: distinguish the existing parameterless canonical
  encoding from ordinary nullable presence/value preservation.
- OutputSchema: distinguish its already accepted valid-JSON-Schema contract from
  the raw upstream unconstrained field.
- Realtime item null: preserve raw source nullability; opaque JSON representation
  alone is not permission to weaken the verifier and accept a faulty decoder.

The flow must independently propose and justify the first two verifier corrections,
while retaining the third as an implementation failure. Fixed outer regressions
also inject an erased assertion, a weak negative fixture, unapproved paths,
changed review head, stale source/base, duplicate continuation, cancelled work,
review refusal and a failed CI gate. None may produce a published sync result or
an unbounded new chain. Tests distinguish generated proposals, executed evidence,
reviewed PRs, integrated verifier changes and successful protocol synchronization.

## Implementation ownership

- `protocolsync/repair.go` and checkpoint helpers: role handoff and shared budget.
- `protocolsync/changes.go`: distinct verifier proposal scope.
- New owner-local verifier proposal/continuation helpers: native provenance,
  readback, idempotency and independent publication, using existing GitHub APIs.
- `cmd/protocolupgrade` and the Codex executor action: bounded role invocation.
- Existing sync workflow: isolated proposal artifact/publication and merged-event
  continuation. Existing auto-forward workflow: filtered verification-completion
  entry, reusing its current acceptance/integration implementation.
- Fixed controller/workflow tests plus a small bounded lifecycle model: identities,
  duplicate events, budgets and authority. The inner verifier cannot modify these.
