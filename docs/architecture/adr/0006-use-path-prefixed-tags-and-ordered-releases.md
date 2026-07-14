---
status: accepted
---

# Use path-prefixed tags and ordered releases

Each public module uses only the tag namespace implied by its directory:

```text
llmkit/vX.Y.Z
codexsdk/vX.Y.Z
llmcaller/codex/vX.Y.Z
```

A repository-level tag or GitHub Release cannot substitute for these module
tags or act as the source of Go module version truth.

Production tag authority and release-plan binding are defined by ADR-0019.

The repository release orchestrator performs an ordered, non-atomic release:

1. Publish each changed upstream module (`llmkit` or `codexsdk`).
2. Wait until each tag is resolvable through the public Go proxy and verify its
   published artifact.
3. Update `llmcaller/codex/go.mod` to the exact released upstream versions when
   the adapter release consumes them.
4. Run the adapter and three-layer clean consumer with `GOWORK=off`.
5. Publish `llmcaller/codex` using its own immutable path-prefixed tag.

Every module maintains its own changelog and release notes. A repository-level
release page may summarize a coordinated batch for humans, but it is only a
navigation aid.

The first path-prefixed versions are defined by ADR-0007; equality between
module versions is neither required nor meaningful.

## Consequences

- Git tags unambiguously identify one Go module version.
- Modules that did not change do not receive empty releases.
- Adapter dependency requirements record exact, already published upstream
  versions.
- A coordinated release can be partially complete; tooling and documentation
  must report that state honestly and support forward-only recovery.
- Published tags are never moved or reused after a failed downstream step.

## Considered options

One repository-wide version was rejected because it recreates lockstep
versioning and gives unchanged modules empty releases.

An atomic coordinated release was rejected because Git hosting and the public
Go proxy provide no cross-module transaction. Claiming atomicity would hide an
observable partial publication.

Publishing the adapter before its declared upstream versions are proxy-visible
was rejected because final compatibility evidence must use real published
modules, not workspace replacements or unpublished revisions.
