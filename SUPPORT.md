# Support

Bugs and feature requests belong in this repository's
[issue tracker](https://github.com/ronhuafeng/llm-go/issues). Include the
module, version, minimal reproduction, expected behavior, and actual behavior.

Choose the owning module first:

- provider-neutral schema, decode, or validation feedback: `llmkit`
- Codex transport, Exact Run, or generated protocol: `codexsdk`
- Codex schema policy or projection into toolkit evidence: `llmcaller/codex`

Application prompts and business validation remain consumer concerns.
Sensitive reports must use [`SECURITY.md`](SECURITY.md), not a public issue.

## Operating systems

Writing these modules in Go does not by itself make every `GOOS` supported.

| Tier | Platforms | Meaning |
| --- | --- | --- |
| Required / continuously tested | Linux (`ubuntu-latest`) | Required `PR verification` and the default support contract. |
| Advisory portability-tested | macOS and Windows (`macos-latest`, `windows-latest`) | Scheduled or manual `Advisory OS portability` runs `go vet`/`go test` on the public modules with `GOWORK=off`, plus current-source integration tests. Failures are investigated; this job is not a required merge gate. |
| Untested / best-effort | Any other `GOOS`/`GOARCH` | No continuous evidence. Do not infer support. |

`codexsdk` process and stdio lifecycle is continuously proven on Linux. Codex
CLI availability and live provider smoke are Linux-only and are not part of
the OS matrix. This repository does not claim official Codex CLI support on
every Go-supported OS, and it does not claim mobile, WASM, or Plan 9 support.
