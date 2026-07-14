You are maintaining codexsdk-go.

Task: Use codexsdk-sync-upstream command: repair-applied-candidate.

Current phase: `${PHASE}`.

Read and follow:

- .agents/skills/codexsdk-sync-upstream/SKILL.md
- .agents/skills/codexsdk-sync-upstream/commands/repair-applied-candidate.md

Current state: Detect and apply have already completed. The workflow resolved `${UPSTREAM_REF}` (`${UPSTREAM_REF_KIND}`) to `${UPSTREAM_SHA}`, generated the candidate, and applied it to `${LAND_REF}` with `scripts/codexsdk_apply_sync_candidate.py`.

Authoritative inputs:

- ${CANDIDATE_DIR}/schema
- ${CANDIDATE_DIR}/reports
- ${CANDIDATE_DIR}/common.rs
- ${CANDIDATE_DIR}/common.rs.source_sha
- ${AUTO_SYNC_DIR}/apply-result.json
- ${AUTO_SYNC_DIR}/diff-name-status.txt

Allowed side effects:

- Edit only the local checkout surfaces permitted by `repair-applied-candidate`.
- When generated type/schema changes invalidate handwritten schema-derived fixtures, update the minimal fixture or test expectation instead of preserving removed upstream fields.
- Run focused checks that are useful for files you touch.
- Report blockers when candidate artifacts or provenance are inconsistent.

Forbidden side effects:

- Do not run `resolve-target`, `detect-drift`, `scripts/codexsdk_track_upstream.sh`, full Rust schema generation, or `apply-candidate`.
- Do not re-copy schemas from upstream.
- Do not commit, push, tag, create PRs, change branches, request merges, or publish remote state.

Do not follow a global sync workflow; stay inside the repair command boundary and use the shortest safe path for the evidence in this checkout.

After you finish, the sync workflow owns full validation, commit creation, and protected PR publication. Branch protection, the required `Go` check, repository auto-merge rules, and the finalize workflow own merge, tags, and drift verification.

Final output must include: completed_actions, files_changed, validation, blockers, highest local completion layer.
