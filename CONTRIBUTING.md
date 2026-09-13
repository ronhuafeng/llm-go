# Contributing

Choose the owning module from [`README.md`](README.md). Read its README, the
affected package documentation, source, and tests.

For a change:

1. update the owning code and behavior tests;
2. update package docs or the module README only when consumer-visible usage
   changes;
3. add an `Unreleased` changelog entry for user-visible changes;
4. run the relevant commands in [`docs/verify.md`](docs/verify.md).

Protocol upgrades use [`docs/protocol-sync.md`](docs/protocol-sync.md) and the
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md)
skill.

Public module `go.mod` files must not use committed `replace` or `exclude`
directives to repair repository-local dependency state. Release ordering and
published dependency checks are in [`docs/release.md`](docs/release.md).

Prefer executable Go examples over duplicate README programs. Do not add
credentials, private prompts, customer data, or local absolute paths. Report
security issues through [`SECURITY.md`](SECURITY.md).
