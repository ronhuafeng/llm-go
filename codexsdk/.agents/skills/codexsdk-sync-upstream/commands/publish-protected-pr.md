# Command: publish-protected-pr

State:
- Validated local sync needs publication and caller explicitly owns that remote side effect.
- `HEAD` is the committed sync change to publish, and the worktree and index are clean before invoking the publish script.

Inputs:
- Landing ref, sync branch prefix, target ref, target kind, target SHA, candidate schema path, and explicit publish request.

Tool:
- `scripts/codexsdk_publish_sync_pr.sh`

Boundaries:
- May create/update a sync branch and PR through the publish script.
- Must stop at PR publication unless the caller explicitly selected a separate merge/finalize command.
- Accept only the repository default branch as the PR base unless an explicit future allowlist exists.
- Must not push directly to protected `main`, introduce bypass tokens/statuses, tag, claim final sync completion, delete branches, or merge around failed checks.
- Keep `action_required` as expected maintainer rerun/approval gate for `GITHUB_TOKEN` PRs.
- Include compact sync metadata in the PR body when available so finalize can verify the landed commit and target.

Checks:
- Published branch is not protected `main`.
- PR targets the landing ref and uses real required checks.
- No synthetic statuses or bypass tokens were introduced.

Output:
- Sync branch, PR URL/number, pushed commit when known, drift fingerprint metadata when provided, `action_required` status when relevant, and `sync PR published` completion layer.

Stop if:
- Publishing was not explicitly requested.
- Local validation failed or is unknown.
- Branch protection would be bypassed.
