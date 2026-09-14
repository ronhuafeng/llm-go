# Support

Bugs and feature requests belong in this repository's
[issue tracker](https://github.com/ronhuafeng/llm-go/issues). Include the
`github.com/ronhuafeng/llm-go` version, owning package family, minimal
reproduction, expected behavior, and actual behavior.

Choose the owning package family first:

- provider-neutral schemas, typed calls, validation, or retries: `llmkit`;
- Codex transport, generated protocol, or thread/turn lifecycle: `codexsdk`;
- Codex-to-`llmkit` translation or Codex schema conversion: `llmcaller/codex`.

Application prompts and business rules are consumer concerns. Sensitive reports
must use [`SECURITY.md`](SECURITY.md), not a public issue.

## Go versions

The root [`go.mod`](go.mod) is the authoritative Go floor. The module supports
Go 1.26.0 or newer.

## Operating systems

| Tier | Platforms | Meaning |
| --- | --- | --- |
| Required | Linux (`ubuntu-latest`) | Required PR verification and default support contract. |
| Advisory | macOS, Windows | Scheduled/manual portability checks; not a merge gate. |
| Best effort | Other `GOOS`/`GOARCH` | Not continuously tested. |

`codexsdk` process and stdio lifecycle are continuously tested on Linux. Live
Codex/provider smoke is Linux-only and is not the OS support contract.
