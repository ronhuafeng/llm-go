# Codex protocol synchronization

This document owns how `codexsdk` moves between selected Codex App Server
protocol sources. The selected source owns wire meaning. Runtime integration
acceptance is separate and is defined in [`live-codex.md`](live-codex.md) and
[`verify.md`](verify.md).

## Source and runtime are different proofs

An accepted protocol baseline records exact upstream source identity together
with schema and generated Go. A target is one selected upstream ref and peeled
commit. A candidate is fresh temporary construction for that target, not an
accepted baseline or durable repair queue.

Protocol provenance may build the selected Rust source to reconstruct schema.
The real integration suite instead uses the corresponding official installed
Codex release as an external black-box runtime. Do not confuse the schema-build
executable with the production/runtime fixture, or import the upstream Rust
test suite as a downstream conformance obligation.

The accepted live policy uses the runtime from the candidate PR's checked-in
baseline, not `latest` and not main's prior baseline. Even provenance-only
runtime advances require the applicable live check. This does not narrow
protocol generation to live-covered methods: full reachable wire representation
and scenario-based runtime evidence have distinct jobs.

**Live implementation:** #376 supplies the baseline-selected native Mini
fixture and fourth required result. #377 supplies the structured composed call
and continuous persistent Start/Resume/Fork stories through the same suite.
Their actual workflow results own execution evidence. Runtime proof and wire
reconstruction remain separate.

## Ownership

Go owns target policy, fresh construction, comparison, planning, application,
and deterministic checks. GitHub Actions owns triggers, permissions, and
publication authorization. An Agent may propose one narrowly scoped handwritten
repair for a typed unresolved representation; it does not select the target,
certify correctness, publish, or integrate.

The checked-in baseline metadata is the source authority. PR titles, visible
prose, hidden publication markers, manifests, and coverage annotations are
projections or explanatory data, not substitutes for that identity and fresh
upstream facts. Observation timestamps are not protocol meaning.

## Construction sequence

```text
accepted baseline + exact target
  -> fresh complete/stable candidate
  -> read-only Plan
  -> optional single handwritten repair for typed semantic_unresolved
  -> re-plan the same retained candidate
  -> Apply only a complete ready plan
  -> deterministic checks
  -> publish protected sync PR
  -> required PR proofs, including the accepted live runtime gate
  -> trusted auto-forward integration
```

Plan must not partially mutate the accepted worktree. It constructs and retains
complete candidate bytes in temporary state. Apply checks that the accepted
baseline has not changed and materializes the prepared result; it does not
reread upstream inputs or repeat generation. Partial generation and
surface-skipping modes are unsupported.

Planning also compiles the public SDK/protocol packages and handwritten tests
using a temporary Go overlay and `go test -c`; initializers and tests do not
execute. If the accepted baseline compiles and the candidate does not, report
typed `go_compatibility` unresolved evidence before Apply. If both fail, retain
both diagnostics without blaming upstream. Resume repeats that check; final
executable tests and the isolated generated build remain separate proofs.

One package constructor is shared by planning, reproducibility, and protocol
CLI operations. Complete and stable schema visibility are distinct inputs.
Reuse selected types/names inside a construction, derive the facade from those
facts, and check generated/handwritten declaration and receiver collisions in
each real Go package. Diagnostics retain available source and Go locations;
never infer upstream ownership by reversing emitted names.

## Fresh wire facts

Construct method/field/type facts in memory from the current candidate. Do not
write manifest/coverage projections and reload them as fresh authority.
Complete schema supplies presence, requiredness, nullability, and types. Stable
schema from the same commit supplies stability. That commit's `common.rs`
supplies request/response mapping; missing or ambiguous mapping fails instead
of falling back to the accepted manifest.

Old manifest/coverage annotations may remain explanatory comparison input.
They cannot suppress a method, preserve stale stability, select stale fields,
or alter requiredness. Checked-in regeneration may use derived metadata as
inputs for reproducibility; fresh reconstruction independently checks agreement
with exact upstream. Only `baseline_metadata.generated_at` is excluded from
exact semantic comparison.

Derive lossless representations from shape rather than today's field names:
scalars, enums, supported unions, required/optional/nullable values, reachable
references, and unconstrained JSON all retain their wire meaning. `true`, empty,
and annotation-only JSON schemas accept every JSON value and may map to
`protocolv2.JSONValue`, including array items and map values. Real constraints,
unknown keywords, empty enum/union alternatives, and false schemas must not be
silently treated as unconstrained. Optional JSONValue preserves absence versus
null; the OutputSchema overlay intentionally rejects null.

Use explicit overlays only where schema cannot express a required local/public
meaning. Application authority, lifecycle correlation, command argv, service-tier
presence, nested elicitation schema documents, and opaque realtime lifecycle
representations retain their focused owner tests and existing public contracts.
A new protocol instance is not itself a reason for a handwritten checkpoint.
Changing an established public representation requires an explicit migration;
repository search cannot establish every external consumer.

