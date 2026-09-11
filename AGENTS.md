# AGENTS.md

Resolve paths from the repository root that contains this file.

## Read by task

- **Choose a module:** [`README.md`](README.md) → that module's `README.md`.
- **Change llmkit behavior:** [`llmkit/CONTEXT.md`](llmkit/CONTEXT.md) → the
  affected package docs, code, and tests.
- **Change Exact Run or protocol:** [`codexsdk/CONTEXT.md`](codexsdk/CONTEXT.md)
  → affected code and tests. Upstream sync uses
  [`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
  Apply [`Agents.test.md`](codexsdk/Agents.test.md) when changing SDK tests.
- **Change adapter execution, projection, or admission wiring:**
  [`llmcaller/codex/CONTEXT.md`](llmcaller/codex/CONTEXT.md) → affected code and
  tests. Concrete execution/approval/sandbox/permission policy remains
  application-owned even when the adapter provides the admission mechanism.
- **Change adapter schema admission:** read the
  [`Schema Policy`](llmcaller/codex/README.md#schema-policy) contract and its
  compatibility-matrix tests in addition to the adapter context. Representation
  adaptation preserves caller-owned contract semantics or fails closed.
- **Change semantic ownership, evidence meaning, judgment/retry-feedback
  boundaries, authority, repository shape, or publication:**
  [`NORTHSTAR.md`](NORTHSTAR.md) → [`DESIGN.md`](DESIGN.md) → the directly
  affected implementation or operation.
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
Distinguish local validation, pushed state, remote verification, and published
artifacts; they are different observations.
