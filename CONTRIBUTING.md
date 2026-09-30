# Contributing

Choose the owning package family from [`README.md`](README.md). Read its README,
affected source, and tests. Keep changes local to the behavior they own.

## Changes and verification

Update implementation and focused deterministic tests together. Update usage
documentation when public behavior changes and record user-visible changes in
[`CHANGELOG.md`](CHANGELOG.md). Run the relevant commands in
[`docs/verify.md`](docs/verify.md).

Codex-runtime-affecting changes also follow the accepted
[live integration contract](docs/live-codex.md). The official runtime is already
installed in the deployment model; CI installation prepares that environment,
not a new SDK responsibility. Do not mirror upstream Rust tests or introduce a
fake-model replacement for the agreed real-model acceptance path.

The first suite contains two integration stories. Prefer sharing fixture setup
and covering behavior through the composed public path over duplicating every
operation as a direct SDK test. Scenario boundaries should reflect real use,
not a mandatory one-capability/one-test pattern.

## Scenario evolution

Live scenario code and assertions are the live guarantee's source of truth.
There is no second capability manifest. A scenario that is not run or does not
pass supplies no positive evidence.

When a PR removes or materially weakens a live scenario, explicitly describe the
live-contract retirement, the behavior no longer covered, and the reason.
Record that change in `CHANGELOG.md` and carry it into release notes. Preserve
failure evidence rather than silently skipping, retrying until green, or
removing assertions. A refactor or consolidation that retains the behavior and
assertions is not retirement; explain that preservation in the PR.

Do not promote a planned scenario or documented gate to implemented status
without execution/enforcement evidence. Close implementation gaps only after
their acceptance criteria are met.

## Boundaries

Application policy stays application-owned, including in test fixtures. Use
isolated temporary state and keep credentials, private prompts, customer data,
auth files, and local absolute paths out of commits and diagnostics.

The root `go.mod` must not contain committed `replace` or `exclude` directives.
Prefer executable Go examples over duplicate README programs.

Protocol upgrades follow [`docs/protocol-sync.md`](docs/protocol-sync.md).
Issue operations follow [`docs/issues.md`](docs/issues.md); release boundaries
follow [`docs/release.md`](docs/release.md). Security reports use
[`SECURITY.md`](SECURITY.md).
