# llm-go

Typed Go building blocks for reliable structured LLM calls.

`llm-go` provides three independently versioned modules. Use the
provider-neutral toolkit on its own, control a local Codex app-server directly,
or connect the two while keeping the complete Codex result available.

## Modules

| Module | Use it to | Import path |
| --- | --- | --- |
| [`llmkit`](llmkit) | Generate JSON Schema from Go types, decode and validate structured output, and run bounded retries without a provider SDK. | `github.com/ronhuafeng/llm-go/llmkit` |
| [`codexsdk`](codexsdk) | Control a local Codex app-server through generated protocol types and typed thread/turn APIs. | `github.com/ronhuafeng/llm-go/codexsdk` |
| [`llmcaller/codex`](llmcaller/codex) | Use Codex as an `llmkit` caller with access to the complete SDK result. | `github.com/ronhuafeng/llm-go/llmcaller/codex` |

Start with the README for the module matching your use case.

For an implementation-oriented guide that a coding agent can follow, see
[Integrating `llm-go`](docs/coding-agent-guide.md). It covers module selection,
a cross-module quickstart, and routes detailed contracts to their owning module
documentation.

## Architecture

```text
structured application
        │
      llmkit
        │
llmcaller/codex
        │
     codexsdk
        │
 Codex app-server
```

The modules remain separate by design:

- `llmkit` and `codexsdk` do not depend on each other.
- `llmcaller/codex` is the only public module that depends on both.
- `internal/tools` may use all three public modules; they never depend on it.
- The repository root coordinates development and releases but is not a Go
  module or umbrella API.

The complete legacy histories are part of this repository. The three legacy
repositories are archived and read-only; all active development and releases
use the module paths above.

## Development

From the repository root, check the module registry, workspace, import
boundaries, and root layout with:

```sh
go run ./internal/tools/cmd/repoctl verify
```

Then test every module independently using the dependencies declared in its
own `go.mod`:

```sh
(
  set -e
  for module in llmkit codexsdk llmcaller/codex internal/tools; do
    echo "==> Testing ${module}"
    (cd "${module}" && GOWORK=off go test ./...)
  done
)
```

See the [context map](CONTEXT-MAP.md) for current semantic ownership, the
[architecture design](docs/architecture/DESIGN.md) for the accepted repository
structure, [repository verification](docs/verification.md) for the complete CI
checks, and the [protected release operation](docs/releasing.md) for release
maintainers.

Consumers moving from the archived module paths should use the module-owned
[toolkit](llmkit/docs/migration/v0.6.0.md),
[SDK](codexsdk/docs/migration/v0.6.0.md), or
[adapter](llmcaller/codex/docs/migration/v0.5.0.md) migration guide.

## Security

Report vulnerabilities through this repository's private intake. See the
[security policy](SECURITY.md); do not publish sensitive details in an issue.
