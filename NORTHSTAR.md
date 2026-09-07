# North stars

This document defines what each semantic owner in `llm-go` exists to preserve.

It is not an API inventory, package map, migration guide, or implementation
plan. Use it when two locally reasonable designs imply different answers to a
more fundamental question:

> What is the system allowed to claim, decide, or cause?

The project is built around one distinction:

> **Models propose. Programs decide. Effects require authority.**

An LLM is not an authority over facts, state, or effects. It is a probabilistic
interpreter that may produce useful semantic propositions. Reliable software
must keep the boundary between a model proposition and an accepted system fact
explicit.

## System model

The conceptual flow is:

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

Each transition has a different semantic owner. A layer may hide the mechanics
of the layer below it. It must not strengthen the meaning of the facts it
received.

In particular:

```text
requested       != effective
unknown         != zero
unreported      != absent
response        != truth
schema-valid    != semantically valid
proposed        != accepted
accepted        != authorized
authorized      != executed
```

No convenience API may erase these distinctions.

## Foundational principles

### P1. Models propose; programs decide

An LLM produces propositions. A proposition may be syntactically valid,
schema-valid, plausible, and still be wrong.

Model output does not become a domain fact merely because it has been decoded
into a Go value. Deterministic code owns acceptance and rejection.

The toolkit may execute caller-supplied deterministic judgment and preserve its
evidence. It does not own the caller's business meaning.

### P2. Types constrain shape, not truth

A Go type and its structured-output contract define what a model is allowed to
express. They do not prove that the expressed value is true.

```text
JSON-valid
    |
    v
schema-valid
    |
    v
Go value
```

The result is still only a typed proposition. Semantic validity requires
separate deterministic judgment. Authorization and state transitions require
additional application-owned rules.

### P3. Evidence may lose information but must never invent it

Projection between semantic layers is loss-aware. A higher layer may expose
fewer details than the layer beneath it. It may not manufacture facts to make
its representation appear complete.

For example:

- a requested model must not silently become an effective model;
- missing usage must not become zero usage;
- an account or thread total must not become call-attributable usage;
- inferred provider behavior must not become provider-observed evidence;
- partial observation must not become complete observation.

This is the evidence monotonicity rule:

> **Projection may reduce knowledge. It must not increase certainty.**

### P4. Unknown is a first-class state

Unknown is not an error in representation. It is often the most accurate
statement the system can make.

When a fact was not observed, was not reported, cannot be attributed, or cannot
be recovered, the representation must preserve that state.

Do not replace unknown with:

- zero;
- an empty string;
- a requested value;
- a configured default;
- an estimate;
- a heuristic;
- a provider-name inference.

Absence of evidence and evidence of absence are different facts.

### P5. Judgment is deterministic

The model must not decide whether its own proposition is acceptable. Acceptance
belongs to deterministic code.

A judgment may produce findings explaining why a proposition was rejected. Any
information sent back to the model for another attempt is a separate projection
of those findings.

Therefore:

```text
judgment evidence
        !=
model-facing repair feedback
```

The system must preserve that distinction.

### P6. Effects require explicit authority

A model proposition must never directly constitute authority to mutate
external state.

The path from model output to an effect must cross application-owned
deterministic rules.

```text
model proposition
      |
      X
      |
   side effect
```

Instead:

```text
model proposition
      |
      v
deterministic judgment
      |
      v
domain state
      |
      v
explicit authority
      |
      v
executor
```

Prompt wording is not a security boundary. Type and capability boundaries are.

### P7. Abstractions own semantics, not convenience

A public abstraction must exist because it owns durable semantics. Code reuse
alone is not enough.

A package should survive this deletion test:

> If the abstraction disappeared, would its semantic complexity be duplicated
> incorrectly across multiple real consumers?

If the answer is no, keep the mechanism internal or use ordinary Go code.

Do not create public abstractions merely to:

- shorten call sites;
- share a loop;
- centralize small helpers;
- avoid local duplication;
- create architectural symmetry.

Deep modules hide mechanics while preserving meaning. Shallow modules
redistribute mechanics without reducing conceptual cost.

## Core language

### Fact

A statement owned by the layer that can directly establish it.

Examples include:

