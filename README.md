# llm-go

`llm-go` is the single source and release repository for three
independently published Go modules:

- `github.com/ronhuafeng/llm-go/llmkit`
- `github.com/ronhuafeng/llm-go/codexsdk`
- `github.com/ronhuafeng/llm-go/llmcaller/codex`

The complete legacy histories have been imported, but their module identities
have not yet migrated and no replacement tag has been released from this
repository. Continue using the legacy module paths until their migration guides
identify verified replacement tags.

The repository root is orchestration-only and intentionally has no public Go
module or umbrella facade. The provider-neutral toolkit, exact Codex SDK, and
Codex adapter remain separate semantic and release units.

## Repository contract

`module-registry.json` identifies the three published modules and the
non-published repository-tools module. The committed `go.work` composes those
four modules for checkout development. Verify the registry, workspace, import
ownership graph, orchestration-only root, and migration provenance with:

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

See the [accepted target design](docs/architecture/DESIGN.md),
[context map](CONTEXT-MAP.md), and
[migration status](docs/migration/README.md).
