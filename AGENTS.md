# AGENTS.md

Read only what the task needs.

- Choose a package family from [`README.md`](README.md), then read that
  family's README, affected package docs, source, and tests.
- For `llmkit` changes, work in the affected `llmschema`, `llmadapter`, or
  `llmstep` package.
- For `codexsdk` runtime changes, read the affected code and tests. For upstream
  protocol upgrades, use [`docs/protocol-sync.md`](docs/protocol-sync.md) and
  [`codexsdk-sync-upstream`](.agents/skills/codexsdk-sync-upstream/SKILL.md).
- For `llmcaller/codex`, read its README and the affected adapter code/tests.
- For GitHub Actions, also read [`.github/AGENTS.md`](.github/AGENTS.md).
- For verification, release, support, or issue operations, use the corresponding
  document under `docs/`, [`SUPPORT.md`](SUPPORT.md), or
  [`docs/issues.md`](docs/issues.md).
- Read [`NORTHSTAR.md`](NORTHSTAR.md) only when changing package-family
  ownership, application authority, protocol/caller meaning, or repository
  architecture.

Do not search historical issues, old branches, or retired design documents when
current source and tests are sufficient.