- a protocol message received from Codex;
- a provider-reported effective model;
- an observed terminal event;
- a provider-reported token count.

Facts are not reconstructed from convenience defaults.

### Observation

Evidence obtained during an execution. An observation may be partial.
Execution failure does not erase facts that were already observed.

### Projection

A representation of lower-layer evidence in the vocabulary of a higher layer.
A projection may omit details. It must not create facts.

### Contract

A caller-defined restriction on the form of a model proposition.

For typed structured output, a contract may project to JSON Schema and decode
provider output into a Go value. The schema is a representation of the
contract. It is not the contract's full meaning.

```text
contract
    |
    +-- provider-facing schema
    +-- decoding rules
    +-- structural violation semantics
```

### Proposition

A typed semantic claim produced through model inference. A proposition is not a
fact merely because it satisfies a contract.

### Judgment

A deterministic decision about a proposition. A judgment may accept or reject
a proposition and may publish structured findings.

The application owns the meaning of its judgment rules.

### Repair feedback

A deliberately narrowed projection of rejection findings that is safe and
useful for a later model attempt.

Repair feedback is not the original judgment. A final rejected attempt has no
fictional repair feedback if no later attempt will consume it.

### Evidence

The attributable record needed to explain what occurred.

Evidence belongs to stages and semantic owners. A complete trace does not mean
every stage knows every fact. It means each stage publishes the facts it owns
without silently replacing unknowns.

### Authority

The right to perform or authorize a state transition or external effect. Model
output never creates authority by itself. Authority is application-owned.

### State

Durable application meaning produced by deterministic state transition. LLM
infrastructure may help interpret inputs into propositions. It does not own
application state semantics.

### Effect

An externally meaningful mutation or action.

Examples include:

- modifying a file;
- committing code;
- pushing a branch;
- deploying software;
- sending a message;
- deleting data.

Effects occur only through explicit application authority.

## LLM Toolkit

Module: `llmkit`. Language: [`llmkit/CONTEXT.md`](llmkit/CONTEXT.md).

### North star

**Convert probabilistic model inference into typed, attributable,
deterministically adjudicated propositions without granting the model state or
effect authority.**

The toolkit exists at the boundary between probabilistic inference and
deterministic software.

Its job is not to make an LLM authoritative. Its job is to make the uncertainty
and evidence around model inference explicit enough that normal Go programs can
reason about it safely.

The toolkit owns mechanisms such as:

```text
caller-defined contract
        |
        v
provider-neutral request
        |
        v
model observation
        |
        v
contract decode
        |
        v
typed proposition
        |
        v
deterministic judgment
        |
        v
bounded repair attempts
        |
        v
complete stage-owned evidence
```

A successful toolkit result means that the proposition passed the configured
contract and deterministic judgment. It does not mean the result is globally
true. It does not mean an application is authorized to act on it.

### The toolkit owns

The toolkit may own:

- compilation of caller-defined typed output contracts;
- provider-neutral invocation contracts;
- structural decoding and violations;
- provider-neutral execution evidence;
- deterministic validation orchestration;
- separation of judgment findings from model-facing repair feedback;
- bounded retry;
- attempt history;
- failure-stage attribution;
- preservation of partial evidence.

### The toolkit does not own

The toolkit does not own:

- provider SDKs;
- transport or credentials;
- application sessions;
- goals;
- task state;
- workflow graphs;
- memory;
- persistence;
- tool execution;
- authorization semantics;
- business rules;
- side-effect execution;
- provider capability registries;
- automatic provider fallback;
- model-selection heuristics;
- cost estimation from unobserved facts;
- prompt libraries;
- generic metadata bags.

The toolkit must not turn a model response into application authority.

## Codex SDK

Module: `codexsdk`. Language: [`codexsdk/CONTEXT.md`](codexsdk/CONTEXT.md).

### North star

**Expose exact, attributable Codex app-server facts while hiding transport
mechanics and refusing to invent a friendlier reality than the protocol
provides.**

The Codex SDK is not an LLM abstraction. It is the factual interface to one
Codex app-server.

The app-server and its generated protocol are the factual authority for Codex
behavior. The SDK exists to make those facts usable without replacing them
with handwritten approximations.

### The SDK owns

The SDK owns:

