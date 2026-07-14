---
status: accepted
---

# Use an orchestration-only repository root

The `llm-go` repository root has no public root module. The three public module
roots are the peer directories `llmkit/`, `codexsdk/`, and
`llmcaller/codex/`, matching their public module-path suffixes and prefixed tag
namespaces. A committed root `go.work` coordinates development, while an
unpublished `internal/tools` workspace module owns repository-level release
orchestration and integration tests. Public modules must not depend on that
tools module, and independent module or release evidence runs with
`GOWORK=off`.

## Consequences

- No public module receives special root status or an unprefixed tag.
- Repository tooling has an explicit owner without becoming part of a public
  product module.
- Workspace builds may compose unpublished sibling changes during development;
  module-level CI must also prove that each public module works without the
  workspace.
- The repository root owns documentation, CI orchestration, and release policy,
  not provider-neutral, Codex-exact, or adapter runtime behavior.

## Considered options

Putting `llmkit` at the repository root was rejected because it would make one
peer module structurally privileged. A public umbrella root module was rejected
because it would introduce a fourth product and blur the three release
boundaries. A `modules/` parent directory was rejected because GitHub-backed Go
module discovery would make that directory part of the public module paths.
