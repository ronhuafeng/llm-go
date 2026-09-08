# Codex Adapter

Module-local vocabulary for the Codex-specific join between `llmkit` and
`codexsdk`. Repository semantics and authority boundaries are defined in
[`NORTHSTAR.md`](../../NORTHSTAR.md). Toolkit-owned evidence vocabulary belongs
in [`llmkit/CONTEXT.md`](../../llmkit/CONTEXT.md).

## Language

**Effective profile**:
The adapter-owned fail-closed check that a named safety profile matches
approval, sandbox, and ephemeral facts observed on the decoded thread-start
Server Observation before `turn/start`, and on a missing-thread-id partial
start that never continues. Those facts prove effect safety, not
confidentiality or disclosure-safety, data-loss prevention, or provider
retention.
_Avoid_: Requested settings, SDK validation, confidentiality guarantee

**Terminal observation**:
A completed or partial Exact Run after the Effective profile is applied.
_Avoid_: Result snapshot

**Exact snapshot**:
Isolated exact Codex facts published without the Effective profile
postcondition. Neutral facts are projected independently of this snapshot.
_Avoid_: Validated result

**Schema policy**:
Adapter-owned JSON Schema dialect admission for Codex-bound output schemas.
_Avoid_: SDK schema validation, llmkit contract compilation