A small facade-name map may preserve public acronyms/names. It has no authority
over method presence, stability, parameters, responses, or type generation.
Unrepresentable meaning fails closed; do not drop fields or introduce lossy
`any`/`interface{}` passthrough just to advance the baseline.

## Reachability and generated surface

Current manifest wire roots, not convenience facade usage, own the reachable
type graph. Request, response, notification, server-request, and aggregate
payload roots remain relevant even when no convenience method is exposed.
Closed RPC error payloads and JSON-RPC request identity are independent roots
when their consumers need them. Missing production method facts cannot promote
every coverage type to a root; small fixtures declare their roots explicitly.

Handwritten transport validation owns the six JSON-RPC envelope roles, not the
public generated surface. Other payloads are not excluded merely because their
names begin with `JSONRPC`. Traversal follows only references represented by the
accepted mapping; an intentional JSONValue overlay terminates typed traversal.

A definition is generated when reachable from a wire root and losslessly
representable. There is no path/name admission catalogue or previously-reviewed
fallback. Proven identical definitions may share a generated type; wire-identical
local/top-level definitions may reuse an identity. Different same-name shapes
receive deterministic scoped identities or fail safely. Preserve explicit
representation overlays separately from admission.

Public facades derive from current method constants and generated params/response
types. Missing prerequisites fail rather than silently deferring a public
method. Historical `facade_status` cannot overrule current construction;
`internal.*` targets remain intentionally non-public. Protocol manifests and
coverage remain generator machinery, not a second live capability registry.

## Pending PRs and recovery

Before spending new generation or Agent work, observe native GitHub PR/branch
state. An unchanged App-owned pending PR for the same base/target can be returned
with current check observations; that acquires no new proof and does not mean
main has integrated it. Main's checked-in baseline owns accepted state.

Check ownership using the configured App bot ID, native PR creator/latest ref
activity actor, actual repository/base/head, source identity, after-SHA, and
allowed paths. Client ID only mints a token; commit author text and editable
metadata do not establish push authority. Ambiguous ownership or human source
interference requires maintainer intervention. Manually closed candidates stay
paused, a changed upstream tag is an integrity failure, and an older attempt
must not replace a newer pending stable target.

When the target or base changes, reconstruct and validate against that exact
base. Carry the observed old head to publication and use an explicit Git lease
for an existing branch. Recheck native state after writing. The PR API's
`base.sha` may lag main; selected checkout and remote base ref own the base.
Read back lost write responses before repeating effects. An orphan App-owned
branch must be rebuilt and verified before its PR is completed.

New branch names derive from target identity and accepted base SHA, not run
number or timestamp. B/U retries reuse the same publication epoch; a later
proposal after an earlier merge gets a distinct base-derived branch. Open
pending branches retain their name on update. Native refs/PRs and accepted
metadata are sufficient cross-run state.

`repair_pending` is explicit maintainer recovery for an open App-owned sync head
that is not a single candidate commit. It does not regenerate protocol files.
Replay its valid protocol diff from the parent already contained in main onto
current main, then update with force-with-lease so later main changes remain.
Do not repair a single-commit head, a closed candidate, or mismatched baseline
identity. Recovery is not the integrator's responsibility.

## Agent scope

Only owner-identified unsupported representation or missing mapping/prerequisite
produces `semantic_unresolved`. I/O, malformed source, identity, storage,
environment, and other ordinary failures retain their cause and fail normally.
Never parse error prose to authorize an Agent.

The one Agent pass receives the already selected target and concrete
stage/path/reason. Read `codexsdk/internal/protocolsync/changes.go`; its policy
owns allowed handwritten Go/test paths. Changes to sync policy, acceptance,
identity, publication, validators, or unknown generated paths are outside that
repair. Report `needs-maintainer` rather than enlarging scope.

The trusted workflow holds a digest of the complete per-run candidate outside
the proposal. Before re-planning, check Git-visible scope and copy candidate
inputs into an isolated directory only if every byte/path and target matches.
Missing, added, redirected, or altered inputs fail. Use the same copy for the
second Plan and Apply.

Before proposed code executes and after tests, verify the prebuilt control
binary and run its scope check. Normalize changed Go before sealing the patch,
compare proposal bytes before/after executable checks, and recheck candidate
identity. A separate publication runner verifies the patch and uses control
built from trusted checkout. Proposed code never receives repository-write
credentials. A second unresolved plan fails without publication.

The protocol Agent's model/effort is owned by its workflow, independently of the
live-suite model. Do not change live scenarios, runtime selection, or acceptance
rules to make an automatic protocol repair pass green.

## Deterministic proof and build inputs

Before publication, require exact target identity, matching fresh schema,
reproducible generated types/facades, preserved wire presence/nullability,
focused tests for handwritten semantics, `go vet ./codexsdk/...`, and
`go test ./codexsdk/...`. Repository-wide acceptance remains separate.

