# AGENTS.md

Read the smallest current source set that owns the task.

- Choose the package family from [`README.md`](README.md), then read its README,
  affected package docs, source, and tests.
- For Codex runtime or live-test work, read
  [`docs/live-codex.md`](docs/live-codex.md). Installed Codex is an external
  runtime; scenarios, not a capability matrix, own the live guarantee.
- For `llmkit`, work in the affected `llmschema`, `llmadapter`, or `llmstep`
  package. For `llmcaller/codex`, read its adapter README and affected tests.
- For protocol upgrades, use [`docs/protocol-sync.md`](docs/protocol-sync.md).
  The [`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md)
  skill is only for the bounded repair pass described there.
- For GitHub Actions, also read [`.github/AGENTS.md`](.github/AGENTS.md) and
  [`docs/verify.md`](docs/verify.md). Live-gate enforcement and protocol
  provenance are different responsibilities.
- For issues, releases, or support, use [`docs/issues.md`](docs/issues.md),
  [`docs/release.md`](docs/release.md), and [`SUPPORT.md`](SUPPORT.md).
- Read [`NORTHSTAR.md`](NORTHSTAR.md) when changing ownership, application
  authority, protocol/caller meaning, or verification policy.

Accepted design is not evidence of implementation. Check current code, native
tracker state, and actual test results; update transition notes when a gap is
closed. Do not report a skipped or unexecuted live scenario as a pass.

Do not search historical issues, old branches, or retired design documents when
current source and tests suffice. Do not weaken a live scenario merely to make
CI green; intentional retirement follows [`CONTRIBUTING.md`](CONTRIBUTING.md).
