# llm-go

`llm-go` is the future single source and release repository for three
independently published Go modules:

- `github.com/ronhuafeng/llm-go/llmkit`
- `github.com/ronhuafeng/llm-go/codexsdk`
- `github.com/ronhuafeng/llm-go/llmcaller/codex`

The repository is currently in bootstrap state. The public modules have not
yet been imported or released from this repository. Continue using the legacy
module paths until their migration guides identify verified replacement tags.

The repository root is orchestration-only and intentionally has no public Go
module or umbrella facade. The provider-neutral toolkit, exact Codex SDK, and
Codex adapter remain separate semantic and release units.

See the [accepted target design](docs/architecture/DESIGN.md),
[context map](CONTEXT-MAP.md), and
[migration status](docs/migration/README.md).
