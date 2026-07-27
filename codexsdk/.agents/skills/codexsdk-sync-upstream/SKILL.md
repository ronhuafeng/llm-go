---
name: codexsdk-sync-upstream
description: Implement the codexsdk module's checked-in Codex app-server protocol at a selected upstream openai/codex tag, ref, or commit. Use for protocol drift detection, baseline metadata/report refresh, protocolv2 regeneration, handwritten compatibility work, and local validation.
---

# Codex SDK Upstream Sync

## Contract

Implement the checked-in app-server protocol completely at the selected upstream version. The checked-in schema baseline remains the source of truth for generated Go; normal builds must not follow a local `codex` binary implicitly.

End with a validated local worktree. Leave GitHub publication and landed-finalization operations to the caller.

All command and source paths are relative to the `codexsdk` module root. In the monorepo checkout, enter `codexsdk/` before running them.

## Completion

Report `protocol implementation complete` only when:

- baseline metadata and schemas identify the selected upstream ref, kind, and commit;
- generated Go and any necessary handwritten compatibility implementation match that baseline;
- focused checks and `scripts/codexsdk_validate_sync.sh` pass for the same target SHA;
- the final tracked and untracked change manifest is captured and contains only reviewed `codexsdk/` implementation files;
- the worktree changes remain unstaged and uncommitted.

Start successful Action reports with exactly one of these machine-readable first lines:

- `protocol implementation complete` after an applied implementation passes full validation and final-manifest capture;
- `protocol implementation current` when policy skips because the selected target is already the checked-in baseline;
- `protocol comparison clean` when `force_compare` completes read-only and finds no drift.

Do not include any of these exact lowercase lines in an incomplete, blocked, or drift-found final response.

For a read-only comparison, report its target provenance and drift result without claiming implementation completion.

## Sync Protocol

When `GITHUB_ACTIONS=true`, use only this protocol. Do not load `references/github-operations.md`.

Read `.cache/codexsdk-sync/action-inputs.json`, the workflow-owned input document containing `target_ref`, `target_kind`, `target_sha`, `target_explicit`, `allow_downgrade`, `force_compare`, and `upstream_repo`. Do not infer or replace missing Action inputs. First confirm that the automation worktree has no tracked or unignored untracked changes, then route as follows:

1. Verify that the resolved target fields are complete and use them unchanged throughout the attempt.
2. Use `detect-drift` to apply target policy and generate candidate evidence.
3. On policy `block`, stop with the policy reason.
4. On `skip` without `force_compare`, stop without changing implementation files.
5. With `force_compare`, finish read-only drift generation and stop. Clean drift passes comparison; remaining drift fails comparison. Never apply or repair in this branch.
6. On `allow` without `force_compare`, use `apply-candidate`, review and complete the implementation through `repair-applied-candidate`, then use `validate-local`.
7. If candidate apply or validation exposes a supported local compatibility defect, use `recover-failure`, then return to apply, repair, or validation as directed. Continue while each iteration fixes a newly evidenced failure and the focused check advances; stop on an external blocker or when the same failure repeats without progress.
8. Capture the final change manifest with `scripts/codexsdk_sync_changes.py capture --repo-root "$GITHUB_WORKSPACE" --phase final --output .cache/codexsdk-sync/final-changes.json`, verify the changes remain unstaged, and stop at `protocol implementation complete`.

Every applied candidate receives agent review. Clean drift requires an explicit no-repair confirmation; review-required drift receives the smallest evidence-backed compatibility implementation.

## Safety Boundaries

- Modify only the local protocol implementation and its focused tests or documentation when justified by reviewed drift.
- Preserve unrelated user changes.
- Keep checked-in metadata and reports free of local absolute paths, cache paths, private repo paths, account data, and raw transcripts.
- Leave all changes unstaged and uncommitted.
- Do not configure GitHub authentication or Git identity.
- Do not stage, commit, push, create or edit PRs, merge, tag, dispatch workflows, or otherwise mutate remote state.

## Command Index

Commands live under [commands/](commands/). Load only the command selected by current local state.

- [resolve-target](commands/resolve-target.md): resolve an upstream target.
- [detect-drift](commands/detect-drift.md): run target policy and create local drift artifacts.
- [apply-candidate](commands/apply-candidate.md): mechanically apply reviewed drift artifacts.
- [repair-applied-candidate](commands/repair-applied-candidate.md): complete or confirm an already-applied implementation.
- [validate-local](commands/validate-local.md): validate the local protocol implementation.
- [recover-failure](commands/recover-failure.md): recover one candidate apply or local validation failure.

References are loaded only for their named branch:

- [references/local-sync.md](references/local-sync.md): local synchronization context and implementation decision rules.
- [references/github-operations.md](references/github-operations.md): user-requested GitHub operations outside GitHub Actions; never load it for the Action protocol.

## Input Policy

Collect only inputs required by the selected local command. If a target cannot be inferred from the request, latest stable tag, or Action context, ask before changing files.

## After Run

Start a successful Action final report with the exact line for its applied, current, or clean-comparison state, then report target provenance, files changed, and validation commands and results. For incomplete, blocked, or drift-found work, report blockers and the highest local completion state without a successful state line. Do not perform caller-owned publication work.
