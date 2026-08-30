# North stars

This document names the destination of each semantic owner and why they share
one repository. Use it when two good changes conflict.

A north star is the outcome that owner will not sacrifice. Module `CONTEXT.md`
files name the language. [`DESIGN.md`](DESIGN.md) lists live repository
invariants. Exported code, inventories, and tests remain the current public
contract.

## LLM Toolkit

Module: `llmkit`. Language: [`llmkit/CONTEXT.md`](llmkit/CONTEXT.md).

**Provider-neutral typed structured output, with complete stage-owned
evidence, and with no provider SDK.**

Callers receive a Go value and can explain every attempt. Schema decode,
generic typed output, validation decisions, and retry feedback stay distinct.
The settle loop is bounded and toolkit-owned.

This module will not take a provider SDK, own transport or credentials, mix a
validation decision with sanitizer output, or absorb prompt libraries, tracing
backends, or business rules.

## Codex SDK

Module: `codexsdk`. Language: [`codexsdk/CONTEXT.md`](codexsdk/CONTEXT.md).

**Exact control of one local Codex app-server: generated protocol facts,
attributable Exact Runs, and fail-closed admission.**

The Root Client owns the process and transport. Lifecycle APIs do not copy
generated facts into handwritten models. Generated facades and `protocolv2`
remain the factual operation and model source. An Exact Run keeps ordered
attributable history; Wait observes completion; Next only moves an Exact Run
History Cursor. Action-bearing messages fail closed. Applications declare
Consumer-Owned Interfaces.

This module will not become a provider-neutral toolkit, a workflow DSL, or an
application safety profile. It will not publish an umbrella Client interface
or a friendlier copy of the protocol.

## Codex Adapter

Module: `llmcaller/codex`. Language: [`llmcaller/codex/CONTEXT.md`](llmcaller/codex/CONTEXT.md).

**A lossless join: toolkit-shaped calls, exact Codex facts, and adapter-owned
Codex policy only.**

The adapter implements toolkit caller contracts, invokes exact Codex
lifecycle, and projects Execution evidence. It publishes typed Provider
details as the escape hatch. Terminal observation applies the Effective
profile. Exact snapshots do not. Schema policy is adapter-owned dialect
admission for Codex-bound output schemas.

This module will not own settle, schema compilation, transport, protocol
generation, or thread lifecycle. It will not drop the complete SDK result in
order to look simpler.

## Repository

Path: `github.com/ronhuafeng/llm-go`. Invariants: [`DESIGN.md`](DESIGN.md).

**Three independently publishable truths, proven by public-proxy artifacts.
One repository must not create a fourth runtime owner.**

The root is orchestration-only. Formal tags are independent SemVer identities.
Compatibility is what a clean consumer resolves from the public proxy, not
what a workspace build can compile.

`internal/tools` consumes the public modules for verification, release
planning, and black-box canaries. It is not a product and must never become
their dependency.

## Walk together

The three destinations share one repository because they must change in sight
of each other without becoming one API.

```text
llmkit         ---\
                 +--> llmcaller/codex
codexsdk       ---/
internal/tools ----> all three public modules
llmkit         <-X-> codexsdk
public modules  -X-> internal/tools
```

- **Adapter → Toolkit**: implements caller contracts and publishes
  toolkit-owned evidence.
- **Adapter → SDK**: invokes exact lifecycle and retains the complete typed
  SDK result.
- **Toolkit ↮ SDK**: neither imports or defines the other.

Shared source, review, CI, and one typed release orchestrator reduce
coordination cost. They do not collapse the three owners or their runtime
APIs. A `common`, `shared`, `core`, or `types` runtime module is prohibited.

Each layer may hide the complexity of the layer beneath it. It must not hide
that layer's facts:

```text
typed application value
  <- llmkit hides provider shape, keeps toolkit-owned evidence
    <- adapter hides request assembly, keeps exact Codex results and policy
      <- SDK hides stdio and JSON-RPC, keeps protocolv2 and Exact Runs
        <- Codex app-server remains the Codex fact source
```

Keep three repositories only if the join and ordered publication no longer
need one review surface. Merge the modules only if exact Codex facts and
provider-neutral evidence may become the same API. Neither move is the
current destination.

## Using this document

When a change is easier for callers and weaker for a north star, reject it.
When a change needs the same helper in two modules, duplicate it or put it
with the owner of its semantics; do not add a shared runtime.
When a document or abstraction reduces cognitive cost by copying facts, delete
the copy and point to the owner.
