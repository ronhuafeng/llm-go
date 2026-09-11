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
unknown; backend-specific facts stay in Backend details. Backend identity,
model identity, and model-provider identity are separate facts and are not
inferred from one another.
_Avoid_: Requested settings, estimated usage, metadata bag, inferred provider

**Observation**:
A toolkit-owned fact that may be unknown. The zero value is unknown and
does not manufacture a value. Presence is distinct from an observed zero or
empty value. Requested settings, defaults, estimates, heuristics, adapter names,
model identifiers, and inferred provider names cannot populate an observation.
_Avoid_: Zero sentinel, empty-as-absence, estimated usage, inferred name

**Execution backend**:
The runtime or adapter through which an inference executes, when that identity
is itself an attributable fact. `ExecutionEvidence.BackendName` and
`BackendDetails.BackendName()` refer to this identity. It does not establish the
model provider.
_Avoid_: Provider name, model provider

**Model provider**:
The provider identity directly established by attributable lower-layer evidence.
`ExecutionEvidence.ProviderName` is a presence-aware Observation and remains
unknown when only the backend or model identifier is known.
_Avoid_: Adapter name, model-name heuristic, requested provider

**Backend details**:
Typed backend-specific evidence published by an adapter. It must not alias
mutable runtime state. Its backend identity must agree with neutral execution
backend identity, but it does not establish model-provider identity.
_Avoid_: Provider identity, metadata bag, raw metadata

**Inference capability**:
A provider-neutral `Caller` that obtains a typed proposition and evidence.
It does not grant mutation authority. Prompt is not authority. An
implementation may satisfy `Caller` only when any model-directed execution
reachable through that implementation is already effect-free or independently
authorized outside the model request.
_Avoid_: Tool grant, write gate, permission token

**Generic typed output**:
A caller-selected Go value decoded from model output with ordinary Go value
semantics. It is a typed proposition, not an accepted domain fact or authority.
_Avoid_: Deep-copied value, immutable output, accepted fact

**Judgment**:
The deterministic acceptance or rejection of a proposition, with optional
validator-owned findings, published exactly as returned. Absence of judgment
is a first-class state. It is not model-facing retry feedback.
_Avoid_: Sanitized feedback, model judgment, shared feedback value

**Repair / retry feedback**:
Application-projected, iteration-stamped information explicitly eligible for a
later prompt render. `llmstep` owns the separation from Judgment, bounded retry
orchestration, and framework iteration. The application owns what finding
content is disclosed, redacted, pseudonymized, classified, or omitted. Raw
validator findings are not model-facing by default.
_Avoid_: Validator output, toolkit content policy, default secret scanner,
shared feedback value

**Attempt evidence**:
The stage-owned record of one bounded inference attempt, including any call,
decode, judgment, retry-feedback, or failure evidence actually obtained.
_Avoid_: Successful result only, trace metadata bag

**Contract**:
One compiled provider-neutral structured-output type. It owns the exact
schema JSON used for a request and the compiled validator used to decode
matching responses. Compile once per owned inference definition; reuse
across decode and bounded retries. The zero value is uncompiled and does
not guess a schema. A provider representation may reject this contract but
must not silently change its accepted instance language.
_Avoid_: Global schema cache, provider dialect, semantic validator
