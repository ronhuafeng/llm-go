# Command: detect-drift

State:
- Caller has a resolved target and needs policy plus local drift artifacts.

Inputs:
- Target ref, ref kind, target SHA, target explicit/default status, policy mode, and downgrade policy.
- Generation mode: upstream Codex repo URL, plus optional `CODEXSDK_CODEX_REPO` and `CODEXSDK_SYNC_OUT` overrides outside GitHub Actions.
- Compare-only mode: candidate schema directory, checked-in baseline, resolved target SHA, and output directory; no upstream repo path is required.

Tools:
- `scripts/codexsdk_target_policy.py`
- `scripts/codexsdk_track_upstream.sh`

Repository preparation:
- Run target policy first. Prepare or fetch an upstream repository only after policy returns `allow`, or returns `skip` while `force_compare` requires generation.
- In GitHub Actions, read the upstream URL only from `.cache/codexsdk-sync/action-inputs.json`. Use `.cache/openai-codex` as the repository and `.cache/codexsdk-upstream-<target-sha-prefix>` as the output directory.
- Outside GitHub Actions, use the same module-local cache paths by default. Honor `CODEXSDK_CODEX_REPO` or `CODEXSDK_SYNC_OUT` when the caller explicitly supplies one.
- Bind `upstream_repo` to the already-read input value. Resolve the selected paths before creating directories, then initialize or verify the repository without fetching all refs. Run tracking in the same shell so it consumes those exact paths:

  ```bash
  set -euo pipefail
  module_root="$PWD"
  target_prefix="${target_sha:0:12}"
  if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
    codex_repo="$module_root/.cache/openai-codex"
    sync_out="$module_root/.cache/codexsdk-upstream-$target_prefix"
  else
    codex_repo="${CODEXSDK_CODEX_REPO:-$module_root/.cache/openai-codex}"
    sync_out="${CODEXSDK_SYNC_OUT:-$module_root/.cache/codexsdk-upstream-$target_prefix}"
    [[ "$codex_repo" == /* ]] || codex_repo="$module_root/${codex_repo#./}"
    [[ "$sync_out" == /* ]] || sync_out="$module_root/${sync_out#./}"
  fi
  rustup_home="$module_root/.cache/rustup"
  cargo_home="$module_root/.cache/cargo-home"
  cargo_target="$module_root/.cache/cargo-target/codex"

  mkdir -p \
    "$(dirname "$codex_repo")" \
    "$(dirname "$sync_out")" \
    "$rustup_home" \
    "$cargo_home" \
    "$cargo_target"
  if [[ -e "$codex_repo" ]]; then
    if [[ ! -d "$codex_repo/.git" ]]; then
      echo "cached Codex path is not a Git repository: $codex_repo" >&2
      exit 1
    fi
    actual_origin="$(git -C "$codex_repo" remote get-url origin)"
    if [[ "$actual_origin" != "$upstream_repo" ]]; then
      echo "cached Codex origin $actual_origin does not match $upstream_repo" >&2
      exit 1
    fi
  else
    git init --quiet "$codex_repo"
    git -C "$codex_repo" remote add origin "$upstream_repo"
  fi

  RUSTUP_HOME="$rustup_home" \
  CARGO_HOME="$cargo_home" \
  CARGO_TARGET_DIR="$cargo_target" \
  scripts/codexsdk_track_upstream.sh \
    --codex-repo "$codex_repo" \
    --commit "$target_sha" \
    --source-ref "$target_ref" \
    --source-ref-kind "$target_kind" \
    --out "$sync_out" \
    --json
  ```

- Pass absolute repository and output paths to tracking; `git -C` resolves relative worktree paths from the repository rather than the module root.
- Keep Rustup, Cargo registry, and Cargo build state inside the writable module cache because the sandbox may expose the runner home read-only.
- Require an existing path to be a Git repository whose `origin` URL exactly matches `upstream_repo`; stop on missing or mismatched provenance.
- Do not discover the repository by searching the runner, reading README files, or reusing sibling checkouts. Those sources are not bound to the workflow-owned upstream URL and may select unrelated or stale Codex source.
- Let `scripts/codexsdk_track_upstream.sh` fetch the exact resolved target after policy allows it. Do not perform a separate broad clone or fetch.

Boundaries:
- Run policy before drift generation.
- May write policy output and local drift artifacts.
- May let tracking fetch the selected target narrowly after policy allows.
- Must not apply a candidate or mutate checked-in sync files.
- Must not configure authentication, stage, commit, push, tag, create PRs, dispatch workflows, or publish remote state.

Checks:
- Policy JSON parses and has decision plus reason.
- Generation uses absolute repository and output paths, writable module-local Rust homes, and the selected repository's verified `origin`, without searching for another checkout.
- On `allow`, compact reports include `SUMMARY.md`, `drift_summary.json`, and `matrix_update_skeleton.json`.
- Artifact evidence records upstream ref, upstream SHA, and drift fingerprint; its `source_commit` equals the resolved target SHA.
- Checked-in baseline files remain unchanged.

Output:
- Policy decision, reason, drift status, drift fingerprint, target provenance, and artifact directory.

Stop if:
- Policy returns `block`; stop drift generation, and fail a caller-owned `force_compare` verification.
- Policy returns `skip` and the caller did not request `force_compare`; forced comparison may continue read-only drift generation.
- The cached repository is missing Git metadata, its `origin` differs from `upstream_repo`, or tracking resolves a commit other than the target SHA.
- Candidate provenance is missing or drift generation fails.
