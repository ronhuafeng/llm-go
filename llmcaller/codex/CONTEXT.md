# Codex Adapter

Module-local vocabulary for the Codex-specific join between `llmkit` and
`codexsdk`. Repository semantics and authority boundaries are defined in
[`NORTHSTAR.md`](../../NORTHSTAR.md). Toolkit-owned evidence vocabulary belongs
in [`llmkit/CONTEXT.md`](../../llmkit/CONTEXT.md).

## Language

**Execution admission**:
The adapter mechanism that wires caller/application-owned policy to approval,
sandbox, ephemeral, permission, or other exact facts observed before a
model-directed continuation. The adapter preserves and presents those facts;
the caller owns the rule that accepts or rejects them. Every Exact Run lifecycle
path exposed by the adapter must preserve this boundary; start, resume, re-entry,
or another spelling does not create an admission bypass.
_Avoid_: Adapter safety profile, requested settings, SDK validation,
confidentiality guarantee, start-only gate

**Effective execution observation**:
Approval, sandbox, ephemeral, permission, or related execution facts actually
observed from Codex. These facts describe execution; they are not themselves an
application policy verdict and do not prove confidentiality, disclosure safety,
data-loss prevention, or provider retention.
_Avoid_: Requested profile, application authorization, confidentiality guarantee

**Terminal observation**:
A completed or partial Exact Run after any caller-owned execution admission has
been applied. Exact server status and presence remain exact even when a
higher-level inference projection cannot produce its own required convenience
result.
_Avoid_: Result snapshot, adapter-rewritten protocol status

**Exact snapshot**:
Isolated exact Codex facts published without converting application policy into
a provider fact. Neutral facts are projected independently of this snapshot.
_Avoid_: Validated result, policy verdict

**Neutral final response projection**:
The loss-aware projection of exact final-answer text. If the Exact Run can
distinguish absent final answer from observed empty final answer, the neutral
projection preserves that presence distinction rather than collapsing both to a
scalar empty string.
_Avoid_: Empty-as-absence, scalar-only final response

**Served model projection**:
A neutral served-model observation published only from exact lower-layer
evidence that establishes serving semantics for the relevant inference. A
requested model, thread-start model, effective selection, or reroute target is a
routing/configuration fact unless the protocol explicitly establishes that it
served the inference.
_Avoid_: Requested model, thread-start model, route target, inferred served model

**Usage projection**:
A neutral token-accounting projection whose scope is supported by exact Codex
usage evidence. Thread-total, cumulative, last-request, turn, and per-attempt
measurements are not interchangeable; the adapter preserves or weakens scope
rather than silently narrowing it.
_Avoid_: Re-scoped total, inferred per-call usage, billing estimate

**Schema admission**:
Adapter-owned Codex representation/dialect admission for caller-owned output
schemas. The adapter may preserve a representable contract or reject an
unrepresentable one before execution. It does not strengthen or weaken the
caller's accepted JSON instance language merely to gain Codex acceptance.
_Avoid_: Semantic schema rewrite, SDK schema validation, llmkit contract
compilation

**Backend identity**:
The attributable, non-empty fact that execution used the Codex adapter/runtime.
It does not establish the upstream model-provider identity. Provider identity
remains unknown unless exact lower-layer evidence establishes it independently.
_Avoid_: Provider identity, model-name inference, empty backend label