Remove ambient Rust/Cargo overrides and set owned cache locations explicitly.
Reject external Cargo configuration in Cargo home or ancestor directories;
selected source configuration and its toolchain declaration remain authoritative.
Cached rustup directory overrides cannot change the selected toolchain. This
bounds build inputs without claiming a hermetic OS/compiler installation.

Where release Cargo.toml versions differ from the workspace entries in
Cargo.lock, inspect `cargo metadata --no-deps` without dependency resolution.
Prepare the lockfile by changing only local workspace versions and explicit
internal version references. Preserve third-party versions, sources, checksums,
and dependency edges. Missing/ambiguous identities fail; already consistent
locks remain byte-identical. Never run an unlocked dependency update.

Retain original and prepared lockfiles and their SHA-256 digests, including on
failure. The original is upstream evidence; the prepared file is the actual
build input. Schema generation and version queries use `cargo run --locked`;
reject later changes to the prepared lock or tracked source. Log commands;
caches only accelerate that selected build.

Final validation reconstructs complete/stable schemas and `common.rs` from the
checked-in exact source, uses the same derivation in an isolated module root,
and compares schema, baseline metadata, manifest generation rules, manifest,
coverage, and all four generated Go files. Only `generated_at` is excluded.
Stale requiredness, stability, mappings, identity, or generated artifacts fail
even when checked-in Go agrees with stale metadata. Validation-only invokes no
Agent, accepted-worktree Apply, or publication.

## Results and effects

Keep native results distinct:

| Result | Meaning |
| --- | --- |
| `baseline_matches` | Baseline identity already matches; no fresh upstream proof. |
| `schemas_match` | Read-only schema comparison only; no provenance advance or full derived proof. |
| `exact_verified` | Fresh reconstruction matches semantic artifacts; other PR proofs remain separate. |
| `plan_ready` | Read-only construction succeeded; not applied. |
| `applied` | Candidate materialized locally; not published or integrated. |
| `pr_pending` | An observed/published PR awaits current acceptance and integration. |

A provenance-only plan advances the exact source identity when a newer target
produces unchanged protocol bytes. Mechanical drift needs no handwritten
semantic repair; semantic drift may use the one bounded pass. Neither permits
bypassing runtime acceptance.

Failures report the last native stage, known target, and an owner-assigned
category only when established. Unknown attribution stays unknown. File
locations are diagnostics, not repair authority. A green summary step cannot
certify failed/cancelled work, and an earlier Apply does not override a later
failure.

Publication creates or updates a protected proposal only after deterministic
proof. It does not merge, tag, release, or distribute the module. Open Git refs
and the checked-in baseline own identity; the hidden `codexsdk-upstream-sync`
block is recoverable metadata. Preserve operator-owned visible PR prose rather
than making its wording correctness authority.

## Existing publication App setup

Use a repository-scoped GitHub App with Contents and Pull requests read/write,
installed only on `ronhuafeng/llm-go` where possible. Configure:

- Actions variable `PROTOCOL_SYNC_APP_CLIENT_ID`: the App Client ID used to mint
  tokens, not its installation ID;
- Actions variable `PROTOCOL_SYNC_APP_BOT_ID`: numeric bot user ID used for
  ownership checks;
- Actions secret `PROTOCOL_SYNC_APP_PRIVATE_KEY`: the private PEM, never source
  or diagnostic content.

The separate publisher uses a read-only built-in token, then requests only the
repository/write grants needed for publication. Only the publication effect
receives the short-lived token; it is not a job output. Agent/proposal tests
and read-only verifiers receive neither App key nor token. Missing or invalid
configuration fails with no fallback identity.

Observe required checks on actual bot-created/updated PR events; a manual
workflow dispatch alone does not prove that event chain. Publication authority
is separate from the Auto-forward integration App and from Mini credentials.

## Final acceptance and native commands

The current PR head H is the sole candidate. Required source, generated, fresh
H/U provenance, and the accepted live-runtime proof must satisfy
[`verify.md`](verify.md). The live test uses H's synced official runtime and the
active scenarios; a passing schema build is not runtime evidence.

Current main must remain an ancestor of H. The existing trusted integration
route may only fast-forward to that verified H. A stale proposal needs new
construction/rebase and new results; the integrator does not repair it.

The Go owner exposes `protocolupgrade sync`, `scope`, `resume`, `check`, and
publication/staging control. Ready plans apply; unresolved plans leave the
accepted worktree untouched; resume re-plans the same candidate after the
bounded handwritten proposal. `validation_only` requires `force_compare` and
binds to the checked-in source; an explicit ref must resolve to that identity.
It is diagnostic and performs no commit, push, PR mutation, tag, or release.

Read the current command/workflow inputs rather than inventing another control
path. Keep the accepted live integration migration separate from deterministic
schema validation. See [`auto-forward.md`](auto-forward.md) for integration.
