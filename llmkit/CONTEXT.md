# llmkit

Provider-neutral typed LLM operations and the evidence they publish to callers.

## Language

**Toolkit-owned state**:
Provider-neutral request or result state whose representation this module owns
and can publish as an isolated snapshot.
_Avoid_: Immutable result

**Provider details**:
Typed provider-specific evidence published by an adapter. It must not alias
mutable runtime state.
_Avoid_: Metadata bag, raw metadata

**Generic typed output**:
A caller-selected Go value with ordinary Go value semantics.
_Avoid_: Deep-copied value, immutable output

**Validation decision**:
The validator result published exactly as returned. It is not retry feedback.
_Avoid_: Sanitized feedback

**Retry feedback**:
Sanitizer-owned, iteration-stamped text eligible for the next prompt render.
_Avoid_: Validator output

**Settle loop**:
Bounded retry owned by `settle` until a terminal validation decision or error.
_Avoid_: Adapter retry

**Schema decode**:
Compilation of a caller type into JSON Schema, then decode of generic typed
output.
_Avoid_: Provider schema
