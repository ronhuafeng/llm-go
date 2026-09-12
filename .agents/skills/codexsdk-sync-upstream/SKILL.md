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
- focused checks and `go run ./internal/cmd/generatedproof` pass for the same target SHA;
- the final tracked and untracked change manifest is captured and contains only reviewed `codexsdk/` implementation files;
- the worktree changes remain unstaged and uncommitted.

Start successful local reports with exactly one of these machine-readable first lines:

- `protocol implementation complete` after an applied implementation passes full validation and final-manifest capture;
- `protocol implementation current` when policy skips because the selected target is already the checked-in baseline;
- `protocol comparison clean` when `force_compare` completes read-only and finds no drift.

Do not include any of these exact lowercase lines in an incomplete, blocked, or drift-found final response.
GitHub Actions does not use these lines as a publication gate.

For a read-only comparison, report its target provenance and drift result without claiming implementation completion.

## Sync Protocol

When `GITHUB_ACTIONS=true`, use only this protocol. Do not load `references/github-operations.md`.

The workflow owns mechanical generation. An implementation agent is invoked only when `.cache/codexsdk-sync/escalation.json` exists after that path failed with a deterministic reason.

Set the module root to `$GITHUB_WORKSPACE/codexsdk` and use it as the working directory for every Action shell command. Read `$GITHUB_WORKSPACE/codexsdk/.cache/codexsdk-sync/escalation.json` and `$GITHUB_WORKSPACE/codexsdk/.cache/codexsdk-sync/action-inputs.json`. Do not search for, infer, or replace missing Action inputs or the selected upstream ref/commit.

1. Confirm `escalation.json` names the same `target_ref`, `target_kind`, and `target_sha` as `action-inputs.json`.
2. Use the recorded reason, detail, and artifact paths. Do not rerun target resolution, detect-drift, or mechanical apply from scratch.
3. Use `repair-applied-candidate` or `recover-failure` only to resolve the recorded unsupported semantic drift.
4. Use `validate-local` against the same target SHA.
5. Leave changes unstaged. The workflow captures, validates again, and publishes.

If `escalation.json` is absent, stop. The mechanical path already finished without an implementation agent.

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

For an Action escalation, report the selected upstream ref/commit, the recorded reason, files changed, and validation results. Do not perform caller-owned publication work. The workflow owns publication and no longer gates on a completion first line.
