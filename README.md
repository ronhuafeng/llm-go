# llm-go

`llm-go` is the single source and release repository for three
independently published Go modules:

- `github.com/ronhuafeng/llm-go/llmkit`
- `github.com/ronhuafeng/llm-go/codexsdk`
- `github.com/ronhuafeng/llm-go/llmcaller/codex`

The complete legacy histories have been imported. Migration is staged in
dependency order: `llmkit` and `codexsdk` now declare their new module
identities, while the adapter still uses its legacy identity until its own
migration step.
No replacement tag is announced as verified until its public-proxy release gate
passes; follow each module's migration guide for the exact cutover point.

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
[migration status](docs/migration/README.md). Production publication follows
the [protected release operation](docs/releasing.md).
