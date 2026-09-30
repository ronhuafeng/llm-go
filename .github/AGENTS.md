# GitHub Actions rules

Keep workflows direct. GitHub Actions orchestrates verification and authorized
repository effects; it is not a second implementation or acceptance authority.

## Candidate and integration

For PRs, the exact current head `H` is the acceptance candidate. Source,
generated, protocol-provenance, and the accepted live gate all verify that same
candidate. Non-PR verification uses the triggering `github.sha`.

Keep the existing source, generated reproducibility, and protocol provenance
contexts independent. Provenance reads the upstream identity from H and is
read-only; unrelated changes complete its context as not applicable.

Auto-forward may advance protected main only by a normal non-force fast-forward
to H after the required checks and existing review policy are satisfied. It
must not merge, rebase, squash, cherry-pick, create commits, force-push, or repair
stale PRs. Revalidate the existing identity, ancestry, checks, and review
conditions before the write. Git's non-fast-forward rejection remains final.
Delete a head ref only after main readback and GitHub's merged-PR observation
prove the existing deletion conditions.

Live-test work reuses this machinery. Do not add a separate approval service,
attestation ledger, candidate model, or extra SHA ceremony.

## Live Codex gate

[`docs/live-codex.md`](../docs/live-codex.md) is the accepted design.
Its implementation transition is explicit there and in
[`docs/verify.md`](../docs/verify.md); documentation alone does not activate a
required check.

- Run the live suite for changes that can affect the Codex runtime path,
  including protocol-sync PRs and the shared dependencies used by the composed
  scenario. An unrelated PR gets successful not-applicable, not a model call.
- Install the official Codex release derived from H's checked-in baseline.
  Do not select `latest` or keep a second runtime-version authority.
- Configure Codex directly to Mini with native URL/key/provider settings.
  The live Codex process may receive its Mini credential through the configured
  environment key. This deliberately replaces the old blanket requirement
  that live Codex receive credentials only through a local proxy.
- Do not install `codex-responses-api-proxy` for this live path or synthesize
  Codex identity headers. Codex owns the headers for its installed version.
- Use the single model/reasoning configuration in the live-test design.
  No model fallback, model matrix, or platform matrix is required.
- Execute the relevant active scenarios without cached success, automatic
  retries, or successful skips for missing prerequisites. Failures block the
  live check; diagnostics may classify them without changing the verdict.
- Support the current single-author controlled same-repository workflow,
  including its protocol-sync bot. Do not add fork-contributor approval
  infrastructure as part of this work.

Mini owns its credential binding and credit controls. The live suite does not
implement provider retry policy. Keep the fixture's `CODEX_HOME` and workspace
isolated, and never print or upload credentials, auth files, or raw private
transcripts.

## Authority and proof hygiene

Provider credentials are not repository-write authority. Live tests receive no
publication/integration App token. Other Agent/protocol workflows retain their
own credential isolation; removing the live proxy does not authorize changing
those unrelated paths.

Repository-owned producers normalize changed Go before sealing or committing
it. Required CI verifies committed source. A model's final message never
replaces deterministic checks or live test assertions.

Inputs that sandboxed commands need belong in an appropriate ignored workspace
fixture; do not assume launcher environment variables are visible to every
child command. This is separate from Codex reading its native provider key.

Workflow tests protect behavior, credential boundaries, exact candidate use,
required/not-applicable/failure outcomes, and integration enforcement. They must
not freeze incidental job count, scenario function count, or step wording.
Pin third-party Actions with provider secrets or repository-write authority to
reviewed immutable commits.
