---
status: accepted
---

# Use independent SemVer with one release orchestrator

The `llmkit`, `codexsdk`, and `llmcaller/codex` modules each advance according
to changes in their own public contract rather than sharing a lockstep version.
One repository-level release orchestrator computes the affected modules,
publishes them in dependency order, and derives the caller's exact upstream
compatibility tuple from its committed `go.mod`. This preserves SemVer as a
statement about each module while the monorepo removes manual pull-request and
release coordination.

## Consequences

- A release may create one, two, or three module-prefixed tags at the same
  repository commit.
- A breaking change in one module does not force unrelated modules onto the
  same major version.
- The caller's committed direct requirements are the compatibility fact source;
  no separately handwritten manifest may redefine the same tuple.
- Module tag identities and the ordered, non-atomic publication contract are
  defined by [ADR-0006](./0006-use-path-prefixed-tags-and-ordered-releases.md).
- Minimum supported Go versions are module-owned compatibility facts, as
  defined by ADR-0013.
- Release automation must fail closed if an affected downstream module still
  requires an unpublished or stale upstream version.

## Considered options

Lockstep versions were rejected because they would make every module's SemVer
describe a repository release train rather than that module's compatibility.
The cost becomes especially high after v1, when one module's next major would
force unrelated modules to adopt the same major-version path and migration.
