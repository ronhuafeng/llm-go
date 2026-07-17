# North Star retention audit, 2026-07-17

Status: review evidence for the change from merge-base
`c44772096b80791f80e09cffca9371e41991d4f1`.

This audit records the consumer, semantic owner, complete closure, and deletion
test for every deletion candidate in the change. It is not a public API
inventory or a source of runtime facts. Current contracts remain in exported
code, module documentation, upstream schema inputs, behavior tests, module
files, and repository verification described by the context map.

## Deletion candidates

### Version-specific release recovery

- **Owner:** `internal/tools` and GitHub release orchestration.
- **Consumer audit:** the two `recover-*.yml` workflows called recovery-only
  `repoctl` commands and typed helpers for already-completed release incidents.
  No current release workflow, script, public module, or external consumer
  invoked them. Current failure handling explicitly requires an
  incident-specific reviewed path.
- **Complete closure removed:** the two recovery workflows,
  `internal/tools/internal/repository/recovery.go` and its tests, recovery CLI
  branches, and standing recovery instructions.
- **Deletion test:** the normal digest-bound plan, authorization, immutable tag,
  Draft Release, proxy, checksum, and clean-consumer path remains. Deletion
  removes dormant mutation entry points; recreating an incident-specific path
  would not move ordinary release complexity into callers.

### First-release migration acceptance

- **Owner:** `internal/tools` during the completed repository migration.
- **Consumer audit:** acceptance commands and tests validated the initial
  migrated release tuple and first-tag conditions. The initial public versions
  now exist; current release planning uses the latest stable tag, canonical API
  inventories, change fragments, and module metadata instead.
- **Complete closure removed:** `acceptance.go`, its tests, the associated CLI
  commands, first-tracer dependency special cases, and migration-only
  verification prose.
- **Deletion test:** no current public behavior or release fact is lost. Keeping
  the gate would preserve a completed transition as a second release policy.

### Repository-import provenance manifest

- **Owner:** one-time repository governance, not a public runtime module.
- **Consumer audit:** `migration-provenance.json`, its tag-object fixtures, the
  provenance validator, and its tests consumed only one another after import.
  Git history and immutable path-prefixed tags own the surviving repository and
  release identities; module-local migration guides own consumer mappings.
- **Complete closure removed:** the root manifest, three tag-object files,
  provenance parser/validator code and fixtures, and root migration
  documentation that existed to explain the manifest.
- **Deletion test:** history, module identities, public migration guidance, and
  release tags remain. The removed manifest duplicated those facts and no
  current workflow required it.

### Legacy-repository archival evidence

- **Owner:** one-time repository governance.
- **Consumer audit:** the archive evidence JSON, archival validator, tests, and
  migration acceptance path formed a closed evidence loop after the legacy
  repositories were archived. No current release, security, support, or
  consumer path reads the file.
- **Complete closure removed:** `docs/migration/archive-evidence.json`,
  `archival_evidence.go`, its tests, and acceptance wiring.
- **Deletion test:** current migration guides continue to identify successor
  module paths. Removing a local copy of completed archival observations does
  not alter the external repositories or any runtime/release fact.

### Completed implementation proposals and plan gates

- **Owners:** the three public modules for their current contracts; repository
  architecture for current cross-module decisions.
- **Consumer audit:** the deleted v0.2, three-repository, ergonomics,
  `llmstep`, and thread-default design/ execution documents described completed
  implementations. Their only active consumers were tests scanning gates for
  references to those same historical filenames.
- **Complete closure removed:** the historical plans and prompts, module-local
  isolation/scanner tests, root scanner policy, and references that treated the
  documents as normative.
- **Deletion test:** exported code, behavior tests, canonical API inventories,
  changelogs, migration guides, accepted ADRs, and module READMEs remain.
  Removing the proposals makes their self-justifying gate complexity disappear.

### Duplicate context records and canary narrative

- **Owners:** module-local context documents; `internal/tools` for executable
  cross-module evidence.