- process and transport lifecycle;
- JSON-RPC mechanics;
- generated protocol facts;
- exact request and response models;
- attributable run history;
- lifecycle observation;
- exact terminal facts;
- protocol admission rules;
- fail-closed handling of action-bearing protocol messages.

An Exact Run represents attributable Codex execution history. Observation APIs
observe that history. They do not create higher-level workflow meaning.

### The SDK does not own

The SDK does not own:

- provider-neutral LLM semantics;
- structured-output contracts for applications;
- application validation policy;
- retry policy for semantic propositions;
- workflow state;
- application authorization;
- provider-neutral evidence;
- a universal LLM `Client` interface.

The SDK must not copy protocol facts into friendlier handwritten models merely
to reduce apparent complexity.

If the protocol is complex, the SDK may hide transport complexity. It must
preserve protocol truth.

## Codex Adapter

Module: `llmcaller/codex`. Language:
[`llmcaller/codex/CONTEXT.md`](llmcaller/codex/CONTEXT.md).

### North star

**Join provider-neutral model inference with exact Codex facts through a
loss-aware projection, while preserving both sides of the boundary.**

The adapter is the only runtime join between `llmkit` and `codexsdk`.

Its responsibility is translation, not semantic ownership transfer.

```text
llmkit request
      |
      v
Codex-specific request assembly
      |
      v
exact codexsdk execution
      |
      +-- complete typed Codex details
      |
      v
loss-aware neutral projection
      |
      v
llmkit observation
```

The adapter may hide how a toolkit request is implemented through Codex. It
must not hide what Codex actually did.

### The adapter owns

The adapter owns:

- Codex-specific request assembly;
- Codex-specific schema admission;
- mapping toolkit contracts to Codex mechanisms;
- execution through exact Codex lifecycle;
- projection into provider-neutral execution evidence;
- typed Codex provider details;
- Codex-specific policy required to perform that projection.

### The adapter does not own

The adapter does not own:

- general contract compilation;
- general deterministic judgment;
- retry orchestration;
- Codex transport;
- generated protocol facts;
- application workflow state;
- application authority;
- application side effects.

Provider-neutral evidence must contain only facts that survive exact semantic
projection. When exact projection is impossible, the neutral field remains
unknown and the complete typed Codex details remain available.

## Repository

Path: `github.com/ronhuafeng/llm-go`. Invariants: [`DESIGN.md`](DESIGN.md).

### North star

**Keep independently publishable semantic truths close enough to evolve
together without collapsing their authority boundaries.**

The repository contains multiple modules because they solve one composed
problem but own different truths.

They share:

- source review;
- integration testing;
- release coordination;
- compatibility evidence;
- design language.

They do not share runtime semantic ownership.

The repository root is orchestration and governance. It must not become a
fourth runtime layer.

## Ownership graph

```text
                  application
                      |
                      | defines domain meaning,
                      | judgment and authority
                      v
                   llmkit
                      ^
                      | provider-neutral inference
                      | and evidence
                      |
              llmcaller/codex
               ^             ^
               |             |
    toolkit contract         | exact Codex facts
               |             |
               |          codexsdk
               |             |
               |             v
               |       Codex app-server
               |
               +---- runtime join
```

Dependency constraints:

```text
llmkit         ---\
                   +--> llmcaller/codex
codexsdk       ---/

llmkit         <-X-> codexsdk

application    ---> llmkit
application    ---> adapter when Codex is selected

public modules -X-> repository internal tools
```

There is no runtime `common`, `shared`, `core`, or `types` owner.

If two modules need similar source code but the semantics belong to different
owners, duplication is preferable to false ownership.

## Epistemic boundaries

The following promotions are prohibited unless the receiving layer has new
evidence or explicit deterministic authority.

### Request is not execution

```text
RequestedModel
    X
EffectiveModel
```

Only execution evidence may establish the effective model.

### Missing is not zero

```text
not observed
    X
0
```

Measured zero and unknown are different states.

### Model output is not fact

```text
typed model output
    X
accepted domain event
```

A deterministic judgment boundary must exist.

### Acceptance is not authority

```text
accepted interpretation
    X
permission to mutate
```

Application authorization rules decide authority.

### Authority is not execution

```text
authorized action
    X
successful effect
```

