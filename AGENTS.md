# AGENTS.md

Resolve paths from the repository root that contains this file.

## Read by task

- **Choose a module:** [`README.md`](README.md) → that module's `README.md`.
- **Change llmkit behavior:** [`llmkit/CONTEXT.md`](llmkit/CONTEXT.md) → the
  affected package docs, code, and tests. Preserve the distinction between
  proposition evidence and accepted result, and name application-owned retry
  feedback hooks as projection rather than toolkit-owned sanitization policy.
- **Change Exact Run or protocol:** [`codexsdk/CONTEXT.md`](codexsdk/CONTEXT.md)
  → affected code and tests. Upstream sync uses
  [`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
  Apply [`Agents.test.md`](codexsdk/Agents.test.md) when changing SDK tests.
  Lifecycle admission must cover every model-directed continuation path that
  observes policy-relevant exact facts before proceeding, including resume or
  re-entry paths rather than only thread start.
- **Change adapter execution, projection, or admission wiring:**
  [`llmcaller/codex/CONTEXT.md`](llmcaller/codex/CONTEXT.md) → affected code and
  tests. Concrete execution/approval/sandbox/permission policy remains
  application-owned even when the adapter provides the admission mechanism.
  Neutral projection preserves final-response presence and measurement scope;
  requested/selected/routed model facts do not become served-model evidence
  without an attributable lower-layer serving observation.
- **Change adapter schema admission:** read the
  [`Schema admission`](llmcaller/codex/README.md#schema-admission) contract and
  its compatibility tests in addition to the adapter context. Representation
  adaptation preserves caller-owned contract semantics or fails closed.
- **Change semantic ownership, evidence meaning, judgment/retry-feedback
  boundaries, authority, repository shape, or publication:**
  [`NORTHSTAR.md`](NORTHSTAR.md) → [`DESIGN.md`](DESIGN.md) → the directly
  affected implementation or operation.
- **Change a pre-v1 cross-module source cohort:** read
  [`DESIGN.md`](DESIGN.md) I6 → [`docs/verify.md`](docs/verify.md) →
  [`docs/release.md`](docs/release.md). Current-source replacement proofs are
  not published-closure proofs; publication remains dependency-first.
- **Verify or release:** [`docs/verify.md`](docs/verify.md) or
  [`docs/release.md`](docs/release.md).

Do not read a second copy of a fact when the canonical owner above is enough.
If a change contradicts a north star or live invariant, surface the conflict
instead of silently overriding it. Use glossary terms from the owning
`CONTEXT.md` exactly.

## Repository operations

Issues live in GitHub Issues; conventions are in
[`docs/issues.md`](docs/issues.md).

Use the canonical documented or scripted path for deterministic operations.
Distinguish local validation, current-source composition, pushed state, remote
verification, published dependency closure, and published artifacts; they are
different observations.
