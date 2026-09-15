# GitHub Actions rules

Keep workflows direct and fail normally. GitHub Actions maintain repository
source correctness; version publication is outside this control plane.

- A `run` step before checkout must use `${{ github.workspace }}` explicitly if
  the workflow has a non-root default working directory.
- Trusted default-branch workflows check out the triggering `${{ github.sha }}`;
  do not re-resolve a moving branch name.
- Inputs that sandboxed Codex commands must read are written to an ignored
  workspace file; do not assume launcher environment variables remain visible.
- Provider credentials may reach proxy startup but must not be inherited by the
  Codex execution step.
- A model final message never replaces deterministic validation. Protocol sync
  follows [`docs/protocol-sync.md`](../docs/protocol-sync.md).
- Workflows with provider secrets or repository-write authority pin
  third-party Actions to reviewed immutable commits.
