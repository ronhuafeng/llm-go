# Contributing

Semantics: [`NORTHSTAR.md`](NORTHSTAR.md).
Live invariants: [`DESIGN.md`](DESIGN.md).

Requires Go 1.23 or newer.

Before opening a pull request, run the same deterministic suite used by CI:

```sh
./scripts/verify.sh
```

Public changes must update the owning module's code and behavior tests. Update
package docs or the module README when consumer-visible usage changes, and add
an `Unreleased` changelog entry when the change is user-visible. Do not create
API inventories, release-state mirrors, compatibility facades, or change
fragments solely for release bookkeeping; exported code, executable examples,
behavior tests, `go.mod`, and immutable tags are the relevant authorities.

Prefer executable Go examples over duplicate README programs. README prose
should explain semantics and point to the example that CI actually compiles.

Protocol baseline work uses
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
SDK test design follows [`codexsdk/Agents.test.md`](codexsdk/Agents.test.md).

Do not check in credentials, private prompts, customer data, or local absolute
paths. Report vulnerabilities through [`SECURITY.md`](SECURITY.md).
