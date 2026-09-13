# Release

A release tags code already accepted on `main`. It does not introduce a second
API inventory or release-state ledger.

## Before dispatch

1. Merge the source change through required PR verification.
2. Update the owning module's `CHANGELOG.md` for user-visible changes.
3. Choose the next stable SemVer.
4. If the module depends on another repository module version, publish that
   dependency first.

Pre-v1 modules may evolve together in one source cohort even when a downstream
`go.mod` names the next upstream version before that upstream tag exists. That is
valid source composition, not published dependency closure.

Before a dependent module tag is created, every version in its committed
`go.mod` must already exist and resolve with `GOWORK=off` without replacement.

## Publish

Dispatch **Release public module** from `main` with:

- `module`: `llmkit`, `codexsdk`, or `codex-adapter`;
- `version`: a stable version such as `v0.13.0`.

The workflow verifies the selected `main` commit with the repository's native Go
checks, confirms remote `main` has not moved, refuses an existing version tag,
creates the module-prefixed immutable tag, and creates the GitHub Release.

Tag prefixes are independent:

```text
llmkit/vX.Y.Z
codexsdk/vX.Y.Z
llmcaller/codex/vX.Y.Z
```

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
