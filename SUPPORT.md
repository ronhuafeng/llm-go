# Support

Bugs and feature requests belong in
[GitHub Issues](https://github.com/ronhuafeng/llm-go/issues).
Include the module revision, package family, minimal reproduction, expected
behavior, and observed result. Sensitive reports use [`SECURITY.md`](SECURITY.md).

`llmkit` owns provider-neutral schemas, calls, validation, and retry mechanisms.
`codexsdk` owns Go integration with local Codex. `llmcaller/codex` owns translation
between those families. Application prompts and business policy remain consumer
responsibilities.

## Codex runtime support

The SDK expects an already installed official Codex. The live compatibility
target is the exact Codex release selected by that SDK revision's
[checked-in baseline](codexsdk/internal/protocolschema/appserver/v2/baseline_metadata.json).
There is no independent latest-runtime promise or model/version matrix.

Runtime coverage is scenario-based. Active passing live scenarios establish the
behaviors checked under the documented environment; an exposed generated method
does not imply exhaustive runtime coverage. Adding or retiring scenarios changes
that coverage, with retirements disclosed in the PR and changelog/release notes.
Tests do not guarantee model determinism or correctness of Codex Core internals.

The accepted two-scenario suite and its migration status are described in
[`docs/live-codex.md`](docs/live-codex.md). Until the implementation tickets are
completed, do not read the design as evidence that the new hard gate has run.

For Codex reports, include the observed `codex --version`, selected SDK baseline,
failing scenario/stage, and non-sensitive error facts. Distinguish requested
model/provider settings from observed server facts. Never attach credentials or
an entire `CODEX_HOME`.

## Go and operating systems

The root [`go.mod`](go.mod) is the Go-version authority.

| Tier | Platform | Verification scope |
| --- | --- | --- |
| Required | Linux (`ubuntu-latest`) | Native PR verification and the accepted live Codex gate. |
| Advisory | macOS, Windows | Existing scheduled/manual portability checks, not a live-model matrix. |
| Best effort | Other `GOOS`/`GOARCH` | No continuously tested guarantee. |

Linux is the only required live environment. Go portability and Codex's own
platform support do not constitute evidence that llm-go's live suite ran on
another platform; expanding that evidence is outside the current plan.
