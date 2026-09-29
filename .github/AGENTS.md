# GitHub Actions rules

Keep workflows direct and fail normally. GitHub Actions verifies immutable
repository candidates; it must not become a second implementation authority.

- For a pull request, the exact current PR head H is the only acceptance
  candidate. Source, generated, and protocol-provenance required checks must
  all verify that same H.
- Do not use GitHub's synthetic merge revision, merge queue candidate, rebase
  result, squash result, or a merge-time generated commit as a correctness
  authority.
- On non-PR events, native verification checks the triggering `${{ github.sha }}`.
- Pull-request protocol provenance reads U from H's checked-in baseline and
  performs fresh exact reconstruction only when relevant. The required context
  must still complete successfully as not applicable for unrelated PRs.
- Keep `Root source verification`,
  `Codex generated reproducibility / Generated reproducibility`, and
  `Codex protocol provenance` independent and required.
- Repository-owned producers normalize changed Go source before sealing or
  committing it. Required CI verifies committed source; formatting is not an
  acceptance proof.
- Auto-forward integration is a separate trusted effect. It may advance
  protected `main` only by a normal non-force fast-forward to the exact H that
  already has current required checks and review policy satisfied.
- The auto-forward integrator must not merge, rebase, squash, cherry-pick,
  create a commit, force-push, rewrite the PR branch, or repair a stale PR.
  After readback proves the fast-forward, it may delete the head ref only when
  GitHub reports that same PR merged and the ref still points at integrated H.
- Correctness must not depend on workflow ordering or concurrency. If main moves
  before integration, Git's non-fast-forward rejection is authoritative and the
  PR must be rebased to a new H and reverified.
- For mutable open PR branches, the Git ref is the head identity authority. PR
  API `head.sha` and managed PR-body metadata are projections, not a second
  Git-head authority.
- Workflow tests protect exact identity, authority, permissions, and
  publication/failure boundaries. Do not freeze incidental job topology.
- Inputs that sandboxed Codex commands must read are written to an ignored
  workspace file; do not assume launcher environment variables remain visible.
- Provider credentials may reach proxy startup but must not be inherited by the
  Codex execution step.
- A model final message never replaces deterministic validation. Protocol sync
  follows [`docs/protocol-sync.md`](../docs/protocol-sync.md).
- Workflows with provider secrets or repository-write authority pin third-party
  Actions to reviewed immutable commits.
