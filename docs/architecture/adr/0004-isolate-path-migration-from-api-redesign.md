---
status: accepted
---

# Isolate module-path migration from API redesign

The first `llm-go` release changes repository topology, module paths, import
paths, CI, and release orchestration while preserving the three modules'
existing exported API shapes and runtime semantics. Consumers should be able to
migrate by replacing imports and module requirements without also adapting to a
new lifecycle, evidence, schema, retry, validation, or safety-profile contract.
API redesigns occur only after the new modules have been published and verified,
through separately reviewed changes.

The source-history topology and the separation between pure relocation and
module-path changes are governed by ADR-0008.

ADR-0022 permits only the mechanical SDK and adapter package flattening needed
to avoid duplicated new import paths; toolkit package redesign remains out of
scope.

## Consequences

- The existing public behavior suites and three-layer canary are migration
  oracles rather than examples to rewrite during the move.
- Repository cleanup may relocate files but may not silently change ownership
  or observable behavior.
- Attractive public API simplifications discovered during migration are
  recorded as follow-up work, not folded into the migration branch.
- A migration failure can be diagnosed as repository, module, import, release,
  or packaging drift without an overlapping semantic redesign.

## Considered options

A greenfield API redesign during the move was rejected because it would combine
multiple breaking dimensions and remove the existing tagged releases as a
reliable behavioral baseline. The monorepo makes later cross-module refactoring
cheaper, so there is no need to take that risk during migration.
