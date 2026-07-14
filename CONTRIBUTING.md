# Contributing

> **Repository frozen:** `v0.5.0` is the final release from this legacy module
> path. Feature and security maintenance has moved to
> [`ronhuafeng/llm-go`](https://github.com/ronhuafeng/llm-go), where the toolkit
> module begins at `llmkit/v0.6.0`.

Thanks for helping keep the `llmkit-go` migration record accurate.

## Project scope

This repository is limited to migration guidance, final-release records, and
archive corrections. Do not open feature, dependency-upgrade, security-fix, or
public API work here. Apply ongoing toolkit work to
`github.com/ronhuafeng/llm-go/llmkit` and follow that repository's contribution
policy.

Public packages are:

- `settle`
- `llmschema`
- `llmadapter`
- `llmstep`

The `internal/` tree is private implementation and repository test support.

## Development setup

Requires Go 1.23 or newer.

```sh
git clone https://github.com/ronhuafeng/llmkit-go.git
cd llmkit-go
go test ./...
```

If you are working from a parent directory that has a `go.work`, run standalone
checks with:

```sh
GOWORK=off go test ./...
```

## Before opening a legacy correction

Run:

```sh
gofmt -w $(find . -name '*.go' -not -path './vendor/*')
go vet ./...
go test ./...
```

Runtime behavior changes are out of scope. Documentation-only corrections do
not need tests unless they update examples that should compile.

## API freeze

Do not change the exported API at this legacy module path. The final public
surface is the `v0.5.0` tag. See [Migrating to llm-go](docs/llm-go-migration.md)
for exact replacement imports.

## Dependency freeze

Do not add or upgrade dependencies in this repository. Dependency maintenance
belongs in the successor module.

## Security

Do not include credentials, private prompts, customer data, or local absolute
paths in code, tests, docs, fixtures, or examples. This frozen repository does
not receive security fixes after cutover. Until the successor repository
publishes its own confidential intake policy, use this repository's private
vulnerability reporting for sensitive disclosures; see [SECURITY.md](SECURITY.md).
