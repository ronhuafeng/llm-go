# North stars

This document defines what each semantic owner in `llm-go` exists to preserve.
Use it when two locally reasonable designs imply different answers to:

> What is the system allowed to claim, decide, or cause?

The project is built around one rule:

> **Models propose. Programs decide. Effects require authority.**

An LLM is a probabilistic interpreter, not an authority over facts, state, or
effects. Reliable software keeps each promotion between those meanings
explicit.

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
what it received.

```text
requested       != effective
unknown         != zero
unreported      != absent
response        != truth
schema-valid    != semantically valid
generated exactness != runtime compatibility
proposed        != accepted
accepted        != authorized
authorized      != executed
read-only       != confidential
effect-safe     != disclosure-safe
ephemeral       != provider-retention-disabled
```

## Principles

### P1. Models propose; programs decide

Model output is a proposition, even after successful JSON parsing, schema
validation, and Go decoding. Deterministic code owns semantic acceptance and
rejection.

### P2. Types constrain shape, not truth

A caller-defined type constrains what the model may express. Structured-output
schema and decoding enforce that contract; they do not prove the resulting
value true or authorize an application to act on it.

### P3. Evidence never gains facts through projection

Projection may reduce knowledge. It must not increase certainty. Requested
values, defaults, heuristics, estimates, and provider-name inference must not
fill facts that were not observed. Unknown is a valid first-class result.

### P4. Failure does not erase observation

A failed operation may still have attributable facts. Preserve evidence already
obtained instead of converting failure into an empty result or discarding
partial observation.

### P5. Judgment is deterministic and repair is a projection

The model does not judge its own proposition. A deterministic validator may
publish findings. Information sent to a later model attempt is a separate,
explicitly narrowed projection of those findings, not the judgment itself.

### P6. Effects require explicit authority

Model output never grants mutation authority by itself. Application-owned rules
must establish authority before files, repositories, deployments, messages, or
other external state can change. Prompt wording is not a security boundary.

Effect safety and confidentiality are different properties. A read-only,
never-approve, or ephemeral execution profile can constrain mutation without
proving that an allowed read stays confidential, that provider-bound context
excludes secrets, or that the provider retains nothing. Application-owned
CWD, workspace, and input selection remain part of the confidentiality
boundary unless a separate proof exists.

### P7. Public abstractions own semantics, not convenience

A public abstraction must protect durable meaning or correctness. Code reuse,
symmetry, future possibilities, and shorter call sites do not justify a new
runtime owner. If deleting an abstraction does not force important semantics to
be reimplemented incorrectly by real consumers, keep it internal or use
ordinary Go.

## Shared semantics

**Fact** — a statement established by the layer that can directly prove it.

**Observation** — attributable evidence obtained during execution; it may be
partial.

**Projection** — a representation of lower-layer evidence in higher-layer
vocabulary. It may omit facts but must not create them.

**Contract** — a caller-defined restriction on the form of a model proposition.
A JSON Schema is one provider-facing projection of that contract.

**Proposition** — a typed semantic claim produced through model inference. It
is not a domain fact merely because it satisfies a contract.

**Judgment** — a deterministic acceptance or rejection of a proposition, with
optional structured findings.

**Authority** — the application-owned right to cause a state transition or
external effect.

**Effect** — an externally meaningful mutation or action. Execution produces
its own evidence; authorization is not proof of success.

Module-specific vocabulary belongs in the owning module's `CONTEXT.md`.

## LLM Toolkit

Module: `llmkit`. Language: [`llmkit/CONTEXT.md`](llmkit/CONTEXT.md).

**North star: convert probabilistic model inference into typed, attributable,
deterministically adjudicated propositions without granting the model state or
effect authority.**

The toolkit owns provider-neutral contracts, structured decoding, neutral
execution evidence, deterministic validation orchestration, bounded repair
attempts, failure-stage attribution, and attempt evidence.

It does not own provider SDKs, transport, credentials, application state,
workflow semantics, authorization, side-effect execution, provider capability
registries, automatic fallback, prompt libraries, or business rules.

A successful toolkit result means that a proposition satisfied the configured
contract and judgment. It does not mean the proposition is globally true or
that an application is authorized to act on it.

## Codex SDK

Module: `codexsdk`. Language: [`codexsdk/CONTEXT.md`](codexsdk/CONTEXT.md).

**North star: expose exact, attributable Codex app-server facts while hiding
transport mechanics and refusing to invent a friendlier reality than the
protocol provides.**

The app-server and generated protocol are the factual authority for Codex
behavior. The SDK owns process and transport lifecycle, generated protocol
facts, exact request/response models, attributable Exact Run history, terminal
observation, and protocol admission.

It does not own provider-neutral LLM semantics, application contracts,
validation policy, repair policy, workflow state, authorization, or a universal
LLM client interface.

The SDK may hide stdio and JSON-RPC mechanics. It must preserve protocol truth.
Checked-in generated exactness is not runtime compatibility: a successful
call or live smoke does not prove the generated surface is compatible with
the connected app-server.

## Codex Adapter

Module: `llmcaller/codex`. Language:
[`llmcaller/codex/CONTEXT.md`](llmcaller/codex/CONTEXT.md).

**North star: join provider-neutral inference with exact Codex facts through a
loss-aware projection while preserving both sides of the boundary.**

The adapter is the only runtime join between `llmkit` and `codexsdk`. It owns
Codex request assembly, Codex-specific schema admission and profile policy,
execution through the exact SDK lifecycle, and projection into toolkit-owned
provider-neutral evidence.

Exact typed Codex details remain available when a neutral projection would lose
meaning. If a neutral fact cannot be established soundly, it remains unknown.

The adapter does not own general contract compilation, general judgment or
repair orchestration, Codex transport or generated protocol facts, application
state, authorization, or side effects. Its named read-only profile proves
effect-safety admission, not confidentiality or provider retention.

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

## Design test

Before accepting a design, ask:

1. Which semantic owner can prove the fact being represented?
2. Does any projection turn unknown, requested, proposed, or authorized state
   into a stronger claim without new evidence?
3. Does model output cross into acceptance, state transition, or effect without
   deterministic application authority?
4. Does each persistent abstraction protect a current invariant that would be
   materially harder to preserve without it?
5. Can the common change path reach the canonical authority without reading a
   second copy of the same fact?

When convenience conflicts with these answers, reject the convenience.

The executable proof of `accepted != authorized` and
`authorized != executed` is
[`internal/tools/integration/example_authority_to_effect_test.go`](internal/tools/integration/example_authority_to_effect_test.go).
It reuses `llmstep` judgment and keeps authority and effect types local
to the example.
