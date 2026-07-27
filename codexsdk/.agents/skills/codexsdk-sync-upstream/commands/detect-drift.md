# Command: detect-drift

State:
- Caller has a resolved target and needs policy plus local drift artifacts.

Inputs:
- Target ref, ref kind, target SHA, target explicit/default status, policy mode, and downgrade policy.
- Generation mode: upstream Codex repo path and output directory.
- Compare-only mode: candidate schema directory, checked-in baseline, resolved target SHA, and output directory; no upstream repo path is required.

Tools:
- `scripts/codexsdk_target_policy.py`
- `scripts/codexsdk_track_upstream.sh`

Boundaries:
- Run policy before drift generation.
- May write policy output and local drift artifacts.
- May let tracking fetch the selected target narrowly after policy allows.
- Must not apply a candidate or mutate checked-in sync files.
- Must not configure authentication, stage, commit, push, tag, create PRs, dispatch workflows, or publish remote state.

Checks:
- Policy JSON parses and has decision plus reason.
- On `allow`, compact reports include `SUMMARY.md`, `drift_summary.json`, and `matrix_update_skeleton.json`.
- Artifact evidence records upstream ref, upstream SHA, and drift fingerprint.
- Checked-in baseline files remain unchanged.

Output:
- Policy decision, reason, drift status, drift fingerprint, target provenance, and artifact directory.

Stop if:
- Policy returns `block`; stop drift generation, and fail a caller-owned `force_compare` verification.
- Policy returns `skip` and the caller did not request `force_compare`; forced comparison may continue read-only drift generation.
- Candidate provenance is missing or drift generation fails.
