# Local Sync Reference

Use this file as context for local sync decisions. It is not a linear playbook. Commands and scripts own execution details; this reference preserves the domain rules that are easy to lose.

- [Baseline And Provenance](#baseline-and-provenance)
- [Artifacts](#artifacts)
- [Action Boundary](#action-boundary)
- [Target Policy](#target-policy)
- [Drift Review](#drift-review)
- [Apply And Repair](#apply-and-repair)
- [Validation](#validation)
- [Target Movement](#target-movement)
- [Decision Rules](#decision-rules)

## Baseline And Provenance

Source of truth:

- checked-in schema baseline: `internal/protocolschema/appserver/v2`
- metadata: `baseline_metadata.json`
- manifest and coverage: `manifest.json`, `coverage_matrix.json`
- generated Go: `protocolv2/*.gen.go`, `sdk_surface.gen.go`

Record selected upstream provenance as:

- `source_ref_name`
- `source_ref_kind`: `stable_rust_tag`, `manual_ref`, or `manual_commit`
- peeled full `source_commit`

Do not check in local absolute paths, `.cache/...` output paths, private repo paths, account data, or raw smoke-test transcripts.

## Artifacts

Use one module-local cache layout for both local and GitHub Actions runs:

- upstream repository: `.cache/openai-codex`
- sync output: `.cache/codexsdk-upstream-<short-sha>`
- Rust build cache: `.cache/cargo-target/codex`

These paths keep disposable state inside the ignored `codexsdk/.cache/` tree. GitHub Actions treats them as ordinary job-local workspace directories: they persist between steps in the same job, but not automatically across jobs or workflow runs. Do not assume that the directory uses the GitHub Actions cache service unless the workflow explicitly adds one.

In GitHub Actions, use the canonical paths and the workflow-owned `upstream_repo` URL. Outside GitHub Actions, use the same paths by default so local and automated runs have the same layout; honor an explicitly supplied existing repository or output path when the caller chooses one.

In normal generation mode, `scripts/codexsdk_track_upstream.sh` requires `--codex-repo` or `CODEXSDK_CODEX_REPO`; in `--compare-only` mode it needs only a resolved `--commit`, checked-in baseline, and candidate schema directory. When `--out` is omitted it creates a temporary `/tmp/codexsdk-upstream.*` directory.

Useful drift evidence:

- `reports/SUMMARY.md`
- `reports/drift_summary.json`
- `reports/matrix_update_skeleton.json`
- upstream `common.rs` response mappings from `source_commit:codex-rs/app-server-protocol/src/protocol/common.rs`

Preserve compact pre-change evidence before overwriting checked-in clean reports.

## Action Boundary

One Codex invocation owns protocol implementation through local validation. It leaves the resulting tracked and untracked changes unstaged and uncommitted. GitHub Workflow steps outside Codex own authentication, commit, publication, and landed finalization.

## Target Policy

Use the canonical resolver and target-policy script. Do not hand-roll tag sorting, prerelease filtering, annotated-tag peeling, or downgrade logic.

Prefer stable tags or named refs for normal syncs. Treat bare `manual_commit` SHA targets as explicit advanced inputs: the resolver accepts them syntactically, and tracking/fetch must fail closed if upstream cannot fetch the object.

Policy meanings:

- `allow`: drift generation may run
- `skip`: selected target is already represented; stop drift generation unless `force_compare` requests read-only verification
- `block`: stop before drift generation; a `force_compare` run must fail rather than report successful verification

Do not convert a `block` into remote publication.

## Drift Review

Classify drift before checked-in baseline changes:

- method drift
- schema file drift
- generated Go type drift
- SDK surface class: `metadata-only`, `generated-only`, `public-facade-required`, or `ignored-internal`
- handwritten SDK impact, with reviewed drift evidence
- coverage impact for new or changed surface

If new methods appear, compare stable vs experimental schema presence before deciding public SDK exposure. Leave experimental or internal upstream-only surface in generated `protocolv2` unless the user asks for a public facade or existing manifest rules require it.

## Apply And Repair

Use `scripts/codexsdk_apply_sync_candidate.py` for mechanical candidate application. Do not copy schema/report files by hand.

`common.rs` must be bound to the same target commit as the candidate. Use `target_sha:codex-rs/app-server-protocol/src/protocol/common.rs` and provide its source SHA to the apply script; when an upstream clone is available, the script verifies the file content against that commit.

Expected mechanical sync surface includes:

- `internal/protocolschema/appserver/v2/**` for schema JSON, baseline metadata, manifest, coverage, and checked-in clean drift reports
- `protocolv2/*.gen.go`
- `sdk_surface.gen.go`

Handwritten SDK, test, or doc changes are justified only when they preserve compatibility, expose already-supported stable surface, fix an existing facade broken by new schema, update tests/docs for real user-visible behavior, or are explicitly authorized by the user. Without reviewed drift evidence or explicit authorization, keep them out of the sync commit.

Prefer typed `protocolv2` params/responses over raw JSON-RPC escape hatches.

## Validation

Use `scripts/codexsdk_validate_sync.sh` for the full local validation path when candidate and target inputs are available.

Validation should prove:

- checked-in baseline matches the trusted candidate and target SHA
- generated files reproduce exactly
- manifest and coverage no longer reference removed upstream surface
- checked-in reports are clean and sanitized
- no local absolute paths or cache output paths leaked into checked-in baseline metadata or checked-in reports

Capture the mechanical and final change sets with `scripts/codexsdk_sync_changes.py`. The final manifest includes untracked files, rejects paths outside `codexsdk/` and automation-owned cache/skill paths, and is the only source used for staging.

After validation passes, stop with the final manifest. Do not stage or commit it.

When validation fails, inspect the first actionable failure before adding code or abstractions.

## Target Movement

For default scheduled syncs, compare against the latest stable `rust-vX.Y.Z` tag, not `main`.

Do not enter an unbounded loop chasing moving upstream refs. If the target moved, report the exact old/new target and whether remaining drift is real.

## Decision Rules

- If drift is clean and the user only asked to check a target, report no SDK update is needed.
- If an allowed forward target is clean, update provenance and clean reports through `metadata-sync`; the sync agent records that review found no repair or schema-derived Go changes.
- If target policy returns `block`, stop before drift generation.
- After local validation and final-manifest capture pass, report `protocol implementation complete` and stop before staging or publication.
- If generated Go fails because a new schema shape is unsupported, update focused generator rules and tests before regenerating.
- If a method disappears upstream, preserve compatibility only when safe and intentional; otherwise document the breaking change.
- If compare-only and full tracking disagree, trust full tracking and investigate candidate provenance before editing checked-in files.
