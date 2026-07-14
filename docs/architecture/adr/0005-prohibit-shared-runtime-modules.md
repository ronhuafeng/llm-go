---
status: accepted
---

# Prohibit shared runtime modules

- Status: accepted
- Date: 2026-07-14

## Context

Moving the three projects into one repository makes it mechanically easier to
share code. That convenience must not weaken semantic ownership or the existing
dependency boundary:

- `llmkit` owns provider-neutral typed LLM toolkit semantics.
- `codexsdk` owns exact Codex interaction facts and lifecycle semantics.
- `llmcaller/codex` is the only dependency join between them.

A repository-level `common`, `shared`, `core`, or `types` runtime package would
create an ambiguous owner. It could also allow Codex-specific concepts to leak
into the neutral toolkit, or toolkit policy to leak into the exact SDK.

Implementation similarity alone does not establish shared semantics.

## Decision

The repository absolutely prohibits a fourth shared runtime module or package.

The permitted dependency graph is:

```text
llmkit         ---\
                 +--> llmcaller/codex
codexsdk       ---/

internal/tools ----> llmkit, codexsdk, llmcaller/codex

llmkit         <-X-> codexsdk
public modules  -X-> internal/tools
```

Runtime types and helpers must remain with the module that owns their stable
semantics. When similar code appears in multiple modules:

1. If one module owns the semantics, the code belongs in that module and other
   modules may consume its public contract only when the dependency direction
   permits it.
2. If the code has different semantics despite a similar implementation shape,
   duplication is acceptable.
3. The similarity must not be resolved by adding a public or private shared
   runtime package between the public modules.

Repository-level fixtures, release automation, integration consumers, and
cross-module canaries may live in the non-published `internal/tools` workspace
module. That module is a consumer of the public modules and can never be their
dependency.

Test-helper ownership and the black-box integration boundary are refined by
ADR-0017.

## Consequences

- The unique adapter dependency join remains mechanically visible.
- Each runtime fact and policy retains one semantic owner.
- Some small implementation duplication may remain intentionally.
- Repository tooling can still exercise the complete system without becoming
  part of the published runtime graph.
- Architecture tests must reject imports between `llmkit` and `codexsdk`, public
  imports of `internal/tools`, and any newly introduced shared runtime module.

## Rejected alternatives

### Add a shared `core` or `types` module

Rejected because it creates an ambiguous semantic owner and an attractive path
for cross-layer leakage.

### Allow unexported repository-wide runtime helpers

Rejected because Go package and module boundaries do not make such helpers
semantically neutral; they would still introduce a new runtime dependency join.

### Deduplicate every similar implementation

Rejected because code shape is weaker evidence than semantic ownership.