Execution produces its own evidence.

### Provider detail is not neutral evidence

```text
provider-specific observation
        |
        v
semantic projection
        |
        v
provider-neutral evidence
```

Only facts with a sound provider-neutral meaning cross the projection boundary.
Everything else remains typed provider detail.

## Failure philosophy

Failure is not absence of information.

An operation can fail after producing useful evidence. The system should
preserve every fact observed before the failure.

For example:

```text
request succeeded
call started
provider identity observed
model reroute observed
terminal output missing
```

The result is not "nothing happened." It is a failed operation with partial
evidence.

Likewise:

```text
model response obtained
contract decode succeeded
deterministic judgment rejected
retry limit exhausted
```

The final proposition and rejection evidence remain real observations even
though no accepted result exists.

Errors and evidence are orthogonal.

## Retry philosophy

Retry is not proof of convergence.

A bounded retry loop is a controlled sequence of new propositions.

```text
proposition 1
    |
    v
reject
    |
    v
repair projection
    |
    v
proposition 2
    |
    v
reject
    |
    v
repair projection
    |
    v
proposition 3
```

The toolkit must not claim mathematical stabilization merely because later
attempts exist.

Retry exists to give a probabilistic system another bounded opportunity to
satisfy a deterministic contract. The retry bound is part of system authority.
The model does not decide whether it receives another attempt.

## State and workflow philosophy

`llm-go` does not own generic workflow semantics.

An application may implement:

```text
user message
    |
    v
typed proposition
    |
    v
deterministic reducer
    |
    v
application state
    |
    v
authorized command
    |
    v
effect
```

That does not justify adding generic concepts such as:

```text
Session
Goal
Task
Plan
Approval
Permission
Command
Workflow
Memory
Agent
```

to the toolkit.

These concepts belong to an application until multiple real consumers
demonstrate a shared semantic law that cannot be expressed clearly with
ordinary Go.

Ordinary reducers are preferable to speculative workflow frameworks.

## Public abstraction rule

Before adding a public package, interface, framework, or shared type, ask:

1. What semantic fact does it own?
2. Which layer is authoritative for that fact?
3. What invalid state does this abstraction make harder to represent?
4. What important complexity does it hide?
5. What evidence does it preserve?
6. What would be duplicated incorrectly if the abstraction were deleted?
7. Does it accidentally promote a proposition into fact or authority?
8. Could ordinary Go express the same domain meaning more clearly?

If these questions do not have strong answers, do not add the abstraction.

## Walk together

The modules stay in one repository because their boundaries need continuous
verification.

A provider-neutral contract change may reveal an adapter projection problem. A
Codex protocol change may reveal that a neutral field was stronger than the
provider can prove. An adapter change may reveal a missing distinction in
toolkit evidence.

These changes must be visible to each other. Visibility does not imply shared
ownership.

```text
typed application proposition
    <- llmkit hides inference mechanics,
       preserves contract, judgment and attempt evidence

        <- adapter hides Codex request assembly,
           preserves neutral projection and exact provider details

            <- codexsdk hides process and transport mechanics,
               preserves exact protocol facts

                <- Codex app-server remains the Codex fact authority
```

Each layer may simplify mechanics. No layer may simplify away epistemic
boundaries.

## Evolution rule

When a new design is easier for callers but weakens the distinction between:

- known and unknown;
- observed and inferred;
- proposed and accepted;
- accepted and authorized;
- requested and effective;
- neutral evidence and provider detail;

reject the design.

When a new abstraction makes these distinctions clearer and removes duplicated
semantic machinery, prefer it even if migration is expensive.

When compatibility and semantic correctness conflict before a stable
major-version contract exists, prefer semantic correctness.

When a historical abstraction no longer has an independent semantic owner,
delete it.

The project should become smaller as its model becomes clearer.

## Destination

The destination of `llm-go` is not a universal LLM framework.

It is not an agent framework. It is not a workflow engine. It is not a provider
abstraction that pretends all providers are equivalent.

It is a set of sharply separated semantic owners for building reliable software
around probabilistic model inference.

The final invariant is:

> **A model may suggest what the world means. Only evidence may establish what
> was observed. Only deterministic programs may decide what the system accepts.
> Only explicit authority may change the world.**
