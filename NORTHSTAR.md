# North stars

This document defines what each semantic owner in `llm-go` exists to preserve.
Use it when two locally reasonable designs imply different answers to:

> What is the system allowed to claim, decide, or cause?

The project is built around one rule:

> **Models propose. Programs decide. Effects require authority.**

An LLM is a probabilistic interpreter, not an authority over facts, state,
policy, or effects. Reliable software keeps every promotion between those
meanings explicit and keeps each decision with the owner that can justify it.

## System model

```text
reality / provider
        |
        | observation
        v
provider facts
        |
        | loss-aware projection
        v
provider-neutral evidence
        |
        | constrained inference
        v
typed proposition
        |
        | deterministic judgment
        v
accepted domain fact or event
        |
        | deterministic state transition
        v
application state
        |
        | explicit authority
        v
effect
```

A layer may hide mechanics below it. It must not strengthen the meaning of
what it received, substitute its own policy for an application decision, or
turn a convenience representation into a stronger fact.

```text
requested                 != effective
unknown                   != zero
unreported                != absent
schema optionality        != absent observation
empty observed value      != absent observation
response                  != truth
schema-valid              != semantically valid
representation adaptation != semantic contract mutation
execution backend         != model provider
model identifier          != provider identity
selected or routed model  != served model
aggregate usage           != per-attempt usage
generated exactness       != runtime compatibility
protocol observation      != convenience projection
proposed                  != accepted
preserved proposition     != accepted result
accepted                  != authorized
authorized                != executed
read-only                 != confidential
effect-safe               != disclosure-safe
ephemeral                 != provider-retention-disabled
```

## Principles

### P1. Models propose; programs decide

Model output is a proposition, even after successful JSON parsing, schema
validation, and Go decoding. Deterministic code owns semantic acceptance and
rejection.

A rejected proposition may remain available as attempt evidence. Preserving it
does not promote it into an accepted result. Public result slots whose meaning
is "accepted output" are populated only after deterministic acceptance.

### P2. Types constrain shape; caller contracts keep their meaning

A caller-defined type constrains what the model may express. Structured-output
schema and decoding enforce that contract; they do not prove the resulting
value true or authorize an application to act on it.

The caller owns the semantic contract. A provider adapter may encode a
semantics-preserving representation or reject a contract the provider cannot
represent. It must not make the caller's accepted instance set stronger or
weaker merely to obtain provider acceptance. Representation compatibility is
not authority to rewrite application meaning.

### P3. Evidence never gains facts through projection

Projection may reduce knowledge. It must not increase certainty. Requested
values, defaults, heuristics, estimates, adapter identity, model names, and
provider-name inference must not fill facts that were not observed. Unknown is
a valid first-class result.

Presence is part of evidence. An observed zero or empty value is not absence
unless the owning protocol or contract defines that equivalence. A convenience
projection may omit or reshape exact evidence, but it must not rewrite the
lower-layer terminal status, presence, identity, provenance, or measurement
scope that produced it.

Schema optionality describes which observations are legal, not whether a field
was absent in a concrete observation. When an optional correlation, identity,
or measurement field is present, that presence is evidence. A projection must
not erase it merely because another legal message of the same schema could omit
it.

Execution-backend identity and model-provider identity are different facts. A
model identifier does not by itself establish provider identity. Each may be
published only by the layer that can directly prove it; otherwise it remains
unknown.

Model selection and routing are also distinct from model service. A requested,
thread-selected, effective, or rerouted model identifier proves only the fact
its owning observation actually states. It must not be promoted to "the model
that served this inference" without attributable serving evidence.

Serving evidence also has scope. A server-reported model for one provider
response does not by itself establish one served model for a higher-layer
attempt that may contain multiple provider responses. Projection preserves or
weakens that scope; it does not silently widen a response-scoped fact into an
attempt-wide identity.

Measurement scope is part of evidence. Thread-total, session-total, cumulative,
last-request, turn, and per-attempt usage are different claims. A projection may
publish a measurement only under a scope justified by the lower-layer
observation; it must not relabel an aggregate as a narrower measurement merely
because that shape is convenient.

