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
- May dispatch follow-up workflow state only when the caller explicitly owns that side effect.
- Must not apply a candidate, mutate checked-in sync files, commit, push, tag, create PRs, or publish remote state unless the caller explicitly owns those side effects.
- Must not rely on a `GITHUB_TOKEN` remote event as the control plane for fixes.
- When dispatching a follow-up workflow, use the trusted default-branch workflow ref rather than untrusted external metadata.

Checks:
- Policy JSON parses and has decision plus reason.
- On `allow`, compact reports include `SUMMARY.md`, `drift_summary.json`, and `matrix_update_skeleton.json`.
- Artifact evidence records upstream ref, upstream SHA, drift fingerprint, and run URL when caller-owned automation updates remote state.
- Checked-in baseline files remain unchanged.

Output:
- Policy decision, reason, drift status, drift fingerprint, target provenance, artifact directory, run URL, and any caller-owned dispatch action.

Stop if:
- Policy returns `block` or `skip`; stop drift generation in both cases.
- Candidate provenance is missing or drift generation fails.
