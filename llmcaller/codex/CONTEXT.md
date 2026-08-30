# Codex Adapter

The provider-specific join between the LLM Toolkit and the Codex SDK. It owns
Codex policy and fact projection, not either adjacent runtime.

## Language

**Effective profile**:
The adapter-owned postcondition that a named safety profile matches the
configuration observed on a terminal or partial Codex result.
_Avoid_: Requested settings, SDK validation

**Execution evidence**:
The provider-neutral projection of exact Codex execution facts, with typed
Provider details as the lossless escape hatch.
_Avoid_: Metadata bag, translated result

**Terminal observation**:
A completed or partial Exact Run after the Effective profile is applied.
_Avoid_: Result snapshot

**Exact snapshot**:
Isolated exact Codex facts published without the Effective profile
postcondition.
_Avoid_: Validated result

**Schema policy**:
Adapter-owned JSON Schema dialect admission for Codex-bound output schemas.
_Avoid_: SDK schema validation
