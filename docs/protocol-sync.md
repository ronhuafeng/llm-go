# Codex protocol synchronization

This document is the current authority for how `codexsdk` evolves from one
selected Codex App Server protocol source to another.

The goal is not to preserve the implementation shape used for the previous
upgrade. The goal is to preserve protocol meaning on the exact selected upstream
source with the smallest current downstream semantics.

## Authorities

Three states are sufficient:

- **Accepted baseline:** the checked-in schema baseline, generated Go surface,
  and exact upstream source identity that have passed repository proof.
- **Target:** one exact `openai/codex` tag, ref, or commit selected for the next
  comparison. The peeled commit SHA is immutable identity; the selected Codex
  source owns wire facts.
- **Candidate:** freshly generated temporary state for the target. It is
  comparison/reconstruction input, not an accepted baseline and not durable
  cross-run state.

GitHub Actions owns triggers, permissions, secret isolation, and publication
authorization. Go owns target policy, schema/generation semantics, comparison,
planning, application, and deterministic checks. A model may propose a targeted
semantic change but never certifies correctness or performs repository effects.

## Evolution contract

Normal evolution is:

```text
accepted baseline
        +
   exact target
        |
        v
generate fresh candidate
        |
        v
PLAN (read-only)
        |
        +---- exact/current --------------------------> deterministic proof
        |
        +---- newer target, no schema drift ----------> provenance-only plan
        |
        +---- mechanically representable drift ------> deterministic plan
        |
        +---- unresolved semantic/generator drift
                         |
                         v
                 one Agent proposal
                         |
                         v
                      re-plan
                         |
                  unresolved? -- yes --> fail closed
                         |
                         no
                         v
                       APPLY
                         |
                         v
                deterministic proof
                         |
                         v
                 protected sync PR
```

Planning must not partially mutate the accepted worktree. Candidate schemas,
manifests, generated Go, and compatibility results should be produced or
validated in temporary state first. Application begins only after the selected
target has a complete deterministic plan, including any narrowly reviewed
semantic change.

A successful Plan retains the complete candidate bytes. Apply materializes that
result after checking the accepted baseline has not changed; it does not reread
upstream inputs or repeat generation. The direct apply command uses the same
construction path. Partial generation and surface-skipping modes are unsupported.

Protocol generation shares one package constructor across upgrade planning,
reproducibility checks, and the protocol CLI. Stable and complete schema visibility
are distinct construction inputs. Type selection and naming are reused within
each construction, and the facade consumes those generated facts directly.
Package collisions are checked from Go declarations in each actual package,
including handwritten declarations and receiver scopes. Diagnostics retain Go
file locations without reconstructing upstream ownership from emitted names.
The isolated compiler check remains the final generated-package proof.

A failed run is retried from the selected upstream source and a fresh candidate.
Do not create repair queues, cross-run state machines, or proof ledgers for
state that can be regenerated.

## Protocol facts and semantic overlays

The selected Codex schema is the authority for wire facts. The generator should
derive lossless mappings from stable schema properties rather than from lists of
today's field paths or type names.

Examples of mechanically owned facts include:

```text
JSON Schema true                  -> protocolv2.JSONValue
array items: true                 -> []protocolv2.JSONValue
additionalProperties: true        -> map[string]protocolv2.JSONValue
string/integer/boolean scalars    -> matching Go scalar
number with format: double         -> float64
nullable/required/optional shape  -> matching wire-preserving Go representation
$ref / definition reachability    -> generated dependency closure
string enums / supported unions   -> generated named types
```

A new protocol instance is not, by itself, a reason for a handwritten
checkpoint. If the generic mapping preserves the entire wire value and its
presence/nullability semantics, generation should accept it.

For each candidate, the complete schema supplies methods, types, fields,
requiredness, and nullability. Method and type stability come from presence in
the stable schema generated from the same upstream commit. Request response
types come from that commit's `common.rs`; a missing mapping fails instead of
falling back to the accepted manifest. The accepted manifest and coverage are
comparison input and may retain local explanatory annotations, but their old
wire facts do not decide the candidate. `generated_at` records observation time:
it may be retained for the same exact source commit and is excluded from
protocol-semantic comparisons. Method coverage status and classification source
descriptions are regenerated from the current input; an old classification
cannot suppress a current method. Ambiguous schema type names fail closed.

