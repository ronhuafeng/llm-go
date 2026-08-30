# llm-go design

Status: current

Destinations: [`NORTHSTAR.md`](NORTHSTAR.md).
Operations: [`docs/verify.md`](docs/verify.md), [`docs/release.md`](docs/release.md).

This file is the live invariant list. It is not an ADR archive. Create a new
ADR only when an item below changes and the why would be judged wrong without
it, or when a module-specific prohibition cannot fit here.

## Invariants

**I1** Three public modules, no root facade, no `common`/`shared`/`core`/`types`
runtime, no forwarding packages or re-exports.

**I2** The Codex Adapter is the only runtime join. `llmkit` and `codexsdk`
must not import or require each other.

**I3** `main` is independently buildable. Public-module and release evidence
run with `GOWORK=off`. The workspace cannot repair a published module graph.
If a downstream change needs a new upstream API: expand and publish upstream,
then migrate downstream.

**I4** Independent SemVer and directory-prefixed tags (`llmkit/vX.Y.Z`,
`codexsdk/vX.Y.Z`, `llmcaller/codex/vX.Y.Z`). Publication is ordered and
non-atomic. A repository-level refactor does not authorize module-owned
behavior change.

**I5** Formal tags are created only by protected CI from an approved,
digest-bound plan. Tags are never moved. Artifact defects use a new version.

**I6** Generators, protocol policy, and the fixtures that prove them stay with
their semantic owner. `repoctl` checks a clean diff; it does not own
generation policy. Tests stay module-local unless they observe the black-box
composed system.

**I7** Current public contract is exported code, package documentation,
canonical inventory, and public behavior tests. `.changes/` fragments declare
human-reviewed impact. `module-registry.json` names release units only.

**I8** Root documents own destination, invariants, and operations. Each public
module owns its current contract, changelog, and upgrade notes for still
published tags. Root documents are not API allowlists.

**I9** Retain material only when a current fact, real consumer, upstream
contract, or indispensable invariant requires it.

Each module owns its minimum Go version. The adapter's committed `go.mod` is
the compatibility-tuple source.

## Layout

```text
README.md          choose a module
NORTHSTAR.md       destinations and joins
DESIGN.md          this file
AGENTS.md          agent reading paths
docs/verify.md
docs/release.md
docs/issues.md
llmkit/            CONTEXT.md README.md CHANGELOG.md UPGRADE.md
codexsdk/          same
llmcaller/codex/   same
internal/tools/    non-published
```

Public package roots:

```text
github.com/ronhuafeng/llm-go/llmkit/...
github.com/ronhuafeng/llm-go/codexsdk
github.com/ronhuafeng/llm-go/codexsdk/protocolv2
github.com/ronhuafeng/llm-go/llmcaller/codex
```

## Fact owners

| Fact | Authority |
| --- | --- |
| Destination | `NORTHSTAR.md` |
| Language | Module `CONTEXT.md` |
| Live repository invariants | this file |
| Current API and behavior | Exported code, package docs, inventory, tests |
| Generated facts | Owner-local inputs, manifests, committed output |
| Release impact intent | Module-local `.changes/` fragments |
| CI procedure | `docs/verify.md` |
| Tag identity | `docs/release.md` |
