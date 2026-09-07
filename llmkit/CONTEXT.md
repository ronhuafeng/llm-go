# llmkit

Module-local vocabulary for provider-neutral typed model inference. Repository
semantics and authority boundaries are defined in [`NORTHSTAR.md`](../NORTHSTAR.md).

## Language

**Toolkit-owned state**:
Provider-neutral request, result, or attempt state whose representation this
module owns and can publish as an isolated snapshot.
_Avoid_: Immutable result

**Execution evidence**:
Provider-neutral facts attributable to one model call. Unknown facts remain
unknown; provider-specific facts stay in Provider details.
_Avoid_: Requested settings, estimated usage, metadata bag

**Provider details**:
Typed provider-specific evidence published by an adapter. It must not alias
mutable runtime state.
_Avoid_: Metadata bag, raw metadata

**Generic typed output**:
A caller-selected Go value decoded from model output with ordinary Go value
semantics. It is a typed proposition, not an accepted domain fact or authority.
_Avoid_: Deep-copied value, immutable output, accepted fact

**Validation decision**:
The deterministic validator result published exactly as returned. It is not
model-facing retry feedback.
_Avoid_: Sanitized feedback, model judgment

**Retry feedback**:
Sanitizer-owned, iteration-stamped information eligible for a later prompt
render. It is a projection of validation findings, not the original decision.
_Avoid_: Validator output

**Attempt evidence**:
The stage-owned record of one bounded inference attempt, including any call,
decode, validation, retry-feedback, or failure evidence actually obtained.
_Avoid_: Successful result only, trace metadata bag
