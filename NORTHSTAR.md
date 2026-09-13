# North stars

`llm-go` is a Go library, not a theory of knowledge. This document keeps only
the rules that materially constrain APIs, runtime behavior, ownership, and
repository automation.

The main rule is:

> **Models propose. Programs decide. Effects require authority.**

Prefer plain Go, direct protocol facts, and executable tests over project-specific
terminology or framework layers.

## Principles

### 1. Report what is actually known

Do not turn requested values, defaults, guesses, adapter names, model-name
heuristics, or missing fields into facts reported by an API.

When a protocol or public API can distinguish two states that matter to callers,
preserve the distinction. Examples include:

- absent final response versus an observed empty response;
- requested model versus a model actually reported by the server;
- total token usage versus usage for one request;
- generated protocol version versus the version reported by a running server.

Do not invent a stronger value merely because it is more convenient for a
higher-level API.

A failed operation may still return useful partial result data when the owning
API can do so cleanly. Failure does not require throwing away information already
obtained.

### 2. Model output is data, not a decision

Successful JSON parsing, schema validation, or Go decoding does not make model
output true or authorized.

Deterministic code decides whether model output is acceptable for the caller's
purpose. If retries are used, the application decides what validation feedback
is safe and useful to send to the next model attempt.

Rejected output may be kept for diagnostics, but it must not appear in a result
field whose contract means "accepted output".

### 3. Applications own policy and side effects

Libraries may expose facts, callbacks, and mechanisms. They do not acquire the
application's authority merely because they sit on the execution path.

The application owns decisions such as:

- approval and permission policy;
- sandbox or execution policy;
- what data may be disclosed to a model or provider;
- whether files, repositories, messages, deployments, or other external state
  may be changed.

Prompt text is not an authorization mechanism.

When the Codex App Server asks for application-owned input or a decision, the SDK
may deliver that request and encode the application's response. It must not
invent a successful answer when the application supplied none.

### 4. Protocol adapters preserve meaning or fail

`codexsdk` follows the Codex App Server protocol. Generated protocol code must
represent the selected protocol faithfully. If the generator cannot represent a
schema shape, generation fails instead of silently dropping known fields,
variants, or distinctions.

`llmcaller/codex` may translate Codex-specific data into `llmkit` types and may
reject a caller contract that Codex cannot represent. It must not silently change
the caller's contract merely to make the provider accept it.

Keep provider-specific details available when a provider-neutral representation
would lose information that callers may need.

### 5. Keep ownership small and obvious

The runtime has three owners:

```text
llmkit         ---\
                   +--> llmcaller/codex
codexsdk       ---/
```

- `llmkit` owns provider-neutral typed inference, validation, retries, and result
  structure.
- `codexsdk` owns the local Codex App Server process, JSON-RPC protocol, generated
  protocol surface, and lifecycle behavior.
- `llmcaller/codex` owns the translation between those two modules.

`llmkit` and `codexsdk` do not depend on each other directly. The repository root
is orchestration, not another runtime layer.

Do not create `common`, `core`, `shared`, registries, facades, or other persistent
abstractions merely to reduce duplication. A new abstraction needs a current
behavioral reason to exist.

### 6. Prefer owner-native tests over proof frameworks

Correctness should normally be established by the code that owns the behavior:

- Go tests for Go behavior;
- Go generators/checkers for generated Go source;
- the selected Codex/Rust source for upstream App Server schema facts;
- GitHub Actions for ordering, permissions, and repository effects.

CI should run those checks, not become a second semantic system around them.

Repository current-source composition and published module availability are
separate checks. A workspace or temporary replacement may prove that current
source composes, but it does not prove that a future module tag already exists.

### 7. Proof scope must match the next decision

Use only as much provenance or verification machinery as the next claim or
effect actually needs.

For ordinary source changes inside one workflow run, the preferred shape is:

```text
read canonical source
        |
        v
make or propose change
        |
        v
run deterministic owner-native checks
        |
        v
perform the allowed effect
```

If the run fails, it fails. A retry normally starts again from canonical source
and reruns the checks.

Do not build historical CI attestation, cross-run repair state, duplicate
identity layers, or durable proof artifacts for state that can simply be
regenerated and checked again.

Immutable identities still matter where identity is itself part of the product
contract, for example an upstream source commit or a released module tag.

### 8. Delete machinery that no longer protects a current requirement

Every persistent abstraction, compatibility path, registry, workflow state,
helper script, document, or test should have a current consumer or invariant.

If deleting it would not make a real current requirement harder to preserve,
delete it. Git history is the archive for retired designs.

## Module north stars

### `llmkit`

**Turn probabilistic model calls into typed Go results with deterministic caller
validation, without owning provider transport, application policy, or side
effects.**

Keep the public API provider-neutral. Keep accepted output separate from rejected
attempts. Keep retry orchestration bounded and deterministic.

### `codexsdk`

**Be a faithful Go client for one local Codex App Server. Hide transport
mechanics without hiding protocol behavior that matters to callers.**

Own process/JSON-RPC lifecycle, generated protocol types and methods, exact
server-request delivery, and composed thread/turn execution. Do not invent
application policy or application-owned server responses.

Protocol upgrades follow [`docs/protocol-sync.md`](docs/protocol-sync.md): one
selected upstream source, one generated candidate, at most one Agent pass for
real drift, one deterministic acceptance set, and one protected PR effect.

### `llmcaller/codex`

**Connect `llmkit` to `codexsdk` with the smallest semantics-preserving
translation possible.**

Keep exact Codex details when the neutral API cannot represent them faithfully.
Reject incompatible schema representation rather than rewriting caller meaning.
Do not turn adapter identity, requested configuration, or routing choices into
provider/runtime facts that Codex did not actually report.

### Repository

**Let the three modules evolve together without creating a fourth runtime or CI
semantic owner.**

Keep verification and release automation thin. Keep module releases independent
and dependency-ordered. Use rebase integration for semantic commits unless a
repository rule requires otherwise.

## Design check

Before adding an API, field, abstraction, workflow state, or proof mechanism,
ask:

1. Which module actually owns this behavior?
2. Is this value directly known, or are we guessing/defaulting/inferencing it?
3. Does this translation preserve the caller/protocol meaning?
4. Is model output being allowed to decide acceptance, policy, or an external
   effect?
5. Can direct Go code and tests enforce the requirement more simply?
6. Does this persistent concept have a current consumer or invariant?
7. Is the verification machinery proportional to the decision it protects?

If the simpler design preserves the same real behavior, use the simpler design.
