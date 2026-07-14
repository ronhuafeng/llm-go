---
status: accepted
---

# Keep main buildable with the workspace disabled

Every public module on `main` must build and pass its module-local tests with
`GOWORK=off`. The committed root `go.work` improves development and exercises
the current checkout as a combination, but it cannot satisfy or hide a public
module dependency.

The verification meanings are distinct:

```text
go.work checkout tests  = compatibility among current repository sources
GOWORK=off module tests = independently consumable committed modules
public proxy tests      = evidence about immutable published artifacts
```

The adapter's committed `go.mod` may require only upstream module versions that
have already been published. If an adapter change needs a new toolkit or SDK
contract, the change is staged:

1. expand the upstream API compatibly;
2. publish and proxy-verify the upstream module;
3. migrate the adapter and its committed requirement;
4. remove superseded upstream behavior only in a later compatible release or
   explicitly breaking change.

The initial repository migration follows the same truth rather than creating a
workspace-only exception. After all histories are attached, `llmkit` and
`codexsdk` receive their new module paths, pass isolated checks, and are
published first. Only after their new tags are proxy-visible does the adapter
receive its new module path, imports, and exact published requirements.

Cross-module pull-request delivery under this invariant is defined by ADR-0015.

## Consequences

- A green `main` never depends on an unpublished sibling module.
- Removing or disabling `go.work` does not reveal a hidden broken module graph.
- Cross-module contract changes require staged commits and releases rather than
  pretending that independent Go module publication is atomic.
- Workspace canaries remain valuable additional integration evidence.
- CI must run both module-isolated checks and the relevant workspace canary.

The CI stages and their distinct evidence contracts are defined by ADR-0010.

## Considered options

Allowing workspace-only green commits was rejected because the published
module graph would differ from the graph that made CI pass.

Using pseudo-versions or persistent `replace` directives as a bridge was
rejected because they would turn temporary repository state into the committed
compatibility contract.

Merging a cross-module breaking change atomically was rejected because the
public Go proxy cannot publish multiple modules as one transaction.
