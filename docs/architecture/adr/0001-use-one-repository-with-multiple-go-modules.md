---
status: accepted
---

# Use one repository as the sole source for multiple Go modules

The future `github.com/ronhuafeng/llm-go` repository will be the sole source
and release repository for three separately importable Go modules:
`github.com/ronhuafeng/llm-go/llmkit`,
`github.com/ronhuafeng/llm-go/codexsdk`, and
`github.com/ronhuafeng/llm-go/llmcaller/codex`. This deliberately accepts a
breaking module-path migration so safe cross-module changes can share one
review and one CI system without weakening the module boundary between the
provider-neutral toolkit, the exact Codex SDK, and their adapter. Changes that
need unpublished module contracts remain staged under ADR-0015.

The existing `llmkit-go`, `codexsdk-go`, and `llmcaller-codex-go` repositories
will receive final migration guidance and then become archived, read-only
historical sources. They will not remain publishing mirrors: retaining mirrors
would preserve the synchronization, tag coordination, proxy provenance, and
release ambiguity that the monorepo is intended to remove.

Their final documentation-only versions and archive gate are defined by
ADR-0021.

## Consequences

- Existing consumers must update module and import paths.
- Each subdirectory module keeps its own `go.mod`, prefixed tag namespace,
  public Proxy artifact, and clean-consumer contract.
- Repository-level development and review are coordinated. Module publication
  is ordered and non-atomic, and the three tag namespaces advance under the
  independent SemVer policy recorded by ADR-0002.
- The old module paths remain resolvable only at their final published
  versions; no transparent forwarding or ongoing mirror publication is part of
  the design.

## Considered options

Keeping the old repositories as release mirrors was rejected because the
monorepo would cease to be the unique source of release truth. A single Go
module containing all three packages was rejected because it would replace
module-enforced independence with convention and would force the toolkit, SDK,
and adapter onto one version lifecycle.
