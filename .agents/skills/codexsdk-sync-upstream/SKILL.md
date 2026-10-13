---
name: codexsdk-sync-upstream
description: Complete a fixed-target Codex SDK upstream upgrade in one native goal, including generation, implementation and contract-grounded verifier changes, full verification, and failure-driven repair.
---

# Complete the upstream upgrade

Own the complete outcome supplied by the goal. Read `AGENTS.md` and the directly
relevant owner source and tests. Choose your own working order and continue
until the final candidate meets the acceptance command below, or a concrete
external blocker requires a decision. Do not wait for an outer repair round,
role switch, or separately published verifier change.

The trusted launcher supplies the base, exact upstream ref and peeled SHA,
workspace, runtime and deadline. These are inputs, not decisions for the Agent.
Preserve unrelated work and the existing application-facing contracts.

## Work to the exact upstream contract

Use the repository's native protocol generation and upgrade commands. Inspect
their current usage before invoking them. Generate schema and Go artifacts from
the supplied exact upstream source; do not hand-edit generated answers or drop
unknown shapes to make generation pass.

Adapt the handwritten implementation and focused tests as required. Verifier
assumptions may evolve in this same proposal when the exact upstream source and
accepted SDK contract justify the change. Include a legitimate-evolution example
that passes and a real-defect example that still fails. Expected behavior must
come from those independent contracts, not reflection over your implementation.
Do not delete tests, reduce coverage, or relax a stable type, authority or
lifecycle invariant merely to obtain a pass. If the authorities genuinely
conflict, report the specific decision required.

## One acceptance entrypoint

Call the provided `verify_upstream` tool with no arguments. It is the single
full-acceptance entrypoint for this goal. The trusted launcher binds its
repository, Draft PR, branch, base, target and deadline; you cannot choose an
alternative publication destination.

The native workflow is deployed on main through PR #421. The operator reviews
the complete prompt and authorizes the fixed-target run before launch. The
target synchronization PR remains Draft, including after successful acceptance.

The tool first freezes the current candidate, including intended untracked
source, and runs the credential-free local checks in the existing isolation
boundary. It does not commit or alter your proposal workspace. After those
checks pass, it may append a normal commit only to the bound Draft PR branch.
It then waits for that exact head's current required CI checks, including real
Live. These are the four existing standards in `docs/verify.md`:

- Source correctness, including applicable syntax/isolation checks,
  tidy-without-diff, vet and race tests.
- Generated reproducibility and isolated compilation of freshly generated code.
- Provenance rebuilt from the exact recorded upstream source, with accepted-base
  behavior and target-schema contracts checked against that same candidate.
- Existing real Codex Live scenarios, with their own model/provider fixture.

Local checks and CI share the check implementations; local success alone is
not full acceptance. The goal's tool invocation includes waiting for CI, reading
its diagnostics and repairing failures. Later checks repeat these same standards.
Do not run Live with host credentials, configure authentication or create a new
credential service. Missing prerequisites or unavailable CI are blockers.

The tool returns only the path to a complete, secret-safe JSONL diagnostic file.
Read it incrementally and fix the underlying cause. Call the full tool again
for a changed final candidate. Preserve diagnostic records; do not replace them
with an inline truncated summary, print credentials or copy private transcripts.
A diagnostic file delivery does not mean its checks passed. Do not repeatedly
rerun real Live without a relevant change or verified transient recovery.

## Completion and authority

Mark the native goal complete only after the exact final candidate passes the
full tool invocation, including all four CI gates for its exact head. Report
the changes, checks, diagnostic paths and Draft PR. A model's
completion claim, a tool exit without the required checks, or a partial pass is
not independent acceptance.

Change the files needed for the fixed-target upgrade, including generated
artifacts, implementation, tests and justified verifier changes. Publication
has no file-path allowlist. Explain changes beyond protocol generation and
adaptation, and their risks, in the final report for independent review. Preserve
the acceptance contract; the trusted launcher owns source identity, credentials,
publication and required-check decisions. Do not configure authentication, stage
or commit in the proposal repository, push, mutate PRs or dispatch workflows
yourself. Only the fixed
verification tool may update the bound Draft PR after local checks pass. Keep
that PR Draft. Do not merge, integrate or tag, even when all checks pass.

Report a concrete blocked outcome for unavailable credentials/environment or an
unresolved contract decision. Respect the supplied deadline; do not invent a new
budget, silently change the target, or start a second goal to evade a limit.