- **Consumer audit:** root copies of module contexts duplicated module-owned
  language. The adapter canary narrative duplicated executable evidence and
  current verification documentation.
- **Complete closure removed or relocated:** duplicate
  `docs/architecture/contexts/*/CONTEXT.md` files, the adapter canary narrative,
  and stale navigation. The adapter context moved to
  `llmcaller/codex/CONTEXT.md`; the executable three-layer canary moved to
  `internal/tools/integration`.
- **Deletion test:** the root context map still reaches all three canonical
  contexts, and repository verification still runs the same exact/neutral,
  partial-result, safety-profile, and streaming canary cases.

### No-variation Codex SDK implementation seams

- **Owner:** `codexsdk`.
- **Consumer audit:** removed wrappers and helpers were unexported, had no
  independent variation, and were called only by their immediate implementation
  or implementation-coupled tests. The canonical handwritten API inventory did
  not change.
- **Complete closure removed:** redundant notification-routing and stream
  attachment wrappers, unused run-presence and request-validation helpers,
  generic legacy identity/default helpers, AST call-name helpers, and tests
  whose only contract was that those helpers existed.
- **Deletion test:** call routing, notification attribution, diagnostics,
  terminal ordering, partial results, server-request safety, and public error
  semantics remain covered by behavior tests. Exact generated protocol types
  and exported lifecycle/streaming escape hatches are unchanged.

### Retired protocol-generator compatibility shapes

- **Owner:** `codexsdk` protocol generation.
- **Consumer audit:** schema-v1 manifest fallbacks, absent generation-input
  defaults, inferred baseline identities, compatibility JSON aliases, and
  obsolete shape checkpoints served only older checked-in generator data. The
  current manifest is schema v2 and current baseline metadata records explicit
  source identity.
- **Complete closure removed or deepened:** permissive fallback branches,
  compatibility properties/JSON keys, obsolete generator helpers and tests.
  Current-shape validation and source-derived semantic tests replace them.
- **Deletion test:** upstream schemas, provenance metadata, classified surface,
  coverage matrix, generated Go output, and reproducibility remain. Invalid or
  obsolete inputs now fail closed instead of being normalized into current
  facts.

## Retained capability evidence

| Owner | Smallest retained capability | Positive evidence |
| --- | --- | --- |
| `llmkit` | `llmschema`, `llmadapter`, `llmstep`, and `settle`; neutral detailed evidence and provider extension seam | Public packages and documented paths, canonical API inventory, behavior tests, and the adapter consumer |
| `codexsdk` | exact transport, generated protocol surface, lifecycle, streaming, diagnostics, partial and terminal evidence, and module-owned sync generation | Public API and migration contract, current upstream schemas/manifests, generated reproducibility, exact behavior tests, adapter and direct consumers |
| `llmcaller/codex` | Codex schema/safety policy and lossless projection into toolkit evidence | Public adapter paths, schema-policy tests, effective-profile contract, complete typed `Details.Run`, and clean consumers |
| `internal/tools` | module/repository verification, release planning and authorization, published evidence, isolated consumers, and genuine cross-module canaries | Active workflows and release path, accepted repository invariants, typed evidence, proxy/checksum consumers, and boundary tests |

No root facade, shared runtime module, public dependency join, or public-module
dependency on repository tooling is introduced. `internal/tools` directly
requires the three published modules only because it owns black-box
cross-module canaries; workspace execution replaces those requirements with the
current checkout, while `GOWORK=off` proves the tools module independently.

## Closure and preservation checks

- Exact deleted paths and basenames have no active references; the only
  historical ADR mention of `migration-provenance.json` states that ADR-0024
  retired its continuing retention.
- Go compilation and canonical API inventories prove that removed Go helpers
  have no unresolved consumers and that public declarations remain owned by
  their modules.
- Module-local migration guides, changelogs, security/support policy, context
  documents, accepted ADRs, and supersession notes remain navigable.
- The release path still owns module archive, proxy, checksum, exact dependency
  tuple, and clean-consumer evidence. Removed recovery and migration paths do
  not claim those current facts.
