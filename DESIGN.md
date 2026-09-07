# llm-go design

Status: current

Destination: [`NORTHSTAR.md`](NORTHSTAR.md).
Operations: [`docs/verify.md`](docs/verify.md), [`docs/release.md`](docs/release.md).

This file defines the repository's live invariants. `NORTHSTAR.md` explains why
they exist. Public code and behavior remain the current contract.

## Invariants

**I1 — Semantic ownership stays explicit.** The public runtime has three
semantic owners: `llmkit`, `codexsdk`, and `llmcaller/codex`. The repository
root is orchestration only. Do not add a root facade, forwarding package,
re-export layer, or runtime `common`/`shared`/`core`/`types` owner.

**I2 — The adapter is the only runtime join.** `llmkit` and `codexsdk` must not
import or require each other. The adapter may translate between them, but it
must preserve exact provider details and expose only sound provider-neutral
projections.

**I3 — Semantic promotion requires evidence or authority.** A layer must not
strengthen what it knows while projecting upward. In particular: requested is
not effective, unknown is not zero, unreported is not absent, model output is
not accepted fact, accepted is not authorized, and authorized is not executed.
Represent unknown explicitly when the owner cannot establish a fact.

**I4 — Failure does not erase observation.** Call, decode, validation, retry,
and provider failures must preserve attributable evidence already obtained.
Errors and evidence are independent outputs. Partial observation must not be
rewritten as complete observation or discarded to simplify an API.

**I5 — Judgment and effects stay deterministic.** A model produces typed
propositions; deterministic code owns acceptance. Validator findings and
model-facing repair feedback are different evidence and must remain distinct.
Application-owned authority is required before any external effect; model
output alone never grants it.

**I6 — Published modules must stand alone.** Public-module compatibility is what
a clean consumer can resolve and build without workspace repair. Verification
of published module graphs runs outside `go.work`. If a downstream module needs
a new upstream public API, publish the upstream expansion before migrating and
publishing the downstream module.

**I7 — Published identity is independent and append-only.** `llmkit`,
`codexsdk`, and `llmcaller/codex` have independent SemVer identities and
directory-prefixed tags. Publication is ordered, not atomic. Formal tags are
created only by the authorized release path, are never moved, and defects are
fixed with a new version. Repository-level coordination does not transfer
semantic authority between modules.

**I8 — Facts and proof stay with their owner.** Generated facts, protocol
policy, schemas, fixtures, and tests stay with the semantic owner whose claim
they prove. Module-local tests prove module-local invariants; repository-level
tests prove only composed behavior. Public truth is established by exported
artifacts and observable behavior, not by inventories, registries, change
fragments, workspace builds, or documentation claims that disagree with them.

**I9 — Every persistent context has one current reason to exist.** Give each
fact one canonical authority. Keep a document, registry, field, compatibility
path, test, or abstraction only when a current consumer, external contract,
current invariant, or irreproducible observation requires it. Route readers to
the authority instead of copying it. Historical design belongs in Git history
or release records, not on the current engineering path.

Each module owns its minimum Go version. The adapter's committed `go.mod` is the
compatibility-tuple source.

Reading path: [`AGENTS.md`](AGENTS.md).

## Fact owners

| Fact | Authority |
| --- | --- |
| Destination and semantic principles | `NORTHSTAR.md` |
| Live repository invariants | this file |
| Module vocabulary | module `CONTEXT.md` |
| Current public API and behavior | exported code, package docs, public behavior |
| Generated facts | owner-local generator inputs and committed output |
| Verification procedure | `docs/verify.md` |
| Release identity and procedure | `docs/release.md` |
