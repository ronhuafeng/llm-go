---
name: codexsdk-sync-upstream
description: Make the minimal codexsdk source and test changes required by an already-observed Codex App Server protocol drift. The workflow owns target resolution, mechanical generation, verification, and publication.
---

# Codex SDK Upstream Sync

Use this skill only for implementation work after a Codex App Server protocol
change has been selected and compared.

## Contract

The caller/workflow owns:

- invoking the Go-native protocol-sync owner for the selected upstream target,
  candidate generation, mechanical updates, and deterministic verification;
- commits, pull requests, merge, tags, and release effects.

You own one targeted implementation pass on the current `codexsdk` worktree.

1. Read the drift/candidate/test information supplied by the caller.
2. Inspect the current worktree and affected `codexsdk` code/tests.
3. Make only changes justified by the observed protocol drift.
4. Add or update focused tests when handwritten behavior changes.
5. Use the repository's canonical generators/checkers; do not hand-edit generated
   files when a generator owns them.
6. Run focused local Go checks when useful, but do not certify success. The
   workflow runs the final deterministic acceptance set.
7. Leave changes unstaged and uncommitted.

If the supplied target or drift information is insufficient, stop rather than
resolving a different target or inventing missing facts.

## Boundaries

- Work only in `codexsdk` and directly related tests/docs justified by the drift.
- Do not configure Git/GitHub identity or authentication.
- Do not stage, commit, push, create/edit/merge PRs, dispatch workflows, or tag.
- Do not change unrelated runtime behavior.
- Do not treat a final model message as proof that the implementation is correct.

Protocol workflow and acceptance rules are documented in
[`../../../docs/protocol-sync.md`](../../../docs/protocol-sync.md).
