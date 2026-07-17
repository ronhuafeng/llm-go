---
status: accepted
---

# Use a minimal module registry

ADR-0024 supersedes only this decision's continuing retention of the one-time
migration provenance manifest.

The repository root contains a versioned machine-readable registry of governed
Go modules. It declares only publication identities that cannot be safely
inferred from arbitrary directory discovery:

```json
{
  "format_version": 1,
  "modules": [
    {"id": "llmkit", "dir": "llmkit", "published": true},
    {"id": "codexsdk", "dir": "codexsdk", "published": true},
    {"id": "codex-adapter", "dir": "llmcaller/codex", "published": true},
    {"id": "repo-tools", "dir": "internal/tools", "published": false}
  ]
}
```

The exact filename is an implementation detail to settle during repository
bootstrap. Its semantic scope is not.

`repoctl` derives all other facts from their existing authoritative sources:

- module path, Go version, and requirements from each `go.mod`;
- dependency edges and the adapter compatibility tuple from committed module
  requirements;
- tag prefix from the registered module directory;
- current versions from immutable Git tags;
- planned versions from generated release-plan evidence;
- checksums and resolved versions from post-tag proxy evidence.

The registry must not contain copies of those values. Validation fails if an
unregistered `go.mod` appears or a registered directory does not contain the
expected module.

Exported API inventories are likewise module-local and are aggregated rather
than mirrored at the root; see ADR-0018.

Two other machine-readable documents have separate lifetimes and ownership:

- `migration-provenance.json` was immutable historical evidence for the
  one-time repository import; ADR-0024 retired its continuing retention;
- release-plan JSON is generated CI evidence for one proposed publication and
  is not committed as current repository state.

## Consequences

- Release units are explicit without hard-coding repository topology in Go
  source.
- `go.mod` remains the only owner of module identity and dependency facts.
- Adding or removing a module requires an intentional registry review.
- Registry schema evolution is versioned and fail-closed.
- The repository avoids recreating a compatibility manifest that mirrors the
  adapter's requirements.

## Considered options

Discovering every `go.mod` implicitly was rejected because accidental tooling
or fixture modules could silently become release units.

Hard-coding module directories in `repoctl` was rejected because repository
topology would be hidden in implementation details.

Recording paths, versions, dependencies, and checksums in one comprehensive
manifest was rejected because those facts already have authoritative owners and
would drift.
