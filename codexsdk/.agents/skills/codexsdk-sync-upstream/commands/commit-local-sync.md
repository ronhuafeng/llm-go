# Command: commit-local-sync

State:
- Applied and repaired sync changes passed `validate-local` for the same target and need a local commit before publication.

Inputs:
- Target ref, ref kind, target SHA, validation evidence, diff/status evidence, and intended commit message metadata.

Tool:
- `git add` and `git commit` on the current branch, scoped by the reviewed local sync diff.

Boundaries:
- May stage and commit only reviewed local sync changes after validation has passed for the same target SHA.
- Must preserve unrelated user changes and must not stage unrelated files.
- Must not push, tag, create PRs, request merges, change branches, or publish remote state.
- Must not claim `sync PR published` or `landed sync finalized`.

Checks:
- Validation evidence names `scripts/codexsdk_validate_sync.sh` or equivalent focused checks and matches the target SHA.
- Diff/status evidence was reviewed after repair and before staging.
- Mechanical sync files are limited to `codexsdk/internal/protocolschema/appserver/v2/**`, `codexsdk/protocolv2/*.gen.go`, and `codexsdk/sdk_surface.gen.go`; any handwritten SDK, test, or doc file in the commit has reviewed drift evidence or explicit user authorization.
- Commit message records upstream ref, upstream ref kind, and upstream commit.
- The resulting `HEAD` is the committed sync change and the worktree/index are clean except for intentionally preserved unrelated user changes that were not staged.

Output:
- Local sync commit SHA, staged file summary, preserved unrelated changes when present, and `local sync complete` completion layer.

Stop if:
- Validation failed, is missing, or targets a different SHA.
- Diff/status evidence shows unrelated changes mixed with the sync.
- No sync changes are available to commit.
