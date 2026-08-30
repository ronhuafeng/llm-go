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

Do not read an ADR archive, architecture index, or second copy of these facts.
If a change contradicts a north star or a DESIGN invariant, surface the
conflict instead of silently overriding it.

Use glossary terms from the owning `CONTEXT.md` exactly.

## Skills

Issues live in this repository's GitHub Issues. See
[`docs/issues.md`](docs/issues.md).

The remaining repository-owned skill is
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).

## Friction-to-knowledge

Treat project docs as working memory. When a correction, repeated friction, or
failed validation suggests a reusable lesson:

1. Treat it as evidence, not an automatic rule.
2. Classify it as one-off, project-local, or broadly reusable.
3. Verify with the closest source, code, tool, or failing validation.
4. Draft the smallest wording and scope that would prevent the same miss.
5. Update the closest working-memory doc when the lesson is verified and local.
6. Ask before promoting it into a broader rule, global instruction, skill, or
   helper.
7. Run lightweight validation when a document, skill, helper, or rule changes.
8. Promote only when repeated, broadly applicable, or mechanically enforceable.

Do not add an ADR unless a DESIGN invariant changes and the why would be
judged wrong without it, or a module-specific prohibition cannot fit in
DESIGN or that module's `CONTEXT.md`.

## Operating principles

- Prefer guardrails over broader automation for dangerous defaults.
- Use a canonical script or documented path for deterministic operations.
- Fail fast before mutating files, branches, tags, issues, or remote state
  when context is ambiguous.
- Do not treat local validation, pushed commits, and remote verification as
  the same state.