### P4. Failure does not erase observation

A failed operation may still have attributable facts. Preserve evidence already
obtained instead of converting failure into an empty result or discarding
partial observation to simplify an API.

Failure also does not authorize semantic synthesis. If a required fact,
decision, or application-owned response was not obtained, a lower layer may
fail closed; it must not manufacture a successful semantic answer in its place.

### P5. Judgment is deterministic; retry disclosure is application-owned

The model does not judge its own proposition. A deterministic validator may
publish findings. Information eligible for a later model attempt is a separate,
explicit projection of those findings, not the judgment itself.

The toolkit owns that separation, bounded retry orchestration, iteration, and
attempt evidence. The application owns what finding content may cross the model
boundary: disclosure, redaction, secret handling, pseudonymization, and content
classification are application policy. Raw validator findings do not become
model input implicitly, and the toolkit does not become a DLP or content-policy
engine by providing the projection seam.

The accepted result is another distinct semantic state. Retry exhaustion,
validation failure, or rejection may preserve the latest proposition inside
attempt evidence, but none of those states may populate an accepted-result
projection without a positive deterministic judgment.

### P6. Effects and execution policy require application authority

Model output never grants mutation authority by itself. Application-owned rules
must establish authority before files, repositories, deployments, messages, or
other external state can change. Prompt wording is not a security boundary.

Concrete execution policy also belongs to the application: approval policy,
sandbox policy, permission grants, allowed execution modes, and equivalent
choices are not transferred to an SDK or adapter merely because that layer
provides a callback, profile-shaped input, or admission hook. Lower layers may
expose exact facts and fail-closed admission mechanisms; caller-owned policy
uses those mechanisms to allow or reject continuation.

An admission boundary must cover every model-directed continuation path whose
execution depends on application-owned policy after exact facts become
available. Start, resume, re-entry, retry, or another lifecycle spelling does
not bypass the need for the same authority boundary. The mechanism may differ
by protocol path; policy ownership does not.

When a protocol asks for application-owned data or authority, an SDK may deliver
the exact request and encode a caller-supplied typed response. If the application
does not supply that response, the absence remains absence or failure; the SDK
must not synthesize a successful approval, denial, user answer, permission,
environment fact, or equivalent semantic decision merely to continue.

Effect safety and confidentiality are different properties. A read-only,
never-approve, or ephemeral execution choice can constrain mutation without
proving that an allowed read stays confidential, that provider-bound context
excludes secrets, or that the provider retains nothing. Application-owned CWD,
workspace, input selection, and disclosure policy remain part of the
confidentiality boundary unless a separate proof exists.

### P7. Public abstractions own semantics, not convenience

A public abstraction must protect durable meaning or correctness. Code reuse,
symmetry, future possibilities, and shorter call sites do not justify a new
runtime owner. If deleting an abstraction does not force important semantics to
be reimplemented incorrectly by real consumers, keep it internal or use
ordinary Go.

Public names should describe the semantic owner they actually represent. A
caller-owned projection hook should not be named as though a shared library
owns sanitization, safety, or content policy.

## Shared semantics

**Fact** — a statement established by the layer that can directly prove it.

**Observation** — attributable evidence obtained during execution; it may be
partial or absent. Presence is distinct from the observed value.

**Projection** — a representation of lower-layer evidence in higher-layer
vocabulary. It may omit facts but must not create them or rewrite their
presence, identity, provenance, terminal meaning, or measurement scope.

**Contract** — a caller-defined restriction on the form of a model proposition.
A JSON Schema is one provider-facing representation of that contract. Provider
adaptation preserves its semantics or rejects it.

**Proposition** — a typed semantic claim produced through model inference. It
is not a domain fact merely because it satisfies a contract.

**Judgment** — a deterministic acceptance or rejection of a proposition, with
optional validator-owned findings.

