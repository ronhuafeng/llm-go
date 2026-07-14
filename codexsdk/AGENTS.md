# Agent Operating Principles

- Prefer guardrails over broader automation for dangerous defaults.
- Use a canonical script or documented path for deterministic, error-prone operations.
- Fail fast before mutating files, branches, tags, issues, or remote state when context is ambiguous.
- Report completion in layers. Do not treat local validation, pushed commits, and remote verification as the same state.
- Preserve useful pre-change evidence before overwriting generated reports or cleanup artifacts.
- Add recovery recipes before adding orchestration frameworks. Promote a recipe to automation only after repeated use justifies the maintenance cost.
- Keep changes scoped to the task and avoid turning one incident into a broad subsystem unless the repo already has that pattern.

# Agents.test.md

Apply the testing guidance in [Agents.test.md](./Agents.test.md) when adding, changing, replacing, or deleting tests. Tests should encode durable behavior and source-of-truth contracts rather than current implementation details, specific historical incidents, or one-off version assumptions.
