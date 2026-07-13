# llmkit-go

Provider-neutral typed LLM operations and the evidence they publish to callers.

## Language

**Toolkit-owned state**:
Provider-neutral request and result state whose representation llmkit-go owns and can publish as an isolated snapshot.
_Avoid_: Immutable result

**Provider details**:
Typed provider-specific evidence published by an adapter without aliases to mutable runtime state.
_Avoid_: Metadata bag, raw metadata

**Generic typed output**:
A caller-selected Go value decoded or produced by an operation and returned with ordinary Go value semantics, including any reference fields.
_Avoid_: Deep-copied value, immutable output

**Validation decision**:
The validator result exactly as returned to `llmstep`, published as an isolated
snapshot. It is not made model-safe by the feedback sanitizer.
_Avoid_: Sanitized feedback

**Retry feedback**:
The sanitizer-owned, iteration-stamped feedback eligible for the next prompt
render, published separately from the validation decision.
_Avoid_: Validator output
