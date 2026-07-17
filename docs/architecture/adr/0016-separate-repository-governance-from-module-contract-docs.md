---
status: accepted
---

# Separate repository governance from module contract documentation

ADR-0024 supersedes only this decision's retention of completed proposals, the
dedicated `repoctl` isolation check, and the one-time root migration evidence
documentation. The module documentation ownership rules remain accepted.

Documentation follows semantic ownership. Repository-root documentation
provides navigation, cross-module architecture, and release governance; each
public module owns its current API, behavior, version history, and consumer
migration documentation.

The intended layout is:

```text
README.md                         repository navigation and module selection
docs/architecture/               North Star, context map, ADRs, boundaries
docs/migration/                  one-time three-repository migration

llmkit/README.md                 provider-neutral module contract
llmkit/CHANGELOG.md
llmkit/docs/migration/

codexsdk/README.md                exact Codex module contract
codexsdk/CHANGELOG.md
codexsdk/docs/migration/

llmcaller/codex/README.md         adapter policy and projection contract
llmcaller/codex/CHANGELOG.md
llmcaller/codex/docs/migration/
```

The root README explains which module a consumer should choose and links to the
authoritative module documentation. It does not reproduce complete public API
contracts.

A cross-module quickstart may demonstrate the adapter path and link to the
adjacent toolkit and exact SDK escape hatches. It must not redefine their facts
or policies.

Current module contracts are evidenced by exported code, package documentation,
module README content, behavior tests, and canonical API inventories. Root
architecture documentation defines ownership and dependency direction, not API
allowlists.

Historical proposals remain with their owning module and are explicitly marked
historical and non-normative. Active CI and release gates do not depend on their
content.

`repoctl` may validate links, expected document locations, and the absence of
active-gate references to historical proposals. It does not enforce byte
mirrors between documents.

## Consequences

- Consumers find a single authoritative contract for each module.
- Repository-level guidance can explain the complete system without absorbing
  module semantics.
- Changelogs and migration guides remain aligned with independent versions.
- Historical rationale stays available without becoming current API truth.
- Some root documents consist primarily of navigation and relationships by
  design.

## Considered options

Centralizing all documentation at the repository root was rejected because it
would blur module release ownership and make independent version history harder
to follow.

Copying module contracts into root overview documents was rejected because the
copies would drift and recreate multiple normative owners.

Using historical proposals as API gates was rejected because current exported
code and mechanical inventories own the active surface.
