# Codex Adapter

Module-local vocabulary for the Codex-specific join between `llmkit` and
`codexsdk`. Repository semantics and authority boundaries are defined in
[`NORTHSTAR.md`](../../NORTHSTAR.md). Toolkit-owned evidence vocabulary belongs
in [`llmkit/CONTEXT.md`](../../llmkit/CONTEXT.md).

## Language

**Effective profile**:
The adapter-owned postcondition that a named safety profile matches the
configuration observed on a terminal or partial Codex result.
_Avoid_: Requested settings, SDK validation

**Terminal observation**:
A completed or partial Exact Run after the Effective profile is applied.
_Avoid_: Result snapshot

**Exact snapshot**:
Isolated exact Codex facts published without the Effective profile
postcondition.
_Avoid_: Validated result

**Schema policy**:
Adapter-owned JSON Schema dialect admission for Codex-bound output schemas.
_Avoid_: SDK schema validation, llmkit contract compilation
