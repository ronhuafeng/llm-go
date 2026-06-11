# Contributing

Thanks for helping improve `llmkit-go`.

## Project scope

This repository provides provider-neutral Go primitives for typed LLM
programming. Keep provider SDKs, credentials, business workflows, prompt
catalogs, and product-specific policy in separate modules.

Public packages are:

- `settle`
- `llmschema`
- `llmadapter`

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

## Before opening a pull request

Run:

```sh
gofmt -w $(find . -name '*.go' -not -path './vendor/*')
go vet ./...
go test ./...
```

Please include tests for behavior changes. Documentation-only changes do not
need tests unless they update examples that should compile.

## API changes

Avoid unnecessary public API changes. For exported identifiers in public
packages:

- Additive changes should include tests and README or package documentation
  updates when they affect user-facing behavior.
- Breaking changes must update `CHANGELOG.md` and explain migration impact.
- Do not add provider-specific SDK imports to public packages.

See [docs/release.md](docs/release.md) for the compatibility and release
policy.

## Dependency changes

Keep dependencies small and provider-neutral. When adding or upgrading a module:

- Run `go mod tidy`.
- Update `THIRD_PARTY_NOTICES.md` with license/provenance changes.
- Confirm the dependency license is compatible with MIT-licensed distribution.

## Security

Do not include credentials, private prompts, customer data, or local absolute
paths in code, tests, docs, fixtures, or examples. Report vulnerabilities using
the process in [SECURITY.md](SECURITY.md).
