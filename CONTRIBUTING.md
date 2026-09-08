# Contributing

Semantics: [`NORTHSTAR.md`](NORTHSTAR.md).
Live invariants: [`DESIGN.md`](DESIGN.md).

The published modules support Go 1.23 or newer. The repository workspace and
`internal/tools` require Go 1.25 or newer; that tooling requirement does not
raise the public modules' consumer baseline.

Before opening a pull request, follow the explicit Go commands and compatibility
checks in [`docs/verify.md`](docs/verify.md). Ordinary verification has no
repository-specific task runner.

Public changes must update the owning module's code and behavior tests. Update
package docs or the module README when consumer-visible usage changes, and add
an `Unreleased` changelog entry when the change is user-visible. Do not create
API inventories, release-state mirrors, compatibility facades, or change
fragments solely for release bookkeeping; exported code, executable examples,
behavior tests, `go.mod`, and immutable tags are the relevant authorities.
Do not add `replace` or `exclude` directives to a public module `go.mod`;
see [`DESIGN.md`](DESIGN.md) I6.

Prefer executable Go examples over duplicate README programs. README prose
should explain semantics and point to the example that CI actually compiles.

Protocol baseline work uses
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
SDK test design follows [`codexsdk/Agents.test.md`](codexsdk/Agents.test.md).

Do not check in credentials, private prompts, customer data, or local absolute
paths. Report vulnerabilities through [`SECURITY.md`](SECURITY.md).
