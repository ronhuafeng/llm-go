# Command: finalize-landed-sync

State:
- Sync PR landed and caller-owned remote completion may need landed-commit verification, tagging, or drift verification.

Inputs:
- Landed ref, landed commit, sync PR, upstream target ref/kind/SHA, optional drift fingerprint metadata, and whether caller requested drift verification.

Tools:
- `scripts/codexsdk_sync_tag.py`
- Caller-owned workflow dispatch or CI tooling for drift verification.

Boundaries:
- Verify the landed commit is the commit being finalized before tagging or dispatching verification.
- Accept only the repository default branch as the landing/finalize ref unless an explicit future allowlist exists.
- May create stable upstream sync tags only through the sync tag script, which must choose suffixes from remote tag state when pushing.
- May run drift verification only when explicitly requested.
- Must not delete/move tags, tag unmerged PR heads or failed attempts, merge PRs, or report final completion before CI, tag handling, and requested drift verification are complete.

Checks:
- Minimum finalization evidence chain is complete: PR is merged, landed commit is on the repository default branch, landed baseline metadata `source_ref_name`/`source_ref_kind`/`source_commit` matches the intended target, tag handling ran only from the landed commit when applicable, and forced drift verification used the same target when requested.
- Tag, if created, points at the landed commit.
- Drift verification result is known when dispatched.
- Existing tags were not moved or deleted.

Output:
- Landed ref, landed commit, sync PR, upstream target, upstream commit, sync tag when present, drift verification result, and completion layer.

Stop if:
- PR has not landed or landed commit cannot be verified.
- Landed baseline metadata does not match the intended target.
- Drift verification was not run against the same target when requested.
- Target is manual ref/commit and tagging would be attempted.
- Tag conflict cannot use the documented suffix path.
