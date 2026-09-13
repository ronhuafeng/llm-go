# Release

A release tags code already accepted on `main`. It does not introduce a second
API inventory or release-state ledger.

## Before dispatch

1. Merge the source change through required PR verification.
2. Update [`../CHANGELOG.md`](../CHANGELOG.md) for user-visible changes.
3. Choose the next stable SemVer for `github.com/ronhuafeng/llm-go`.

## Publish

Dispatch **Release** from `main` with:

```text
version=vX.Y.Z
```

The workflow verifies the selected `main` commit with the repository's native Go
checks, confirms remote `main` has not moved, refuses an existing version tag,
creates the immutable tag `vX.Y.Z`, and creates the GitHub Release.

If `main` moves while approval or verification is running, dispatch again from
the new head. Never move or reuse a formal tag. If tag creation succeeds but the
GitHub Release step fails, create the release for the existing tag rather than
recreating the tag.

## After publication

A post-release module-resolution smoke checks the external consumer path. It is
an observation after publication, not a prerequisite for or redefinition of the
immutable tag. It may be rerun manually if public proxy propagation is delayed.

Live Codex smoke is likewise separate from release state; see
[`verify.md`](verify.md).
