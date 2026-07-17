---
status: accepted
---

# Keep canonical API inventories module-local

Each public module owns its canonical exported API inventory. Repository tooling
aggregates inventory diffs and release classification but does not maintain a
root API allowlist.

A module inventory records its exported Go surface using `go/types`, including:

- exported declarations;
- exported methods;
- exported struct fields;
- other externally implementable obligations that are part of the Go type
  system.

It excludes private fields, private helpers, source-file placement, and other
implementation layout.

Generated APIs are covered by generator-owned manifests or generated
inventories in the same module. A handwritten inventory does not byte-mirror or
redeclare generated output.

Inventory changes correspond exactly to real exported API changes. Module-local
diff logic classifies them as additive, breaking, or metadata-only. Behavior
tests continue to own runtime semantics; an inventory describes surface only.

Each public module exposes that classification to release orchestration through
its non-published `internal/cmd/apiinventoryreport` command. The command accepts
one baseline inventory and one candidate inventory and emits a versioned JSON
report binding both digests and the module-owned impact. `repoctl` validates and
aggregates those reports; it does not reimplement their classification logic.

`repoctl release-plan` aggregates the module reports and checks their changelog,
migration, and requested SemVer impact. It does not become an independent API
fact source. Pre-v1 releases still report breaking changes explicitly; after v1,
release plans that violate SemVer fail closed.

Human-reviewed behavior impact is supplied by the structured fragments defined
in ADR-0020; API diffs establish a mechanical minimum rather than the whole
classification.

## Consequences

- Private refactors do not create public release noise.
- Each module can review and version its API independently.
- Reviewers receive one repository-level release report without a duplicated
  root allowlist.
- Generated and handwritten API ownership remains explicit.
- Surface and runtime behavior evidence remain complementary rather than
  conflated.

## Considered options

One root inventory containing all modules was rejected because it would become
a fourth owner of independently published public surfaces.

Recording private fields for structural drift was rejected because external
consumers cannot depend on those details and such checks obstruct safe internal
refactoring.

Using historical proposals as inventories was rejected because prose rationale
is not a canonical or mechanically complete exported surface.
