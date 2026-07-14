# Recovery Reference

Use recovery recipes before adding automation. Keep recovery on the protected PR path. Never weaken branch protection, bypass required checks, synthesize statuses, force-push `main`, or move/delete tags.

## Sync PR `Go` Check Is `action_required`

Trigger:
- A sync PR created by `GITHUB_TOKEN` has a required `Go` run in `action_required`.

Evidence:
- First run has no jobs or shows GitHub's maintainer rerun/approval gate.

Allowed action:
- A maintainer with write access may approve or rerun the check once.

Stop:
- Do not manually merge around this gate. After the real `Go` check passes, protected auto-merge should continue.

## Sync PR Does Not Merge After Publication

Trigger:
- A published sync PR remains open or repository auto-merge does not land it.

Evidence:
- PR state, merge state, review decision, status rollup, required checks, and recent CI runs on the sync branch.

Allowed action:
- If the blocker is `action_required`, use that recipe.
- If a real check failed, inspect logs and fix with a normal follow-up commit or rerun caller-owned automation.
- If checks passed but auto-merge is not enabled, leave the PR on the protected path for a maintainer or repository auto-merge policy to handle.

Stop:
- Do not force a merge around failed required checks.

## Finalize Failed After PR Merged

Trigger:
- Sync PR merged, but tag creation or drift dispatch failed.

Evidence:
- Landed `main` commit, intended upstream target, existing sync tags, and finalize logs.

Allowed action:
- Recover from the exact landed commit.
- Create a stable sync tag through `scripts/codexsdk_sync_tag.py`; when pushing, let the script choose suffixes from remote tag state.
- If the base tag already exists at another commit, use the documented suffix path.
- Run caller-owned drift verification after tagging when required.

Stop:
- Do not tag unmerged PR heads, failed attempts, or manual upstream refs/commits.

## Drift Verification Fails After Main CI Passes

Trigger:
- Main CI passed but dispatched drift verification failed or still reports drift.

Evidence:
- Landed `baseline_metadata.json`, target provenance, and drift run logs.

Allowed action:
- Rerun if transient.
- If drift is real, start another sync from fresh drift evidence.

Stop:
- Do not report `landed sync finalized` until drift verification is clean when requested.

## Sync Tag Already Exists

Trigger:
- Base tag `upstream-codex-rust-vX.Y.Z` already exists.

Evidence:
- Existing tag target and current landed commit.

Allowed action:
- Use `scripts/codexsdk_sync_tag.py --next-suffix --create --push origin` for follow-up syncs to the same upstream tag.
- The script must consider remote tag state before selecting the base tag or the next `-sync.N` suffix.

Stop:
- Never move or delete an existing upstream sync tag.