Facade target names are local public Go API representation. A small handwritten
name map keeps established acronyms and notification names where the wire method
and schema title cannot derive them. It has no authority over method presence,
stability, parameters, responses, or type generation; revisit entries when a
deliberate public API rename is accepted.

Keep an explicit semantic overlay only when schema facts are insufficient to
derive the required local meaning. Examples include application-owned authority,
lifecycle or correlation semantics, and a deliberately narrower public behavior
that is separately justified. An overlay must identify its owner, invariant,
exit/revisit condition when useful, and focused tests.

Unknown or unrepresentable schema meaning fails closed. Do not replace an
unsupported shape with `any`, `interface{}`, silent field dropping, or another
lossy passthrough merely to advance the baseline.

## Dependency generation

Protocol type generation starts from the manifest's wire roots, independently
of whether a convenience SDK facade method is currently exposed. Request,
response, notification, server-request, and aggregate message roots own the
reachable wire graph. A facade policy may hide a convenience method, but it
cannot make an upstream wire type cease to exist.

JSON-RPC envelope schemas are traversed for typed dependencies but remain
outside the public generated protocol surface. Their own envelope definitions
stay with handwritten validation. Closed RPC error payloads with a required
typed data field are independent wire roots, so their reachable definitions are
generated even when they have no manifest method entry.

The planner follows only references accepted by the current schema mapping. A
field overlay that intentionally represents an upstream subtree as
`JSONValue`, for example, terminates typed dependency traversal at that
boundary. This keeps dependency closure aligned with the representation the
generator actually promises rather than blindly walking every raw `$ref` in a
schema document.

A definition is eligible for generated Go if and only if it is reachable from a
generated wire root and its schema shape has a lossless supported
representation. There is no path/name admission catalogue and no
"previously reviewed definition" fallback.

Before Plan reports `ready`, each real Go package is checked for conflicts
between generated and handwritten declarations, including receiver members.
Diagnostics contain Go file locations and any source paths already available
from selected type or method facts. Generated helpers without a direct schema
owner retain their Go location; naming is never replayed to infer an owner.
Handwritten-only conflicts are source errors, not schema incompatibilities.

Two structurally identical reachable definitions may share one generated type
when the generator can prove their schema identity. A local definition that is
wire-identical to an already generated top-level type reuses that top-level
identity. Same-name definitions with different shapes receive deterministic
scoped identities or fail if the distinction cannot be represented safely.

Representation overlays are separate from admission. For example, selected
upstream string aliases that are intentionally exposed as plain Go strings may
remain inline, while `additionalProperties: true` is handled generically as an
open JSON-value object. Handwritten semantic overlays remain justified only
when schema facts are insufficient to preserve the required API meaning.

SDK facade availability is derived from the current generated protocol surface,
not inherited from historical `facade_status`. For each client-to-server request
with a public facade target, the current method constant plus params/response
types are the complete prerequisites. If they all exist, the facade is generated;
if any are missing, generation fails closed. Old
`deferred_missing_generated_types` values remain readable metadata during
migration but have no admission authority and are normalized away on the next
manifest regeneration.

This keeps product policy explicit: `internal.*` targets are intentionally not
public facades, while public facade targets follow current mechanically proven
capability rather than a remembered inability from an older generator.

## Outcomes

A target comparison has one of these meanings:

- **current:** baseline identity already matches the exact selected target and a
  fresh deterministic check succeeds;
- **provenance-only:** a newer accepted target regenerates to the same protocol
  bytes/surface, so only exact upstream provenance needs to advance;
- **mechanical drift:** schema/surface changed and the generic generator can
  produce a complete lossless candidate without handwritten semantics;
- **semantic drift:** the candidate exposes a concrete meaning the generic
  generator cannot decide; at most one targeted Agent pass may propose the
  owner-local source/test change before re-planning;
- **blocked/failure:** target policy, generation, planning, semantic work, or
  deterministic proof failed. Nothing is published.

Only an owner-identified unsupported schema representation or missing protocol
mapping/prerequisite yields `semantic_unresolved`. File I/O, temporary storage,
source identity, and malformed source errors keep their original causes and
fail normally; their message text is never parsed to authorize an Agent pass.

A provenance-only advance records the newer exact source identity as the
accepted baseline. Do not persist a second "verified upstream" state when the
accepted baseline can carry the fact directly.

## Agent boundary

The Agent is exceptional, not part of routine regeneration.

