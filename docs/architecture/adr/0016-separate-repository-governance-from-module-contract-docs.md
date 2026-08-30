---
status: accepted
---

# Separate repository governance from module contract documentation

ADR-0024 supersedes only this decision's retention of completed proposals, the
dedicated `repoctl` isolation check, and one-time root migration evidence
documentation. Module documentation ownership remains accepted.

Documentation follows semantic ownership.

- Root documents: navigation, destinations, repository model, still-binding
  ADRs, verification, and release identity.
- Each public module: current API, behavior, version history, and consumer
  migration for still-published tags.

Current layout:

```text
README.md                         module selection
CONTEXT-MAP.md                    owners and joins
docs/architecture/Northstar.md    destinations
docs/architecture/DESIGN.md       current repository model
docs/architecture/adr/            still-binding repository decisions
docs/verification.md
docs/releasing.md

llmkit/CONTEXT.md                 toolkit language
llmkit/README.md                  toolkit contract
llmkit/CHANGELOG.md
llmkit/docs/migration/            current published-tag guides

codexsdk/CONTEXT.md
codexsdk/README.md
codexsdk/CHANGELOG.md
codexsdk/docs/adr/                still-binding SDK decisions
codexsdk/docs/migration/

llmcaller/codex/CONTEXT.md
llmcaller/codex/README.md
llmcaller/codex/CHANGELOG.md
llmcaller/codex/docs/migration/
```

The root README chooses a module and links to that module's contract. It does
not reproduce complete public APIs. A cross-module quickstart may demonstrate
the adapter path; it must not redefine adjacent facts.

Current module contracts are exported code, package documentation, module
README content, behavior tests, and canonical API inventories. Root
architecture defines ownership and dependency direction, not API allowlists.

Completed transitions, superseded ADRs, and pre-monorepo migration notes are
git history. They are not the default local-change path. Active CI and release
gates do not depend on them.

## Consequences

- Each module has one current contract owner.
- Repository guidance can explain the system without absorbing module
  semantics.
- Changelogs and current migration guides stay aligned with independent
  versions.
