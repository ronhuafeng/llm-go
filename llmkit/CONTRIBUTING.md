# Contributing

Thanks for improving the provider-neutral toolkit in the `llm-go` repository.

## Project scope

This module owns typed schemas and decoding, provider-neutral call evidence,
bounded settling, and validation-feedback retries. It does not own provider
transport, Codex policy, credentials, prompt libraries, or business validation.

Public packages are:

- `settle`
- `llmschema`
- `llmadapter`
- `llmstep`

The `internal/` tree is private module implementation and test support. Do not
introduce imports from the sibling `codexsdk` module or repository tooling.

## Development setup

Requires Go 1.23 or newer.

```sh
git clone https://github.com/ronhuafeng/llm-go.git
cd llm-go/llmkit
GOWORK=off go test ./...
```

The repository workspace is useful for composition tests, but standalone
module checks must use `GOWORK=off`.

## Before opening a change

Run:

```sh
test -z "$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))"
GOWORK=off go mod tidy -diff
GOWORK=off go vet ./...
GOWORK=off go test ./...
GOWORK=off go test -race ./...
```

Public changes must update the canonical API inventory, behavior tests,
`CHANGELOG.md`, module documentation, and a structured change fragment as
applicable. Breaking changes require module-local migration guidance.

## API review

The canonical public inventory is
`internal/architecture/testdata/handwritten-api.txt`. Treat any diff as an API
review, not incidental test churn. Simple APIs remain exact projections of
detailed APIs, and provider-neutral abstractions must not discard evidence.

## Security

Do not include credentials, private prompts, customer data, local absolute
paths, or generated build artifacts in code, tests, docs, fixtures, or
examples. Report vulnerabilities through the confidential process in
[SECURITY.md](SECURITY.md).
