# Contributing

Thank you for helping keep the adapter focused and its evidence complete.

This repository is intentionally small. It should stay focused on adapting
`llmkit` typed requests to `codexsdk` Codex thread calls.

## Development Setup

Requirements:

- Go 1.23 or newer.
- A normal Go module checkout. The project should build with `GOWORK=off`.

Useful commands:

```sh
go mod download
gofmt -w .
go vet ./...
go test ./...
GOWORK=off go test ./...
```

## Scope

Out of scope for this module:

- provider-neutral typed schema generation or decoding, which belongs in
  `llmkit`;
- Codex protocol transport, app-server lifecycle, and streaming APIs, which
  belong in `codexsdk`;
- business prompts, application workflows, private paths, credentials, or
  organization-specific examples.

## Compatibility

Follow Semantic Versioning. Before `v1.0.0`, breaking changes can happen in
minor releases, but they must be documented in `CHANGELOG.md`. After `v1.0.0`,
breaking exported API changes require a new major version.

Avoid unnecessary public API churn. If an issue can be fixed with docs, tests,
or internal code, prefer that over changing exported names or behavior.

## Pull Request Checklist

Before opening a pull request:

- run `gofmt`, `go vet ./...`, and `go test ./...`;
- run `GOWORK=off go test ./...` if you normally develop inside a local
  workspace;
- update README or package docs for user-visible behavior changes;
- update `CHANGELOG.md` for notable changes;
- update `THIRD_PARTY_NOTICES.md` when dependency license provenance changes;
- confirm no credentials, private paths, production data, or organization-only
  details were added.

## Dependency Policy

New dependencies should be rare. Prefer the Go standard library and the
existing `llmkit` and `codexsdk` contracts. Any new dependency must have
an OSI-compatible license and a clear purpose documented in the pull request.
