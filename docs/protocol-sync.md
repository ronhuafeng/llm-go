# Codex protocol synchronization

One native Codex goal owns the complete fixed-target upgrade. The
[upstream skill](../.agents/skills/codexsdk-sync-upstream/SKILL.md) defines its
passing standard; the [goal prompt](../.agents/skills/codexsdk-sync-upstream/goal.txt)
contains the objective and trusted launch bindings. An ordinary `codex exec`
prompt containing `/goal` does not enable native goal continuation.

The native workflow is deployed on main through
[#421](https://github.com/ronhuafeng/llm-go/pull/421). Its engineering tests and
the actual target-upgrade acceptance are separate obligations in
[#420](https://github.com/ronhuafeng/llm-go/issues/420). After review of the complete
prompt and authorization of the fixed-target run, use the existing main entrypoint.
The target synchronization PR remains Draft during the native goal. Only successful
trusted completion and final exact-head acceptance permit the controller handoff
to Ready and existing Auto-forward. A green workflow implementation alone does
not establish a completed upgrade.

## Scheduled detection

The accepted main workflow checks upstream on weekdays at 10:00 UTC and keeps
its manual entrypoint. Before schema builds, Codex setup, or App-token creation,
a read-only native preflight resolves the latest stable tag and peeled commit
(or the explicit manual target). It skips an already accepted commit. An existing trusted open native sync PR
returns actionable `needs_recovery` with its exact identity instead of silently
skipping forever or allocating a second PR. Repository, base, publishing App identity,
and branch identity must be valid; unknown API results fail closed.

Each new goal retains that selected target. Preparation checks that generation
resolved the same ref, kind, and commit before publication. Detection never
resumes an old goal, resets its deadline, marks a PR ready, or integrates it.
A skipped or blocked detection is not an upstream acceptance result.

## One goal, one acceptance standard

The trusted launcher fixes the accepted base, exact upstream ref/peeled SHA,
credential-free candidate workspace, dedicated Draft PR/branch, model and runtime.
It uses `gpt-6.1-sol/high`; official latest CLI is resolved, installed, checked
and recorded once per run. The selected schema source is independent of that
CLI version. Live retains its own existing `gpt-6-sol/high` fixture.

The goal chooses how to generate, adapt, correct justified verifier assumptions,
verify and repair. Codex owns continuation through its native GoalSet API; there
is no host-authored repair round, verifier role, separate verifier PR or merge
continuation. Goal completion is only a lifecycle fact. Acceptance additionally
requires unchanged candidate bytes and actual exact-head checks.

The fixed no-argument `verify_upstream` tool:

1. Freezes tracked and intended untracked candidate content. It runs source,
   generated and exact-source provenance checks without credentials or network,
   using trusted prewarmed inputs. Target-schema probes and original-baseline
   public regressions remain real checks; candidate verifier changes execute
   inside the same existing isolation boundary.
2. On local success, validates the fixed repository, Draft PR, App actor, branch,
   original base, exact target and snapshot. It appends one normal commit
   to the observed head. Identical content reuses the same head. Uncertain effects
   are read back; another remote head is not silently adopted. The remote Git ref
   owns the exact head and accepted main SHA; lagging PR head/base SHA
   projections do not invalidate those refs. The PR still owns repository,
   ref name, number, open/Draft state and creator identity.
3. Waits for that exact head's latest trusted PR-verification attempt, including
   the existing real Live job. It does not dispatch retries. The same goal reads
   the complete diagnostic JSONL path and repairs a failed candidate.

Local checks and CI call the same gate implementations. Source, generated,
provenance and Live remain the four contexts in [verify.md](verify.md). A thin
CI launcher also records compiler/start failures when the Go gate itself cannot
start. Diagnostic files have a 16 MiB hard bound: overflow or incomplete output
is explicitly incomplete, never a truncated successful report. The model sees
only a file path; it reads records incrementally. Do not retain model transcripts.

The upstream run uses native Actions steps for preparation, goal execution,
evidence upload and its final summary. Within the single goal step, controller
log groups show local verification, Draft publication, CI observation and final
acceptance. CI progress is emitted only when the existing observation changes;
it does not add polling, checks or retries. Intermediate failures remain in the
log; a terminal launcher failure emits an Actions error annotation.

`protocolupgrade goal-summary` renders the retained source, binding and result
without network access. It reports the actual goal step outcome, native status,
last controller stage, safe stopping reason and PR/CI/evidence links. Checks are
shown only for the retained published head. Missing or incomplete files remain
unavailable, and native completion is never promoted to acceptance. The summary
is an end-of-run snapshot; live progress remains in the log and target PR checks.
Model editing/generation phases are not inferred from its private transcript.

Contract-grounded verifier changes belong with implementation in the same
proposal. Legitimate evolution must pass and real defects must still fail;
expectations derive from the exact source and accepted public contract. Source
identity, credential isolation, publication and required-check controls remain
owned by the trusted launcher. Publication has no file-path allowlist. The
result and publication diagnostics retain all changed paths for independent
review; the goal's final report explains risks from changes beyond protocol
generation and adaptation. Review checks that the acceptance contract was not
weakened, including when the proposal changes verification or control code.

## Runtime and effect boundaries

The complete App Server runs in a fixed Ubuntu GitHub-hosted empty-root bwrap
view: public runtime trees, exact toolchains/module cache, the candidate and its
isolated state, plus read-only diagnostic files. A private PID namespace and
fresh proc hide the publishing parent's process environment. Host HOME, GitHub
configuration, controller checkout, temporary directories and filesystem sockets
are not exposed. Provider networking remains available to the CLI; Codex's inner
model-command sandbox remains network-denied. This layout is not a claim of
support for arbitrary hosts or isolation from host TCP/abstract sockets.

The trusted parent alone holds the existing repository-scoped App publication
identity. It exposes no arbitrary command/path/ref input through the tool. The
existing CI Live trust arrangement is unchanged: candidate CI tests there have
provider access. No new Live credential broker is introduced. Missing setup or
expired credentials produce a concrete blocked outcome, not an alternative identity.

`PROTOCOL_SYNC_APP_CLIENT_ID`, `PROTOCOL_SYNC_APP_BOT_ID` and
`PROTOCOL_SYNC_APP_PRIVATE_KEY` retain their existing roles. The App requests
only existing Contents and Pull requests write grants for Draft preparation.
Each later push obtains a fresh token with Contents write for this repository,
then revokes it after the Git operation. The existing installation ID and App
key stay in the trusted parent; a long goal does not reuse the one-hour seed
token. Check/artifact observations use the read-only workflow identity. The separate
Auto-forward App is not used by the goal. The model and its verification tool
never mark Ready or integrate. After successful command exit and runtime cleanup,
the trusted launcher exports a minimal exact-head handoff. A fresh existing
publication App token with Pull requests write marks that bound PR Ready after
revalidating source, creator, head actor, remote branch, and all four latest CI
contexts. Uncertain Ready responses receive one readback, not a repeated write.
The same workflow then calls existing reusable Auto-forward with explicitly
scoped secrets. The trusted integration controller may hold the integration App
configuration and existing provider credentials. Any bounded repair uses the
existing GoalFlow process isolation: the model sandbox receives only provider
access, never App credentials, host process environments, or a host credential
service. Effect tokens are minted and revoked by the trusted controller around
validated writes. This FF-only foundation does not use provider credentials; the
richer convergence controller owns repair. Publication credentials are not passed
to Auto-forward.
A deadline fixed at workflow start bounds the entire chain and is never reset
after completion. No tags or releases are created.

Native terminal-goal, idle-turn and observed-completed-turn conditions are all
required. Blocked/paused/limit/cancellation/disconnect/cleanup errors cannot imply
acceptance. The launcher supplies one finite deadline, including verification
and CI wait. It never resets the old pipeline's exhausted checkpoint or budget.

## Publication intent and explicit same-PR recovery

Publication has three different facts: intended effect, observed effect, and
accepted candidate. None substitutes for the next. The controller first
prepares its private push credentials and exact-ref hook. Before executing Git,
it retains a bounded `publication-intent.json` and minimal parentless
`candidate.bundle` using synced, atomic file installation in its private
publication directory. An interrupted, incomplete pair fails closed. The intent fixes
repository/PR/ref/actor, original B, verified M, U, previous H, intended H′,
whole-tree digest and the unchanged execution deadline. The trusted pre-push
hook compares the server-advertised old ref with exact H; the normal push is
never force-enabled. Preparation failure consumes no effect attempt. Once Git
execution starts, an error or a read of old H is not proof that no write occurred.

A lost response is reconciled by reading Git and the same PR identity. Only the
exact intended H′ with the recorded parent/tree can become a locator. Missing
readback, old H or a foreign head blocks further writes; no retry/fallback is
inferred. Reconciliation runs before the next local check and before a launcher
starts a model. Discovering H′ still requires fresh local checks, unchanged
candidate bytes, the latest exact-head four CI contexts and native completion.
An unchanged re-adopted candidate is valid; an empty seed with green CI is not.
Safe diagnostics preserve token failure category/status and Git exit status,
never raw credential-helper output, provider text or token values.

The workflow retains the enriched launch binding, intent and bundle with its
existing evidence. The result separately records the last intended SHA so a
missing or stale active pair cannot masquerade as an unattempted write. A
re-adopted source pair is retained separately from the new execution's active
intent, preserving recovery continuity without renewing the old receipt. This is host-local durability until artifact upload succeeds,
not a guarantee across runner death. Missing or incomplete retained source is a
blocker, not permission to recreate or attest a vanished candidate.

Recovery is an explicit manual request, not a rerun or a scheduled model retry.
Supply `recovery_run_id`, `recovery_pr_number` and `recovery_expected_head` to the
accepted main workflow. `goal-recover` authenticates the terminal unsuccessful
run/latest attempt and its uniquely named artifact, including its digest, and
recovers B/U/PR/deadline from those records rather than operator guesses. It
checks the current App-owned Draft and exact head, fixed target, accepted main
and linear fast-forward ancestry before any schema build, credentials or model
execution. The current accepted controller M executes; original B remains a
separate immutable compatibility input. Fresh launch records preserve that B/U
and use the admitted existing PR, never `goal-prepare` to allocate another one.

An expired attempt requires explicit `authorize_new_bounded_execution`; its new bounded
execution does not renew the old intent or resume a private native thread.
`regenerate_empty_seed` separately permits a new candidate only for a proven
unchanged empty seed with no retained publication intent. A retained candidate
must use its authenticated bundle and observed intended head. Every new execution
starts fresh verification; prior checks and goal status are diagnostic only.

Recovery deliberately stops when current M is not an ancestor of retained H.
Appending single-parent commits cannot repair that graph. It requires a separate
authorization decision for an exact-lease rewrite or replacement, not automatic
force-push, merge commit, new PR or relaxed integration policy. Legacy artifacts
without complete launch identity/deadline also fail closed. In particular, the
old #464 seed cannot be claimed resumed: its candidate was not retained, its old
binding is incomplete, and its branch diverges from current main. Controller
repair delivery uses an independent development PR, leaving that upgrade binding
untouched for an explicit later recovery decision.

Offline regression ownership: `goal_publication_intent_test.go` exercises actual
local-Git unknown-effect traces and bounded reconciliation combinations;
`goal_flow_test.go` proves fresh verification after locator discovery;
`goal_recovery_test.go` covers authenticated admission, missing source, expired
execution and graph blockers; `goal_controller_test.go` preserves B/M separation.
These tests do not dispatch a workflow or prove a production upgrade. No TLC run
or production recovery is implied by their success.

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

Decoding policy follows the entry point. Standalone public `json.Unmarshal`
and transport `ActionBearingMessage` decoding reject undeclared record fields.
Transport `ServerObservation` decoding ignores unknown fields recursively while
still validating known fields, requiredness, nullability, and union variants.
The same nested type can appear in both roles; its name does not choose policy.
These existing roles remain unchanged by protocol synchronization.

The source-derived oracle invokes standalone public `json.Unmarshal`, so its
record overlay is strict: named object records at schema roots or definitions
reject undeclared fields, including empty records with no `properties`, when
`additionalProperties` is absent. This applies uniformly to existing and new
records in that entry point, not to `ServerObservation`. It is a local Go API
contract, not a claim that upstream Rust empty structs reject unknown fields. Explicit
`additionalProperties: true` or schema-valued maps retain their declared open
behavior. Anonymous open union alternatives remain open and are evaluated as
part of the whole union; unconstrained `true`, `{}`, and annotation-only schemas
remain unconstrained. Neither silently ignoring nor retaining unknown fields
satisfies the standalone strict record's rejection contract. Role-specific
regressions separately prove observation tolerance and action-bearing rejection;
the standalone oracle does not certify a lossless observation/relay API.

Frozen JSON values, diagnostic strings and source-location objects share
immutable storage across the complete target graph. The controller counts unique
retained metadata and JSON payloads plus per-probe references against the same
128 MiB corpus limit.
The child wire stores JSON values verbatim in a shared table; each independent
probe references that value and a diagnostic string table. Controller
source locations remain available in failure JSONL rather than being duplicated
in an unused child field. This representation does not merge target obligations
or change the 8 MiB compressed bound. Candidate decoders receive fresh input
bytes so they cannot mutate the frozen expected value.

The oracle checks each wire witness in its own Go subtest, independently of the
declared-field check. An assertion failure does not suppress later witnesses for
the same contract. Operator console summaries still use the existing diagnostic
bounds and mark omitted output; the repair file retains complete failure records
within its explicit file limit. Compiler errors, panics, skipped or missing trusted tests remain
failed or unavailable evidence.

Round trips preserve JSON member presence except the accepted parameterless
`ClientRequest` canonical encoding. At that source document's top-level envelope,
a simple singleton-method branch with required `id` and `method` and optional
null-only `params` permits decoding explicit null and encoding without params.
Eligibility comes from the exact raw source branch, not a method name or Go
binding. The comparison permits only that direction of omission with the same
method; other members still compare exactly. Required params, nullable non-null
params, nested or ordinary fields, and other envelope roles receive no exception.
Additional envelope constraints or ambiguous discriminators retain exact
comparison until separately reviewed. This rule changes neither source validity
classification nor action/observation decoding policy.

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

## Native commands and final evidence

`protocolupgrade sync` still resolves, generates, plans and applies a ready
candidate; an unresolved representation remains a useful diagnostic for the same
goal. `-offline-upstream` uses the explicitly prewarmed official-origin ref and
commit, without replacing the generation implementation. The goal may use native
`compare`, `apply` and `check` while editing; `check` alone is not full acceptance.

`verify` performs local checks only and requires an external trusted source cache.
`verify-ci-gate` runs one of the same four checks in its established CI context.
The fixed tool composes local success, append-only publication and actual CI;
the model cannot substitute the CI-only entrypoint to obtain host credentials.

`diagnose` and `sync -force-compare -validation-only` retain their read-only
workflows. The latter rebuilds the checked-in exact source and never invokes an
Agent, accepted-worktree Apply, commit, PR mutation, tag or release.

Record actual base/target/head/PR/run and all four results. A base moving during
a verification attempt blocks that attempt; the same native goal can request
fresh verification against current main while preserving original-baseline
obligations. A late result cannot overwrite newer work. The trusted completion
handoff uses the protected exact-head route in [auto-forward.md](auto-forward.md).
Implementation tests do not authorize a production merge or establish rollout evidence.

## Completion handoff and rollout evidence

The success-only `goal-handoff.json` is written outside the model workspace after
native status `complete`, final unchanged snapshot acceptance, successful exact-head
checks and runtime cleanup. `goal-result.json` remains diagnostic output on both
success and failure and is never itself authorization to mark Ready. Failed,
blocked, cancelled, empty-seed, changed-head or changed-target results cannot
produce a usable handoff. A repeated finalization of the same Ready head performs
read-only reconciliation; it does not replay the mutation.

Local mocked-API/isolated-Git fixtures prove these boundaries. A real production
Ready → Auto-forward integration chain requires the reviewed workflow to land and
an authorized upstream run; ordinary PR tests do not establish that rollout proof.

## Rebinding a fixed target after main moves

Original B, newly accepted main M, fixed upstream U and new candidate H′ remain
separate identities. `ResolveGoalRebinding` reads immutable B/M baseline metadata
and reuses the existing stable-target policy. Exact U already accepted is a no-op;
a newer stable main supersedes U. Missing metadata, manual-track ambiguity,
same-tag/different-SHA movement and downgrades fail closed.

A controller constructing H′ must preserve U and pass both accepted checkouts:
`BaseRoot=M` plus `OriginalBaseRoot=B` (`verify`/`verify-ci-gate` expose
`-original-base-root`). The same provenance contract checker then executes both
sets of immutable accepted regression obligations. Dropping tests from M cannot
remove obligations promised at B, and B cannot hide new M regressions. This is
additional input to the existing provenance gate, not another check context.
Old H's results never certify H′; all four current exact-head CI contexts remain
required after every candidate change. Classification alone never grants
integration or claims either baseline's tests executed.

During an active goal, each `verify_upstream` call refreshes an immutable accepted
M checkout and evaluates its baseline against fixed U. If M advanced legitimately,
local provenance checks both M and original B. The publication binding records M
separately; moving main again invalidates that attempt and returns diagnostics to
the same native goal. Neither the proposal nor B is silently rewritten. If main
moves after native termination, final observation may hand off the unchanged
accepted H/B/U only after monotonic-main and fixed-target policy checks. It does
not claim the newer main contracts were verified on H. The handoff retains the
last actually certified `verification_base`; integration must preserve its
ancestry while obtaining current-main obligations on H′. A completed runtime is
never resumed by this helper.

The FF-only reusable foundation validates the inherited deadline, exact U at H,
B/verified-M/current-M ancestry, and requires current main to equal the last
actually verified baseline before writing main. A later M can still receive a
legitimate completion/Ready handoff, but integration reports reconciliation
required until the richer convergence implementation executes both immutable
contract obligations and all four H′ checks. Branch cleanup also retains the
same inherited deadline.
