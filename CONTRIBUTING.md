# Contributing

Choose the owning package family from [`README.md`](README.md). Read its README,
the affected package documentation, source, and tests.

For a change:

1. update the owning code and behavior tests;
2. update package docs or the family README only when consumer-visible usage
   changes;
3. add an `Unreleased` changelog entry in [`CHANGELOG.md`](CHANGELOG.md) for
   user-visible changes;
4. run the commands in [`docs/verify.md`](docs/verify.md).

Protocol upgrades use [`docs/protocol-sync.md`](docs/protocol-sync.md) and the
[`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md)
skill.

The root `go.mod` must not use committed `replace` or `exclude` directives.
Release policy is in [`docs/release.md`](docs/release.md).

Prefer executable Go examples over duplicate README programs. Do not add
credentials, private prompts, customer data, or local absolute paths. Report
security issues through [`SECURITY.md`](SECURITY.md).
