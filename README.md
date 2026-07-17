# llm-go

`llm-go` is the single source and release repository for three
independently published Go modules:

- `github.com/ronhuafeng/llm-go/llmkit`
- `github.com/ronhuafeng/llm-go/codexsdk`
- `github.com/ronhuafeng/llm-go/llmcaller/codex`

The complete legacy histories are part of this repository. The three legacy
repositories are archived and read-only; all active development and releases
use the module paths above.

The repository root is orchestration-only and intentionally has no public Go
module or umbrella facade. The provider-neutral toolkit, exact Codex SDK, and
Codex adapter remain separate semantic and release units.

## Repository contract

`module-registry.json` identifies the three published modules and the
non-published repository-tools module. The committed `go.work` composes those
four modules for checkout development. Verify the registry, workspace, import
ownership graph, and orchestration-only root with:

```sh
go run ./internal/tools/cmd/repoctl verify
```

Workspace composition is additional evidence, not a substitute for independent
module verification. Disable the workspace when checking a release unit:

```sh
(cd llmkit && GOWORK=off go test ./...)
(cd codexsdk && GOWORK=off go test ./...)
(cd llmcaller/codex && GOWORK=off go test ./...)
(cd internal/tools && GOWORK=off go test ./...)
```

See the [current design](docs/architecture/DESIGN.md),
[context map](CONTEXT-MAP.md), and [protected release operation](docs/releasing.md).
Consumers moving from the archived module paths should use the module-owned
[toolkit](llmkit/docs/migration/v0.6.0.md),
[SDK](codexsdk/docs/migration/v0.6.0.md), or
[adapter](llmcaller/codex/docs/migration/v0.5.0.md) migration guide.

## Security

Report vulnerabilities through this repository's private intake. See the
[security policy](SECURITY.md); do not publish sensitive details in an issue.
