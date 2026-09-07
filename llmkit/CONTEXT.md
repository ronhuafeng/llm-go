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

**Observation**:
A toolkit-owned fact that may be unknown. The zero value is unknown and
does not manufacture a value. Requested settings, defaults, estimates,
heuristics, and inferred names cannot populate an observation.
_Avoid_: Zero sentinel, estimated usage, inferred name

**Provider details**:
Typed provider-specific evidence published by an adapter. It must not alias
mutable runtime state.
_Avoid_: Metadata bag, raw metadata

**Generic typed output**:
A caller-selected Go value decoded from model output with ordinary Go value
semantics. It is a typed proposition, not an accepted domain fact or authority.
_Avoid_: Deep-copied value, immutable output, accepted fact

**Judgment**:
The deterministic acceptance or rejection of a proposition, with optional
validator-owned findings, published exactly as returned. Absence of judgment
is a first-class state. It is not model-facing repair.
_Avoid_: Sanitized feedback, model judgment, shared feedback value

**Repair**:
Sanitizer-owned, iteration-stamped information eligible for a later prompt
render. It is a projection of judgment findings, not the judgment itself.
_Avoid_: Validator output, shared feedback value

**Attempt evidence**:
The stage-owned record of one bounded inference attempt, including any call,
decode, judgment, repair, or failure evidence actually obtained.
_Avoid_: Successful result only, trace metadata bag
