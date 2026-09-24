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
nullable/required/optional shape  -> matching wire-preserving Go representation
$ref / definition reachability    -> generated dependency closure
string enums / reviewed unions    -> generated named types
```

A new protocol instance is not, by itself, a reason for a handwritten
checkpoint. If the generic mapping preserves the entire wire value and its
presence/nullability semantics, generation should accept it.

Keep an explicit semantic overlay only when schema facts are insufficient to
derive the required local meaning. Examples include application-owned authority,
lifecycle or correlation semantics, and a deliberately narrower public behavior
that is separately justified. An overlay must identify its owner, invariant,
exit/revisit condition when useful, and focused tests.

Unknown or unrepresentable schema meaning fails closed. Do not replace an
unsupported shape with `any`, `interface{}`, silent field dropping, or another
lossy passthrough merely to advance the baseline.

## Dependency generation

Public protocol generation starts from current protocol roots: request,
response, notification, and server-request payloads selected by the manifest.
Referenced definitions are dependencies of those roots.

Prefer dependency reachability over a growing inventory of approved definition
names. Two structurally identical definitions may share one generated type when
the generator can prove that identity; conflicting definitions must remain
distinct or fail deterministically.

Handwritten types remain justified only when they protect semantics that the
selected schema and generic generator cannot own.

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

A provenance-only advance records the newer exact source identity as the
accepted baseline. Do not persist a second "verified upstream" state when the
accepted baseline can carry the fact directly.

## Agent boundary

The Agent is exceptional, not part of routine regeneration.

It receives the already selected exact target plus concrete drift/failure
evidence. It may change only the owner-local handwritten code and tests required
by that evidence. It must not choose a different target, stage, commit, push,
publish, merge, tag, or inherit repository-write credentials.

After the proposal, Go re-plans and performs deterministic checks. A model final
message has no acceptance meaning.

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

Repository-wide required verification remains separate; see
[`verify.md`](verify.md).

## Publication and effects

Publication is allowed only after deterministic proof. It creates or updates a
protected protocol-sync PR from the proven worktree. It does not self-merge,
publish the module, or create a release.

Repository-write credentials belong only to the publication step. Base movement
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
