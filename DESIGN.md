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
projections. Provider-specific representation/schema admission may reject an
unrepresentable caller contract; it must not strengthen or weaken that
caller's accepted instance language merely to obtain provider acceptance.

**I3 — Semantic promotion requires evidence or authority.** A layer must not
strengthen what it knows while projecting upward. In particular: requested is
not effective, unknown is not zero, unreported is not absent, an observed
empty/zero value is not an absent observation, execution-backend identity is
not model-provider identity, a model identifier is not provider identity,
selected/routed model is not served model, aggregate usage is not per-attempt
usage, model output is not accepted fact, a preserved proposition is not an
accepted result, accepted is not authorized, and authorized is not executed.
Convenience projections must not rewrite exact terminal status, presence,
identity, provenance, or measurement scope. Represent unknown and absence
explicitly when the owner cannot establish a fact.

**I4 — Failure does not erase observation.** Call, decode, validation, retry,
and provider failures must preserve attributable evidence already obtained.
Errors and evidence are independent outputs. Partial observation must not be
rewritten as complete observation or discarded to simplify an API. Failure to
obtain an application-owned fact or decision does not authorize a lower layer
to synthesize a successful semantic substitute. Rejected or unjudged
propositions may stay in attempt evidence without occupying an accepted-result
slot.

**I5 — Judgment, retry disclosure, admission, and effects keep separate owners.**
A model produces typed propositions; deterministic code owns acceptance.
Validator findings and model-facing retry feedback are different evidence and
must remain distinct. `llmkit` may own the projection seam, retry orchestration,
iteration, and attempt evidence; the application owns what finding content is
disclosed to a later model attempt, including redaction and secret/content
policy. Public naming for that hook must describe projection, not imply that the
toolkit owns sanitization or disclosure policy.

Application-owned execution policy and authority are required before external
effects. A callback, profile-shaped parameter, or admission hook does not
transfer approval, sandbox, permission, confidentiality, or effect authority to
the library that implements the mechanism. Every model-directed continuation
path that obtains exact execution facts before proceeding — including start,
resume, re-entry, or an equivalent lifecycle spelling — must expose the
application-owned admission boundary before continuation when those facts are
policy inputs.

When the continuation also depends on caller-owned request overrides applied
after the observed facts, the admission boundary must expose the exact pending
request separately from the observation or fail closed. It must not hide
policy-relevant overrides, require the application to reconstruct a second copy
of the outbound request, or merge requested and observed values into a synthetic
"effective" fact.

**I6 — Source cohorts and published closures are different proofs.** Public
modules must stand alone when published: a clean consumer must resolve and
build the tagged module without workspace repair. Before publication, pre-v1
modules may evolve atomically in one source cohort, including a downstream
`go.mod` naming the next upstream version before that upstream tag exists.
Required PR verification may prove such a cohort against repository current
source through temporary, uncommitted module replacements. That current-source
proof is not proof of published dependency availability.

Publication remains dependency-first. Before a dependent module tag is created,
every version named by its committed `go.mod` must already exist and the
committed dependency closure must resolve with `GOWORK=off`. Public `llmkit`,
`codexsdk`, and `llmcaller/codex` manifests must not contain `replace` or
`exclude` directives; `go.work` is repository composition only, never a
published-consumer repair mechanism.

**I7 — Published identity is independent and append-only.** `llmkit`,
`codexsdk`, and `llmcaller/codex` have independent SemVer identities and
directory-prefixed tags. Formal tags are never moved or reused; defects get a
new version. Repository location does not transfer semantic authority between
modules.

**I8 — Facts, policy, and proof stay with their owner.** Generated facts,
protocol admission rules, schemas, fixtures, tests, and executable examples stay
with the semantic owner whose claim they prove. Application policy does not
move into a toolkit, adapter, or SDK because that layer exposes the mechanism
where policy is applied. An SDK may deliver an exact server request and encode
a caller-supplied response; it must not invent application-owned approval,
input, permission, environment, or equivalent semantic facts when the
application supplied none. Module-local tests prove module-local invariants;
repository-level tests prove composed behavior. Public truth is established by
exported artifacts and observable behavior, not by inventories, release
ledgers, verification attestations, workspace-only builds, current-source
replacement proofs, or contradictory documentation.

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
| Caller-owned structured-output semantics | caller contract as represented by `llmkit` |
| Final-response presence | attributable lower-layer observation; neutral projection preserves it |
| Served-model identity | attributable lower-layer serving evidence; requested/selected/routed model is insufficient |
| Usage measurement scope | attributable lower-layer observation; projection preserves or weakens scope |
| Accepted step output | positive deterministic judgment |
| Retry-feedback disclosure/redaction/content policy | application |
| Execution/approval/sandbox/permission policy and effect authority | application |
| Execution-backend identity | the runtime/adapter that can directly establish it |
| Model-provider identity | attributable lower-layer observation; unknown when unobserved |
| Generated facts | owner-local generator inputs and committed output |
| Generated Baseline Provenance | `codexsdk` `baseline_metadata.json` |
| Runtime App-Server Observation | `codexsdk` initialize Server Observation |
| Runtime Compatibility | current initialize cannot prove it; `codexsdk` keeps it unknown |
| Current-source cohort verification | `docs/verify.md` and required PR workflow |
| Published dependency closure and release order | `docs/release.md` |
| Verification procedure | `docs/verify.md` |
| Release identity and procedure | `docs/release.md` |
