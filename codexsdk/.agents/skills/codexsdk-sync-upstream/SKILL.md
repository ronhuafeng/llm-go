---
name: codexsdk-sync-upstream
description: Sync codexsdk-go's checked-in Codex app-server protocol baseline to a selected upstream openai/codex tag, ref, or commit. Use for protocol drift detection, protected sync PR publication, baseline metadata/report refresh, protocolv2 regeneration, validation, upstream sync tagging, and finalize verification.
---

# Codex SDK Upstream Sync

## Contract

The checked-in app-server schema baseline is the source of truth for generated Go. Do not make the SDK implicitly follow a local `codex` binary during normal builds.

This skill is context and routing, not orchestration. It gives Codex the domain contract, safety boundaries, command index, and completion language. Inside a selected command, Codex may choose the shortest safe path from current evidence.

Use canonical scripts for deterministic work. Use Codex judgment for classification, repair decisions, focused validation choices, and recovery routing.

All command and source paths in this skill are relative to the `codexsdk`
module root. In the monorepo checkout, enter `codexsdk/` before running them.

## Completion Layers

Report the highest completed layer precisely:

- `local sync complete`: files validate locally and any requested local sync commit is complete, but nothing was pushed
- `sync PR published`: the sync commit was pushed to a PR branch, but protected merge, tag handling, and drift verification are still pending
- `landed sync finalized`: the landed commit was verified, tags were handled when applicable, and drift verification passed when requested

Never call a sync finalized at PR publication time.

## Automation Phases

- Detect resolves the upstream target, runs policy, generates drift evidence, and writes PR-ready drift analysis artifacts.
- Fix runs in the scheduled/manual sync workflow for an allowed forward target when the run is not `force_compare` verification. Clean drift uses `metadata-sync` to advance provenance without a Codex repair pass; `review-required` drift uses `repair-sync` to apply the candidate and run `repair-applied-candidate`. Both paths validate, commit the bounded local sync, and publish a protected PR. The PR body records the sync mode, drift analysis, fix description, and compact sync metadata.
- Finalize runs only after the PR landed. The PR-closed trigger is the fast path after a sync PR merges; schedule and manual dispatch are required recovery paths. It verifies the landed commit, creates sync tags when applicable, and dispatches drift verification when requested.

Sync PR metadata records the upstream target, drift fingerprint, sync commit, and base branch needed by finalize. It must not select workflow code refs, landing refs, or finalize refs outside the repository default branch.

## Safety Boundaries

- Do not push directly to protected `main`.
- Do not introduce a PAT, GitHub App token, bot-token bypass, or synthetic required status.
- Do not delete or move upstream sync tags.
- Do not weaken branch protection or merge around failed required checks.
- Keep `action_required` documented as an expected maintainer rerun gate for sync PRs created by `GITHUB_TOKEN`.
- Keep merge decisions on the real protected-branch PR path after the required `PR verification` check passes.
- Keep checked-in baseline metadata and checked-in reports free of local absolute paths, `.cache` output paths, private repo paths, account data, and raw smoke-test transcripts.
- Preserve unrelated user changes.
- Keep merge decisions on the protected PR path. Branch protection, the real required `PR verification` check, and an authorized maintainer decide whether a sync PR lands; repository auto-merge may be enabled separately but is not assumed.
- Keep workflow control-plane refs and remote landing/finalize refs constrained to the repository default branch unless a future explicit allowlist is added.

## Command Index

Commands live under [commands/](commands/). A caller may invoke any single command directly when its state and inputs match. The command-specific boundary wins over any example or prior conversation.

- [resolve-target](commands/resolve-target.md): resolve an upstream target; caller or target policy owns baseline provenance.
- [detect-drift](commands/detect-drift.md): run target policy and create local drift artifacts.
- [apply-candidate](commands/apply-candidate.md): mechanically apply reviewed drift artifacts.
- [repair-applied-candidate](commands/repair-applied-candidate.md): repair or confirm an already-applied candidate.
- [validate-local](commands/validate-local.md): validate local sync state.
- [commit-local-sync](commands/commit-local-sync.md): commit a validated local sync change without publishing.
- [publish-protected-pr](commands/publish-protected-pr.md): publish through the protected PR path when explicitly owned by the caller; stop at PR publication.
- [finalize-landed-sync](commands/finalize-landed-sync.md): verify, tag, and drift-check a landed sync when explicitly owned by the caller.
- [recover-failure](commands/recover-failure.md): recover failed checks, open sync PRs, finalize failures, drift verification failures, or tag conflicts.

References are optional context, not required linear playbooks:

- [references/local-sync.md](references/local-sync.md): local sync context and decision rules.
- [references/recovery.md](references/recovery.md): recovery recipes for known remote failure states.

## Input Policy

Collect only the inputs needed by the selected command. Do not chase upstream repo paths, generator mode, output workdirs, or target provenance in commands that already receive authoritative artifacts.

If a command needs an upstream target and it cannot be inferred from the request, latest stable tag, or local context, ask before changing files.

## After Run

If execution reveals a concrete improvement to this skill, a helper script, or an environment assumption, mention it briefly and ask whether to update the skill. Do not modify the skill during the original command unless the user explicitly asks.
