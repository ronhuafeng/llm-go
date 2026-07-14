---
status: accepted
---

# Keep generators with their semantic owner

Generation logic, source manifests, generated API inventories, and generation
tests live inside the public module that owns the generated facts. The root
repository tool invokes generation checks but does not define generation
semantics.

Examples include:

```text
codexsdk/internal/cmd/protocolgen  -> Codex protocol and facade output
llmkit/internal/...                -> neutral toolkit generation, if needed
llmcaller/codex/internal/...       -> adapter policy assets, if needed
internal/tools/cmd/repoctl         -> orchestration only
```

Generated runtime output is committed. A consumer of a published module does
not need the generator or its input environment to compile the library.

The generation contract is:

- the owning module pins its generator tool dependencies;
- the owning module tests the mapping from source facts to generated output;
- `repoctl verify` invokes the module's declared generation check and requires
  the repository diff to remain empty;
- root workflows do not reproduce generator commands or schema policy;
- no shared generator framework is introduced merely because generators have
  similar implementation shapes.

The repository migration relocates existing generators and adjusts paths only.
It does not redesign generator architecture or intentionally change generated
runtime output during the semantic freeze.

## Consequences

- Codex protocol generation remains owned by `codexsdk`.
- Provider-neutral generation remains owned by `llmkit`.
- Adapter-specific policy generation remains owned by `llmcaller/codex`.
- Release orchestration cannot become a hidden source of runtime facts.
- Generated drift is still checked consistently from the repository root.

## Considered options

Moving all generators into `internal/tools` was rejected because repository
tooling would acquire protocol and runtime semantic ownership.

Generating only during consumer builds was rejected because it would make
published builds depend on generator availability and external source facts.

Adding a cross-module generator framework was rejected under ADR-0005: shared
code shape does not establish a shared runtime or generation domain.