**Accepted result** — a proposition promoted by a positive deterministic
judgment into the result state promised by the higher-level API. Rejected or
unjudged propositions remain attempt evidence, not accepted results.

**Retry feedback** — application-projected information explicitly eligible for
a later model attempt. It is not the judgment and does not inherit validator
content automatically.

**Admission** — a mechanism boundary where caller-owned policy may allow or
reject continuation using observed facts. The existence of the mechanism does
not transfer policy ownership to the layer that implements it.

**Execution backend** — the runtime or adapter through which an inference is
executed. It is not evidence of the actual model provider.

**Model provider** — the provider identity directly established by attributable
lower-layer evidence. Model names, backend type, credentials, URLs, or requested
configuration do not establish it by inference.

**Served model** — the model identifier directly established as having served
the relevant inference. Requested, selected, effective, or routed model facts
are not equivalent unless the owning protocol explicitly establishes that
semantics.

**Measurement scope** — the execution interval or aggregation boundary to which
an observed measurement applies. Projection preserves this scope or publishes a
weaker claim; it does not silently narrow it.

**Authority** — the application-owned right to cause a state transition or
external effect.

**Effect** — an externally meaningful mutation or action. Execution produces
its own evidence; authorization is not proof of success.

Module-specific vocabulary belongs in the owning module's `CONTEXT.md`.

## LLM Toolkit

Module: `llmkit`. Language: [`llmkit/CONTEXT.md`](llmkit/CONTEXT.md).

**North star: convert probabilistic model inference into typed, attributable,
deterministically adjudicated propositions without granting the model state,
policy, or effect authority.**

The toolkit owns provider-neutral contracts, structured decoding, neutral
execution evidence, deterministic validation orchestration, the explicit
judgment-to-retry-feedback boundary, bounded repair attempts, failure-stage
attribution, and attempt evidence. Neutral observations preserve presence and
measurement scope; accepted-result state is distinct from retained proposition
evidence.

It does not own provider SDKs, transport, credentials, application state,
workflow semantics, authorization, side-effect execution, execution safety
policy, retry-feedback disclosure/redaction/content policy, provider capability
registries, provider inference heuristics, automatic fallback, prompt libraries,
or business rules.

A successful toolkit result means that a proposition satisfied the configured
contract and judgment. It does not mean the proposition is globally true or
that an application is authorized to act on it. A rejected or unjudged
proposition may remain in attempt evidence without becoming the successful
result.

## Codex SDK

Module: `codexsdk`. Language: [`codexsdk/CONTEXT.md`](codexsdk/CONTEXT.md).

**North star: expose exact, attributable Codex app-server facts while hiding
transport mechanics and refusing to invent a friendlier reality than the
protocol provides.**

The app-server and generated protocol are the factual authority for Codex
behavior. The SDK owns process and transport lifecycle, generated protocol
facts, exact request/response models, attributable Exact Run history, terminal
observation, typed server-request delivery/response encoding, and protocol
admission.

Generated exactness requires generator fidelity. Once a schema shape is part of
the generated exact surface, an unsupported generator shape fails generation;
it is not silently represented by dropping known members, union variants, or
presence distinctions.

Lifecycle composition must not create a policy bypass. Whenever a start,
resume, re-entry, or equivalent Exact Run path obtains an exact observation and
then proceeds into a model-directed stage governed by application policy, the
SDK exposes the caller-owned admission seam before that continuation.

It does not own provider-neutral LLM semantics, application contracts,
validation policy, repair policy, workflow state, application execution policy,
authorization, application-owned server-request answers, or a universal LLM
client interface. Missing application-owned data or authority remains missing
or fails closed; the SDK does not manufacture semantic success.

The SDK may hide stdio and JSON-RPC mechanics. It must preserve protocol truth.
A convenience projection over an exact result may not redefine observed status
or collapse presence. Checked-in generated exactness is not runtime
compatibility: a successful call or live smoke does not prove the generated
surface is compatible with the connected app-server.

## Codex Adapter

