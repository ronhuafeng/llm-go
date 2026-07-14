---
status: accepted
---

# Use module-local structured change fragments

Each public module owns structured change fragments that declare the intended
release impact of user-visible changes. Repository tooling validates and
aggregates these declarations; it does not infer all behavior compatibility from
source code or parse changelog prose.

An illustrative fragment is:

```yaml
format_version: 1
module: llmkit
impact: patch
breaking: false
summary: Sanitize retry feedback only when another attempt will run.
issue: 24
migration: none
```

The exact serialization and directory name are implementation details, but the
contract is:

- user-visible runtime or API changes add a fragment in the owning module;
- impact is one of `patch`, `minor`, or `major`;
- a breaking change is declared explicitly even when pre-v1 policy permits a
  minor version;
- pure tests, private refactors, and repository tooling may declare
  `release: none` and do not create empty public-module releases;
- a cross-module pull request supplies one fragment for each affected release
  unit;
- a breaking fragment links to module-local migration documentation rather than
  embedding the migration guide in release metadata.

Mechanical exported-API diffs establish a minimum impact. A breaking surface
cannot be declared as a patch, and an additive surface cannot be declared below
the module's SemVer policy. Reviewers remain responsible for behavior changes
that static API analysis cannot classify.

The release workflow aggregates a module's unreleased fragments, validates the
target version, and generates that module's changelog entry and release notes.
Fragments are consumed or archived by the release commit so released history
has one stable module-owned representation.

## Consequences

- Version intent is explicit in the change review that introduces behavior.
- Release planning does not scrape prose or pretend that API shape captures all
  compatibility.
- Independent modules receive independent release impact declarations.
- Internal-only repository work does not cause public version churn.
- Changelog and release-note generation can be deterministic.

## Considered options

Having `repoctl` infer SemVer entirely from code was rejected because behavior,
policy, diagnostics, and evidence compatibility exceed static API shape.

Maintaining only an `Unreleased` Markdown section was rejected as the release
tool's primary input because prose is difficult to validate and attribute per
change.

Using one root change stream was rejected because it would blur independent
module release ownership.
