# User GitHub Operations Guide

Use this guide only when a user explicitly requests GitHub publication, recovery, tagging, or finalization outside GitHub Actions. The Action protocol never loads this guide.

Select [commit-local-sync](../commands/commit-local-sync.md), [publish-protected-pr](../commands/publish-protected-pr.md), or [finalize-landed-sync](../commands/finalize-landed-sync.md) only when the user's request explicitly owns that operation.

## Preflight

Before a user-requested remote side effect:

1. Verify `gh auth status` and the intended repository.
2. Run `gh auth setup-git` only when an authenticated Git remote write is required.
3. Verify the current Git identity. Preserve the user's identity unless they explicitly request another one.
4. Verify the target branch, commit, PR, or tag with read-only commands before mutation.
5. Stop if the operation would bypass branch protection, move an immutable tag, or overwrite unrelated remote state.

## Protected Publication

- Publish protocol changes through a non-protected branch and a PR targeting the repository default branch.
- Leave required checks and merge decisions to branch protection, an authorized maintainer, merge queue, or configured repository auto-merge.
- Treat `action_required` on a `GITHUB_TOKEN` PR as a maintainer rerun gate, not permission to bypass checks.

## Landed Finalization

- Finalize only a verified default-branch commit whose checked-in baseline provenance matches the intended upstream target.
- Create stable sync tags only through the canonical tag helper.
- Keep existing tags immutable. Stop when the base sync tag already points at a different commit.
- Leave manual upstream refs and commits untagged.
- Run same-target drift verification when the user requests it before reporting finalization.

Use [recovery.md](recovery.md) for documented remote failure states.