Module: `llmcaller/codex`. Language:
[`llmcaller/codex/CONTEXT.md`](llmcaller/codex/CONTEXT.md).

**North star: join provider-neutral inference with exact Codex facts through a
loss-aware, semantics-preserving projection while preserving both sides of the
boundary.**

The adapter is the only runtime join between `llmkit` and `codexsdk`. It owns
Codex request assembly, Codex-specific representation/schema admission,
wiring caller-owned execution admission into every Exact Run lifecycle path it
exposes, execution through that lifecycle, and projection into toolkit-owned
provider-neutral evidence.

Schema adaptation must preserve the caller-owned contract's accepted instance
language; if Codex cannot represent that contract without semantic mutation,
the adapter rejects it before execution.

Exact typed Codex details remain available when a neutral projection would lose
meaning. If a neutral fact cannot be established soundly, it remains unknown.
Adapter/backend identity does not establish model-provider identity. Exact final
response presence must remain distinguishable after neutral projection. A
thread-start model or reroute target is not published as an attempt-wide
served-model fact unless exact lower-layer evidence establishes that serving
meaning at that scope. Response-scoped serving evidence remains response-scoped
when one neutral attempt may contain multiple provider responses. Usage is
published only under a neutral scope supported by the exact observation.

The adapter does not own general contract compilation, general judgment or
repair orchestration, Codex transport or generated protocol facts, application
state, named application safety profiles, approval/sandbox/permission policy,
confidentiality/disclosure policy, authorization, or side effects. It supplies
mechanisms and exact facts to application-owned policy; it is not the policy
authority.

## Repository

Path: `github.com/ronhuafeng/llm-go`. Live invariants:
[`DESIGN.md`](DESIGN.md).

**North star: keep independently publishable semantic truths close enough to
evolve together without collapsing their authority boundaries.**

The current runtime ownership graph is:

```text
llmkit         ---\
                   +--> llmcaller/codex
codexsdk       ---/

llmkit         <-X-> codexsdk
public modules -X-> internal/tools
```

The root is orchestration and governance, not a fourth runtime owner. A runtime
`common`, `shared`, `core`, or `types` module would create false ownership and
is prohibited. Similar implementation may be duplicated when the semantics
belong to different owners.

The modules share review, CI, release coordination, and compatibility evidence
because their boundaries must evolve in sight of each other. Shared repository
location does not transfer semantic authority.

Source evolution and publication are different observations. Pre-v1 modules may
change together in one source cohort, including a downstream manifest naming the
next upstream version before that tag exists. Publication remains
dependency-ordered: a dependent module is not publishable until its committed
dependency closure exists and resolves without workspace repair.

## Design test

Before accepting a design, ask:

1. Which semantic owner can directly prove the fact being represented?
2. Does any projection turn unknown, requested, inferred, selected, routed,
   proposed, or authorized state into a stronger claim without new evidence?
3. Does representation adaptation change the caller-owned contract instead of
   preserving it or rejecting it?
4. Does a library mechanism make an application-owned disclosure, execution,
   approval, permission, or authority decision merely because it has a hook
   where that decision could be made?
5. Does every model-directed lifecycle continuation expose application-owned
   admission before execution when policy depends on newly observed facts?
6. Does a convenience projection collapse presence, narrow measurement scope,
   erase a present optional correlation, or rewrite an exact protocol status,
   identity, or provenance fact?
7. Does a rejected or unjudged proposition get promoted into an accepted-result
   slot merely because preserving it is useful?
8. Does model output cross into acceptance, state transition, or effect without
   deterministic application authority?
9. Does each persistent abstraction protect a current invariant that would be
   materially harder to preserve without it?
10. Can the common change path reach the canonical authority without reading a
    second copy of the same fact?

When convenience conflicts with these answers, reject the convenience.

The executable proof of `accepted != authorized` and
`authorized != executed` is
[`internal/tools/integration/example_authority_to_effect_test.go`](internal/tools/integration/example_authority_to_effect_test.go).
It reuses `llmstep` judgment and keeps authority and effect types local
to the example.