It receives the already selected exact target plus concrete drift/failure
evidence. It may change only the owner-local handwritten code and tests required
by that evidence. It must not choose a different target, stage, commit, push,
publish, merge, tag, or inherit repository-write credentials.

The initial unresolved Plan records a digest of the complete per-run candidate:
complete and stable schemas, reports, `common.rs`, and its exact source marker.
The digest is held in the workflow step output outside the Agent proposal. Before
re-planning, the trusted workflow checks every Git-visible Agent path against
the proposal scope, and Go copies the ignored candidate into an isolated
directory only if its entire content still matches that digest and target SHA.
Missing, added, redirected, or changed candidate inputs fail closed. The
isolated copy is used for both the second Plan and Apply.

Agent proposal scope contains handwritten SDK/runtime or protocol generator
Go and focused tests. The workflow and Go reject changes to sync policy,
candidate identity, Plan/Apply acceptance, generated checks, schema/generated
artifacts, and publication code. A needed change to those control paths takes
the ordinary reviewed development path rather than expanding one automatic
proposal's authority.

After the proposal, Go re-plans and performs deterministic checks after the
executable tests. The read-only job compares the complete Git proposal before
and after tests, rechecks the candidate digest, and hands the proven patch to a
separate publication runner. That runner verifies the patch digest and uses a
control binary built from the trusted checkout. Proposed Go code is never run
with repository-write credentials. A model final message has no acceptance
meaning.

## Deterministic proof

Before publication, require evidence appropriate to the candidate:

- target ref/kind/commit identity is exact and consistent;
- checked-in candidate schemas match a fresh generation for that target;
- generated protocol Go and SDK surface reproduce exactly;
- generated wire representations preserve schema requiredness/nullability;
- focused tests cover any new handwritten semantic overlay;
- `gofmt` and `git diff --check` pass;
- `go vet ./codexsdk/...` passes;
- `go test ./codexsdk/...` passes.

The checked-in generated check is a fast proof that current source inputs
reproduce generated Go. Final validation is a separate read-only reconstruction:
resolve the checked-in exact upstream ref/SHA, freshly generate complete and
stable schemas plus that commit's `common.rs`, and run the same Plan/Apply
derivation in an isolated module root. Compare schema files, baseline metadata,
manifest generation rules, manifest, coverage, and all four generated Go files
against the accepted head. Only `baseline_metadata.generated_at` is excluded
from semantic comparison. A stale requiredness, stability, response mapping,
source identity, or generated artifact fails validation even if checked-in Go
agrees with stale checked-in metadata. Validation-only invokes neither Agent
nor accepted-worktree Apply or publication.

Repository-wide required verification remains separate; see
[`verify.md`](verify.md).

## Publication and effects

Publication is allowed only after deterministic proof. It creates or updates a
protected protocol-sync PR from the proven worktree. It does not self-merge,
publish the module, or create a release.

Repository-write credentials belong only to the separate publication job. Base movement
or any uncertainty about which commit was proven causes publication to fail and
the run to restart from canonical input.

Validation-only execution performs target resolution, fresh generation,
planning/comparison, and deterministic checks but never commits, pushes, opens
or updates a PR, merges, tags, or releases.

## Final acceptance

On the same final protocol PR head `H`:

```text
PR verification(H) == success

AND

Codex Upstream Protocol Sync(
  head=H,
  upstream_ref=<checked-in exact baseline>,
  force_compare=true,
  validation_only=true
) == success
```

The validation-only run must regenerate from the exact selected upstream source
and prove the checked-in baseline without relying on artifacts from an earlier
run.

## Native control path

The current Go owner implements the boundary above directly:

- `protocolupgrade sync` resolves/generates the exact candidate and runs a
  read-only plan in an isolated temporary module root;
- a ready mechanical or provenance-only plan is applied immediately;
- `semantic_unresolved` returns structured stage/path/reason evidence while the
  accepted worktree remains unchanged;
- the workflow invokes exactly one tokenless Agent pass only for that outcome;
- `protocolupgrade resume` validates that the Agent touched only handwritten
  `codexsdk` paths, re-plans the same candidate/target, and applies only when
  that second plan is ready;
- a second unresolved plan fails closed and publishes nothing.

The protocol Agent in this workflow uses `gpt-6-sol` with
`model_reasoning_effort="xhigh"`. Model choice is execution configuration, not
correctness authority; deterministic Go proof remains mandatory.
