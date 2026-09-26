# GitHub Actions rules

Keep workflows direct and fail normally. GitHub Actions verifies immutable
repository candidates; it must not become a second implementation authority.

- A `run` step before checkout must use `${{ github.workspace }}` explicitly if
  the workflow has a non-root default working directory.
- Trusted default-branch and integration workflows check out the triggering
  `${{ github.sha }}`; do not re-resolve a moving branch name.
- Pull-request protocol provenance checks out the exact
  `${{ github.event.pull_request.head.sha }}` and reads U from that head's
  checked-in baseline identity.
- Keep source correctness, generated reproducibility, and protocol provenance as
  independent GitHub checks. Branch protection should require all three; do not
  hide provenance behind `Root source verification`.
- A required provenance context must always report a result. For unrelated PRs
  and non-PR integration events, complete it explicitly as not applicable rather
  than suppressing the required workflow with broad path filters.
- Repository-owned producers normalize changed Go source before sealing or
  committing it. Required CI verifies the committed candidate; it does not use
  `gofmt` as an acceptance proof.
- Exact provenance proves only H/U reconstruction. Do not add ordinary source
  formatting, vet, or test responsibilities to that job.
- Workflow tests protect authority, immutable identity, normalization-before-
  seal, and publication/failure boundaries. Do not freeze incidental job count
  or step topology.
- Inputs that sandboxed Codex commands must read are written to an ignored
  workspace file; do not assume launcher environment variables remain visible.
- Provider credentials may reach proxy startup but must not be inherited by the
  Codex execution step.
- A model final message never replaces deterministic validation. Protocol sync
  follows [`docs/protocol-sync.md`](../docs/protocol-sync.md).
- Workflows with provider secrets or repository-write authority pin third-party
  Actions to reviewed immutable commits.
