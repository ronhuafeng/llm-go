# Issue tracker: GitHub

Implementation work for this repository lives in GitHub Issues. Use the owning
GitHub interface (the connected GitHub tools or `gh`) and verify the repository
identity; a local checkout's remote is useful when one is available.

## Evidence-backed tickets

Before creating work, read the governing goal, current source revision, relevant
tests/docs, and open issues/PRs. Prior discussion is accepted intent or candidate
analysis, not evidence that an implementation is still missing. Reuse an issue
that already owns the outcome. Keep unverified facts explicitly unknown.

Create one issue per independently acceptable outcome, not one per file or gap
sentence. Specify current evidence, desired observable behavior, scope,
acceptance criteria, and only real dependencies. Do not create an umbrella
without a tracking consumer or invent priorities/labels.

Apply `ready-for-agent` only after scope, acceptance criteria, and blockers are
explicit. Use native GitHub blocking dependencies when the available interface
supports them; otherwise retain a `Blocked by` section. Do not close or change
an unrelated parent while implementing a child.

## Live integration work

[`live-codex.md`](live-codex.md) is the accepted design. Its remaining work is
owned by #376 (runtime configuration and required-gate enforcement) and #377
(the two initial integration stories). Do not duplicate their outcomes as
separate model, proxy, per-RPC, or documentation tickets.

Tests are scenarios rather than a capability registry. Any proposal to remove
or materially weaken a live story must disclose the retirement and include the
changelog/release-note obligation in its acceptance criteria.

## Publication and completion

Publish only when issue creation/update is requested. Read back each write
before reporting completion; do not repeat an uncertain creation without first
checking whether it succeeded. Read the full issue, labels, dependencies, and
comments before implementation.

Issue publication does not authorize source changes, commits, branches, PRs,
or integration. A separate request may authorize those effects. PRs carry
implementation and review evidence; they are not the request/triage surface.
