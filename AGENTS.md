# AGENTS.md

Resolve paths from the repository root that contains this file.

## Read by task

- **Choose a module:** [`README.md`](README.md) → that module's `README.md`.
- **Change llmkit behavior:** [`llmkit/CONTEXT.md`](llmkit/CONTEXT.md) →
  package docs and tests. Read [`NORTHSTAR.md`](NORTHSTAR.md) only if the
  change could mix validation with sanitizer output or add a provider SDK.
- **Change Exact Run or protocol:** [`codexsdk/CONTEXT.md`](codexsdk/CONTEXT.md)
  → code and tests. Upstream sync uses
  [`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
  Apply [`Agents.test.md`](codexsdk/Agents.test.md) when changing SDK tests.
- **Change adapter projection or policy:**
  [`llmcaller/codex/CONTEXT.md`](llmcaller/codex/CONTEXT.md) → adapter README
  schema matrix and tests.
- **Change repository shape, joins, or release:** [`NORTHSTAR.md`](NORTHSTAR.md)
  → [`DESIGN.md`](DESIGN.md) → [`docs/verify.md`](docs/verify.md) or
  [`docs/release.md`](docs/release.md).

Do not read a second copy of these facts.
If a change contradicts a north star or a DESIGN invariant, surface the
conflict instead of silently overriding it.

Use glossary terms from the owning `CONTEXT.md` exactly.

## Skills

Issues live in this repository's GitHub Issues. See
[`docs/issues.md`](docs/issues.md).

The remaining repository-owned skill is
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).

## Friction-to-knowledge

Verified local lessons go in the closest CONTEXT or DESIGN. Do not add a
second design file.

## Operating principles

- Prefer guardrails over broader automation for dangerous defaults.
- Use a canonical script or documented path for deterministic operations.
- Fail fast before mutating files, branches, tags, issues, or remote state
  when context is ambiguous.
- Do not treat local validation, pushed commits, and remote verification as
  the same state.
