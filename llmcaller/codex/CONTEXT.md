# Codex Adapter

The provider-specific dependency join between the LLM Toolkit and Codex SDK.
It owns Codex policy and fact projection, not either adjacent module's runtime.

## Language

**Codex Adapter**:
The module that implements toolkit caller contracts by invoking the Codex SDK
and projecting exact Codex results.
_Avoid_: Workflow engine, SDK wrapper

**Effective profile**:
The adapter-owned postcondition comparing a requested named safety profile with
the exact configuration observed in a terminal Codex result, including partial
results.
_Avoid_: Requested settings, SDK validation

**Execution evidence**:
The provider-neutral projection of exact Codex execution facts, published with
typed Provider details as the lossless escape hatch.
_Avoid_: Metadata bag, translated result
