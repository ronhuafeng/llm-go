# llm-go Context Map

This repository contains three separately published Go modules whose
semantic ownership remains independent even though development and review share
one repository. Each owner's destination, and why they walk together, is in
[docs/architecture/Northstar.md](./docs/architecture/Northstar.md).

## Contexts

- [LLM Toolkit](./llmkit/CONTEXT.md) — owns provider-neutral typed LLM
  operations, validation, retry, and published toolkit evidence.
- [Codex SDK](./codexsdk/CONTEXT.md) — owns exact Codex transport,
  generated protocol access, and thread/turn lifecycle facts.
- [Codex Adapter](./llmcaller/codex/CONTEXT.md) — owns Codex-specific
  schema and safety policy and projects exact Codex facts into toolkit evidence.

## Relationships

- **Codex Adapter → LLM Toolkit**: implements the provider-neutral caller
  contracts and publishes toolkit-owned evidence.
- **Codex Adapter → Codex SDK**: invokes exact Codex lifecycle and retains the
  complete typed SDK result as the provider-specific escape hatch.
- **LLM Toolkit ↮ Codex SDK**: neither module imports or defines the other; the
  adapter is their only dependency join.
- **Repository Tools → all public modules**: the non-published `internal/tools`
  workspace module may consume all three modules for release orchestration,
  integration tests, and canaries. Public modules may not depend on it.

There is no shared runtime context or module. A fourth `common`, `shared`,
`core`, or `types` runtime owner is prohibited; see ADR-0005.

Repository documentation maps these contexts and their relationships. Each
context's module documentation remains the authoritative location for its
current public contract; see ADR-0016.
