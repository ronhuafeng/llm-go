# Local Sync Reference

Use this file as context for local sync decisions. It is not a linear playbook. Commands and scripts own execution details; this reference preserves the domain rules that are easy to lose.

## Baseline And Provenance

Source of truth:

- checked-in schema baseline: `codexsdk/internal/protocolschema/appserver/v2`
- metadata: `baseline_metadata.json`
- manifest and coverage: `manifest.json`, `coverage_matrix.json`
- generated Go: `codexsdk/protocolv2/*.gen.go`, `codexsdk/sdk_surface.gen.go`

Record selected upstream provenance as:

- `source_ref_name`
- `source_ref_kind`: `stable_rust_tag`, `manual_ref`, or `manual_commit`
- peeled full `source_commit`

Do not check in local absolute paths, `.cache/...` output paths, private repo paths, account data, or raw smoke-test transcripts.

## Artifacts

Recommended disposable locations:

- upstream clone: `.cache/openai-codex`
- sync output: `.cache/codexsdk-upstream-<short-sha>`
- Rust build cache: `.cache/cargo-target/codex`

These are caller-chosen locations, not all script defaults. In normal generation mode, `scripts/codexsdk_track_upstream.sh` requires `--codex-repo` or `CODEXSDK_CODEX_REPO`; in `--compare-only` mode it needs only a resolved `--commit`, checked-in baseline, and candidate schema directory. When `--out` is omitted it creates a temporary `/tmp/codexsdk-upstream.*` directory.

Useful drift evidence:

- `reports/SUMMARY.md`
- `reports/drift_summary.json`
- `reports/matrix_update_skeleton.json`
- upstream `common.rs` response mappings from `source_commit:codex-rs/app-server-protocol/src/protocol/common.rs`

Preserve compact pre-change evidence before overwriting checked-in clean reports.

## Automation Phases

- Detect: resolve target, run policy, generate drift, record upstream ref/SHA, drift fingerprint, and workflow run URL, then render PR-ready drift analysis.
- Fix: when workflow-owned drift is `review-required` and the run is not `force_compare` verification, apply the generated candidate, run `repair-applied-candidate`, validate, commit the local sync, and publish a protected PR automatically.
- Finalize: after the PR lands, verify the landed commit, create the stable sync tag when applicable, and run forced drift verification when requested.

Do not depend on a `GITHUB_TOKEN` remote event to trigger the fix. The sync workflow should continue directly from drift evidence to protected PR publication when drift requires it.
Keep upstream target selection flexible, but keep workflow code refs, PR base refs, and finalize refs on the repository default branch unless a future explicit allowlist is added.

## Target Policy

Use the canonical resolver and target-policy script. Do not hand-roll tag sorting, prerelease filtering, annotated-tag peeling, or downgrade logic.

Prefer stable tags or named refs for normal syncs. Treat bare `manual_commit` SHA targets as explicit advanced inputs: the resolver accepts them syntactically, and tracking/fetch must fail closed if upstream cannot fetch the object.

Policy meanings:

- `allow`: drift generation may run
- `skip`: selected target is already represented; stop drift generation
- `block`: stop before drift generation

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

- `codexsdk/internal/protocolschema/appserver/v2/**` for schema JSON, baseline metadata, manifest, coverage, and checked-in clean drift reports
- `codexsdk/protocolv2/*.gen.go`
- `codexsdk/sdk_surface.gen.go`

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

After validation passes, use `commit-local-sync` to create the local sync commit before publication. `publish-protected-pr` consumes that committed `HEAD`; it must not create the commit itself.

When validation fails, inspect the first actionable failure before adding code or abstractions.

## Target Movement And Tags

For default scheduled syncs, compare against the latest stable `rust-vX.Y.Z` tag, not `main`.

Do not enter an unbounded loop chasing moving upstream refs. If the target moved, report the exact old/new target and whether remaining drift is real.

Tag only after a successful baseline sync commit exists on the landing ref:

- stable upstream tags use `upstream-codex-rust-vX.Y.Z`
- manual refs and manual commits do not get upstream sync tags
- never move or delete existing upstream sync tags
- follow-up syncs to the same upstream tag use the documented `-sync.N` suffix path, selected from remote tag state when pushing

## Decision Rules

- If drift is clean and the user only asked to check a target, report no SDK update is needed.
- If drift is clean but the user asked to move provenance, update provenance and clean reports without changing schema-derived Go output.
- If target policy returns `block`, stop before drift generation.
- If a fix PR was published, report `sync PR published` and stop before merge, tag, or drift verification.
- If a PR has landed, finalize from the landed commit rather than the PR branch head or an unmerged attempt.
- If generated Go fails because a new schema shape is unsupported, update focused generator rules and tests before regenerating.
- If a method disappears upstream, preserve compatibility only when safe and intentional; otherwise document the breaking change.
- If compare-only and full tracking disagree, trust full tracking and investigate candidate provenance before editing checked-in files.
