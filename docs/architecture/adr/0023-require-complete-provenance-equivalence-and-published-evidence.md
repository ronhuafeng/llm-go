---
status: superseded by ADR-0024
---

# Require complete provenance, equivalence, and published evidence

The `llm-go` migration is complete only when all evidence classes below pass.
Workspace success or source-level tests cannot substitute for missing published
artifact evidence.

## Provenance

- The final legacy tags, commits, source URLs, destination directories, new
  module paths, and first new tags are recorded in the provenance manifest.
- Original source commits are reachable in the new repository.
- Each pure relocation commit is tree-equivalent to its source commit after the
  declared directory-prefix transformation.
- Three independent merge nodes attach the three source histories.

## API and behavior equivalence

- Exported API inventories are equivalent after applying the declared old-to-new
  import mapping.
- Generated outputs contain no unintended changes.
- Existing public behavior, race, streaming, partial-evidence, effective-profile,
  validation, and retry suites pass.
- The declared import paths are the only intentional breaking public surface.

## Architecture boundaries

- The module registry contains exactly three public modules and one
  non-published repository-tools module.
- `llmkit` and `codexsdk` remain independent.
- `llmcaller/codex` is the only runtime dependency join.
- No shared runtime or test utility, root facade, or public-module dependency on
  `internal/tools` exists.

## Independent consumption

- Every public module passes minimum-Go, current-Go, and race checks with
  `GOWORK=off`.
- Module files are tidy.
- Module archives exclude sibling modules, repository orchestration, and local
  workspace state.
- Clean consumers use only published public paths.

## Published dependency chain

The first verified chain is:

```text
llmkit/v0.6.0
codexsdk/v0.6.0
        |
        v
llmcaller/codex/v0.5.0
```

- Every tag resolves through the public Go proxy from empty caches.
- The adapter proxy artifact declares the exact two new upstream versions.
- A clean consumer resolves the complete declared tuple and runs the typed
  three-layer canary.
- The graph contains no `replace`, `exclude`, pseudo-version, or workspace
  participation.
- Checksums and machine-readable evidence artifacts are complete.

## Cutover

- Final legacy migration releases are proxy-available; each
  migration-preparation commit remains documentation-only.
- Import mappings, migration guides, changelogs, and release notes are complete.
- Legacy repositories become archived and read-only only after all replacement
  modules are verified.
- Active CI, release scripts, and API gates do not depend on legacy repositories
  or historical proposals.

Failure in any category leaves the migration incomplete. Evidence is reported
per category rather than collapsed into one ambiguous green check.

## Consequences

- Completion describes consumer-observable artifacts, not only repository
  state.
- Reviewers can identify exactly which proof is missing.
- The initial migration has a bounded, machine-verifiable exit condition.
- Old repositories cannot be archived prematurely.

## Considered options

Accepting workspace tests as final evidence was rejected because workspaces do
not prove independent module publication.

Accepting source equivalence without provenance was rejected because historical
origin and relocation integrity would be unverifiable.

Accepting successful tags without behavioral equivalence was rejected because
packaging correctness does not prove semantic preservation.
