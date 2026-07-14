# Sync Workflow Automation Reference

Use this reference for the GitHub Actions sync/finalize workflows, protected PR path, auto-merge behavior, drift verification, and remote completion layers.

## Contents

- [Workflow Contract](#workflow-contract)
- [Sync Workflow](#sync-workflow)
- [Finalize Job](#finalize-job)
- [Remote Completion Layers](#remote-completion-layers)
- [Helper Boundaries](#helper-boundaries)

## Workflow Contract

Routine drift handling is split across a sync workflow and a finalize workflow. Scheduled or manual sync detects drift, renders PR-ready analysis, and publishes a protected sync PR automatically when drift is `review-required` and the run is not `force_compare` verification. Finalize runs only after the protected PR has landed; the PR-closed trigger is the fast path, with schedule and manual dispatch as required recovery paths.

The high-level job structure must stay recognizable:

1. `sync`: resolve target, run policy, generate drift, upload candidate evidence, apply mechanical sync when needed, run a bounded Codex repair pass, validate, commit, and publish a protected sync PR with drift analysis and fix description.
2. protected branch auto-merge: GitHub merges only after required real checks pass.
3. `finalize`: after a sync PR merge event or recovery scan, verify the landed commit, tag landed stable syncs when applicable, and dispatch drift verification when requested.

Invariants:

- landing ref is the repository default branch
- no direct push to protected `main`
- no PAT, GitHub App token, or bot-token bypass
- no synthetic required status writing
- no tag deletion or tag movement
- auto-merge continues through real PR `Go` checks
- merge queue, if enabled, may enqueue instead of immediate merge

Sync PRs created by `GITHUB_TOKEN` may require one maintainer approval or rerun before GitHub schedules the required `Go` pull request check. This `action_required` state is an expected GitHub Actions safety gate, not a sync failure.

## Sync Workflow

The detect phase must:

- resolve landing ref as the repository default branch
- check out the landing ref with full history
- resolve upstream target through `scripts/codexsdk_resolve_upstream.py`
- evaluate target policy through `scripts/codexsdk_target_policy.py`
- stop cleanly on policy `block`
- stop drift generation on policy `skip`
- clone upstream only after policy `allow`
- generate drift with `scripts/codexsdk_track_upstream.sh`
- capture upstream `common.rs` response mappings for the fix phase
- render drift PR analysis as a deterministic artifact
- upload the full candidate artifact directory as pre-change evidence

The fix phase must run only when drift status is `review-required` and the run is not `force_compare` verification. Scheduled drift may publish a protected sync PR automatically; it must still stop at PR publication.

The fix phase must:

- set up Go and Codex Rust
- configure a bot git identity
- apply the candidate mechanically with `scripts/codexsdk_apply_sync_candidate.py`
- build a bounded Codex repair prompt
- run `openai/codex-action@v1` against the current worktree
- validate with `scripts/codexsdk_validate_sync.sh`
- commit the validated local sync through the `commit-local-sync` boundary only when there are real changes
- publish a sync PR with `scripts/codexsdk_publish_sync_pr.sh`, including drift analysis and the fix description in the PR body
- stop at PR publication unless a separate caller-owned merge/finalize command was selected

The Codex repair prompt must keep judgment bounded:

- read the sync skill before editing
- inspect apply summary, compact drift reports, separate git diff/status evidence, `common.rs` provenance, and the required drift review checklist
- do not re-copy schemas or rerun the full Rust schema generator
- do not print large schema, report, manifest, coverage, or generated Go files
- make only small, concrete fixes found by review
- leave repository side effects to the workflow
- keep handwritten SDK changes minimal and tied to reviewed drift

Post-publication diagnostics must keep merge decisions on the protected PR path:

- PR state, merge state, review decision, and status rollup
- `gh pr checks`
- recent `ci.yml` runs for the sync branch
- reminder that an empty `action_required` first run needs one maintainer rerun

## Finalize Workflow

Finalize must run only after the sync PR has landed. A `pull_request.closed` event for a merged default-branch PR is the fast path when GitHub emits it; scheduled no-input runs scan recent merged PRs as a fallback; manual dispatch remains the explicit recovery path.

The job must:

- check out the landed ref and exact landed commit
- configure git identity
- create an upstream sync tag only for `stable_rust_tag`
- if the base tag already exists at another commit, use the suffix path through `scripts/codexsdk_sync_tag.py --next-suffix`
- when pushing, let `scripts/codexsdk_sync_tag.py` choose the base tag or next `-sync.N` suffix from remote tag state
- dispatch `upstream-protocol-sync.yml` with `force_compare=true` when drift verification is requested
- watch the dispatched drift workflow to completion
- report landing ref, landed commit, sync PR, upstream target, upstream commit, and sync tag when present

Manual refs and manual commits do not get upstream sync tags.

## Remote Completion Layers

Report remote completion precisely:

- `sync PR published`: sync PR branch exists, but protected merge, tag handling, CI, or drift verification is pending
- `landed sync finalized`: sync PR merged, stable-tag sync tag handling completed when applicable, pushed CI passed, and requested drift verification passed

After a sync PR merges into `main`, the PR-closed finalize trigger dispatches forced drift verification rather than relying on ordinary pushes. If the event path is unavailable or suppressed, the scheduled finalize fallback should select the current default-branch sync PR. To verify a selected upstream ref manually, dispatch drift verification and watch the run:

```sh
gh workflow run upstream-protocol-sync.yml --ref main -f upstream_ref=<target_ref> -f force_compare=true
gh run watch <run-id> --exit-status
```

Cold GitHub runners may spend several minutes compiling Rust before the drift report step advances.

Confirm outcome:

- if drift is clean, finalize may report `landed sync finalized`
- if drift remains, publish another protected sync PR from fresh drift evidence
- if the selected upstream tag/ref moved or a newer stable tag exists, report the exact tag/ref and commit and whether remaining drift is real or clean

## Helper Boundaries

Workflow helper scripts may perform deterministic formatting, rendering, validation, polling, or writing. They must not decide analysis strategy, model, prompt policy, task split, repair logic, branch protection strategy, or sync target policy.

Good helpers in this workflow include:

- rendering the Codex prompt file from explicit inputs
- rendering drift PR analysis artifacts from `drift_summary.json`
- publishing the protected sync PR and printing post-publication diagnostics
- validating generated outputs and sync state

Do not collapse deterministic helpers into a monolithic shell blob; keep target resolution, drift rendering, candidate application, validation, PR publication, and tag handling in canonical scripts.
