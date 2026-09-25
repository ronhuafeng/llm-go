---
name: codexsdk-sync-upstream
description: Make the minimal codexsdk handwritten source and test changes required when read-only planning finds an unresolved Codex App Server protocol incompatibility. The Go protocol-sync owner already selected and generated the exact candidate; the workflow re-plans and applies it after this one pass.
---

# Codex SDK Upstream Sync

Use this skill only after the Go protocol-sync owner has selected an exact
Codex App Server target, generated its candidate, and returned a concrete
`semantic_unresolved` planning result.

## Contract

The caller/workflow owns:

- exact upstream target selection and candidate generation;
- the first read-only plan;
- re-planning the same candidate after this pass;
- deterministic application only after the re-plan succeeds;
- final deterministic verification and publication authorization.

You own one targeted handwritten implementation pass on the current `codexsdk`
worktree.

1. Read the supplied target, candidate, stage/path, and failure reason.
2. Inspect only the affected `codexsdk` generator/semantic source and focused tests.
3. Change the smallest handwritten owner that makes the supplied candidate
   mechanically representable without weakening wire meaning.
4. Add or update focused tests for the changed rule.
5. Do not hand-edit generated protocol/schema outputs; the workflow applies them
   only after the second plan succeeds.
6. Run focused local Go checks when useful, but do not certify success. The
   workflow owns re-plan, apply, and final deterministic acceptance.
7. Leave changes unstaged and uncommitted.

If the evidence is insufficient, or the required change would alter product
intent rather than generator/semantic representation, stop rather than selecting
another target or inventing missing facts.

## Boundaries

- Work only in `codexsdk` and directly related tests/docs justified by the drift.
- Do not configure Git/GitHub identity or authentication.
- Do not stage, commit, push, create/edit/merge PRs, dispatch workflows, or tag.
- Do not change unrelated runtime behavior.
- Do not treat a final model message as proof that the implementation is correct.

Protocol workflow and acceptance rules are documented in
[`../../../docs/protocol-sync.md`](../../../docs/protocol-sync.md).
