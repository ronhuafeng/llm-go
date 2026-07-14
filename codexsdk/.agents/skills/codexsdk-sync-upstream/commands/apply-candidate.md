# Command: apply-candidate

State:
- Reviewed candidate is ready for mechanical application into the checked-in baseline.

Inputs:
- Candidate schema directory, reports directory, upstream `common.rs`, `common.rs` source SHA, target ref, ref kind, and target SHA.

Tool:
- `scripts/codexsdk_apply_sync_candidate.py`

Boundaries:
- May update checked-in schemas, metadata, clean drift reports, manifest, coverage, generated `protocolv2` Go, and SDK surface through the apply script.
- May write a local apply summary from the apply script and separate diff name-status evidence from `git diff --name-status` or `git status --short`.
- Must not hand-copy schemas or reports.
- Must not make judgment calls beyond provenance/input checks.
- Must not commit, push, tag, create PRs, request merges, or change branches.

Checks:
- Apply result JSON parses.
- `common.rs` source SHA matches the target SHA, and content is verified from `target_sha:codex-rs/app-server-protocol/src/protocol/common.rs` when an upstream clone is available.
- Changed files from separate git diff/status evidence stay inside the allowed sync surface.
- Mechanical sync surface is limited to `codexsdk/internal/protocolschema/appserver/v2/**`, `codexsdk/protocolv2/*.gen.go`, and `codexsdk/sdk_surface.gen.go`; handwritten SDK, tests, or docs require reviewed drift evidence or explicit user authorization.
- Candidate provenance still matches target.

Output:
- Apply summary path or key findings, separate diff name-status path or summary, and whether follow-up repair is needed.

Stop if:
- Candidate artifacts or provenance are missing/mismatched.
- `common.rs` provenance is missing, mismatched, or cannot be verified when verification inputs are available.
- Apply script fails or wants to touch files outside the allowed sync surface.
