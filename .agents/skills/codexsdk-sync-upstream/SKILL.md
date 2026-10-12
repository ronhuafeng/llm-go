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

From the repository root, run:

```sh
go run ./codexsdk/internal/cmd/protocolupgrade verify
```

**Draft integration status:** this entrypoint is being implemented in PR #421.
Until it exists and its same-candidate behavior is verified, this revised skill
must not be used for a formal goal. The earlier `protocolupgrade check` command
checks only schema/generated artifacts and is not a substitute.

The command owns the complete acceptance standard and is shared with CI. It
checks a frozen snapshot of the current candidate, including intended untracked
source, without committing or changing the proposal workspace. All gates refer
to those same bytes:

- Source correctness, including the repository's applicable syntax/isolation
  checks, tidy-without-diff, vet and race tests.
- Generated reproducibility and compilation of freshly generated protocol code
  in isolation.
- Provenance rebuilt from the exact recorded upstream source.
- The existing real Codex Live scenarios, with the run-selected installed CLI
  and their own existing model/provider fixture.

These are the four required CI contexts defined in `docs/verify.md`, not four
additional standards. Necessary build/runtime prerequisites are provisioned by
the launcher. Missing prerequisites or credentials are blockers, not skips.
The Agent must execute full verification before completion; later CI repeats
that same standard and cannot replace the Agent's first verification.

On failure the command writes complete, secret-safe diagnostics to JSONL and
returns the file path. Read the file incrementally, fix the underlying cause,
and rerun the affected checks; rerun the full entrypoint on the final candidate.
Preserve all diagnostic records. Do not replace the file with a truncated inline
summary, print credentials, or copy private model transcripts. Rerun real Live
only when a relevant change or verified transient recovery justifies it, not in
an unbounded retry loop.

## Completion and authority

Mark the native goal complete only after the exact final candidate passes the
full entrypoint. Report the changes, checks and diagnostic paths. A model's
completion claim, a tool exit without the required checks, or a partial pass is
not independent acceptance.

The proposal may contain generated artifacts, implementation changes, focused
tests and justified verifier changes. Do not alter the launcher's source
identity, credential boundary, scope checks, publication controls or required
CI gates. Do not configure authentication, stage or commit in the proposal
repository, push, mutate PRs, dispatch workflows, integrate or tag. The trusted
publisher and independent review own those effects.

Report a concrete blocked outcome for unavailable credentials/environment or an
unresolved contract decision. Respect the supplied deadline; do not invent a new
budget, silently change the target, or start a second goal to evade a limit.
