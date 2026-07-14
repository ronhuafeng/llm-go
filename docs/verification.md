# Repository verification

Pull-request verification is driven by the non-published `repoctl` command in
`internal/tools`. GitHub Actions owns platform wiring; the command owns module
selection, checks, and evidence semantics.

`repoctl affected` compares exact base and head commits, maps changed paths to
the minimal module registry, and closes the result over repository-module
requirements parsed from each `go.mod`. A root orchestration change affects all
modules. A tools-only change remains confined to the non-published tools module.

For every affected public module, CI invokes `repoctl verify-module` in three
separate environments:

- `minimum` runs standalone tests on the module's declared minimum Go version;
- `current` checks formatting, module metadata, vet, ordinary tests, the public
  API inventory, and module-owned generator drift where applicable;
- `race` runs the standalone module with the race detector.

Every module subprocess uses `GOWORK=off`. The current checkout is separately
verified by `repoctl verify-checkout`, which runs the repository boundary
contract, the fast adapter canary, and one isolated consumer per affected
public module. The canary is named three-layer workspace evidence only when the
adapter requires the current checkout identities of both upstream modules.
During staged path migration it is explicitly recorded as a legacy-path adapter
canary and does not claim current three-module composition. Each consumer
replaces only the module under test with its checkout directory; its upstream
requirements remain those declared by the module.

The workspace canary uses read-only module metadata through an ephemeral copy
of `go.work`; any transient workspace sums are removed with that copy. It must
not create or update the repository's `go.work.sum`, because checkout
verification proves one immutable source identity before and after all checks.

The non-published repository tool has its own Go toolchain contract. Minimum-Go
jobs therefore build `repoctl` with current Go before switching the job to the
public module's declared minimum; only the target module is constrained by that
minimum version.

## Evidence boundary

Both verification paths emit format-versioned JSON. `module_source` evidence
describes a standalone module stage. `checkout_source` evidence describes the
composition of the candidate source tree. Each subject records the exact Git
commit and tree and first verifies that the checkout has no unrecorded source.

Checkout-source evidence explicitly does **not** prove:

- published module artifact identity;
- public proxy availability;
- checksum database records.

Those facts belong to release preflight and post-tag verification. A pull
request must never present a filesystem replacement or workspace build as
published-artifact evidence.

The stable required check is `PR verification`. Detailed matrix jobs and JSON
artifacts remain available for diagnosis without making branch protection
depend on a changing set of affected modules.

## Release evidence

The manually dispatched release workflow uses three additional typed seams:

- `repoctl release-plan` verifies the current `main` commit, path-prefixed tag,
  SemVer impact, archived change fragments, mapped legacy API inventory,
  module archive, documentation, dependencies, and input digests;
- `repoctl finalize-release` validates and hashes the minimum/current/race and
  checkout evidence together with the plan into the authorization envelope;
- `repoctl authorize-tag` validates every authorized artifact and re-derives
  the plan after protected-environment approval against a freshly fetched
  `main`; the workflow compares remote `main` again before its
  empty-expectation tag lease push;
- `repoctl verify-tag` binds the immutable tag and authorization digest to the
  public proxy artifact, checksum-database sums, Git origin, canonical module
  `h1:` content sum, observed raw zip SHA-256, exact `go.mod` SHA-256, and an
  isolated typed consumer.

The initial tracer is intentionally exact-scope: only `llmkit/v0.6.0` is
currently authorized. First-tag API mapping is generic across migrated
modules; while a first replacement tag is still absent, repository verification
requires the current canonical inventory to equal its legacy inventory after
the declared flattened import mapping. That migration baseline is not an
allowlist for later versions. The workflow also validates any
pre-existing GitHub Release through the authenticated, paginated Releases list
because GitHub's get-by-tag endpoint returns 404 for Drafts. Exactly one tag
match must be an unverified Draft whose `target_commitish` equals the authorized
commit; duplicate, published, prerelease, or wrong-target matches fail closed.
The remote annotated tag's peeled commit is still validated independently.
This makes lost-response reruns forward-only and idempotent.

Only the protected tag job can read the Environment-scoped
`RELEASE_DEPLOY_KEY`. It keeps `GITHUB_TOKEN` read-only, authenticates local Git
through `actions/checkout`'s strict SSH configuration, pins `origin` to the SSH
URL, and fails before tag creation when the key is absent or invalid. Draft and
published GitHub Release mutation remains separately authorized through their
job-scoped `contents: write` tokens.

The workflow also rejects dispatches whose `github.ref` is not
`refs/heads/main`. GitHub does not expose secret provenance to the job, so the
Environment-only part of this contract additionally requires the hosted
precondition that no repository or organization secret shares the
`RELEASE_DEPLOY_KEY` name.

Hosted tag protection uses two active rulesets over the same formal tag
prefixes. Only the creation-only ruleset has the sole `DeployKey` bypass
(`actor_id: null`, `bypass_mode: always`). The separate deletion and
`non_fast_forward` immutability ruleset has an empty bypass list. The release
identity can therefore create an absent authorized tag, while neither it nor
any other bypass actor can move or delete that tag.

Probe, artifact-validation, and consumer caches are distinct and freshly
created. Every public Go command runs with `GOWORK=off`, `GOVCS=*:off`, an
exclusive `https://proxy.golang.org`, and `sum.golang.org`. A GitHub Release
remains Draft until the post-tag JSON evidence has been uploaded successfully.

The one-time `llmkit/v0.6.0` recovery workflow consumes the immutable preflight
artifact from failed source run `29342863026`; it does not rebuild release
authorization or receive the release Deploy Key. Typed recovery validation
binds artifact `8314814782`, the annotated tag message and peeled commit, and
the original authorization digest before the authorized-commit `repoctl
verify-tag` reruns public-proxy and typed-consumer checks. This read-only job
does not call the Release API, because its `contents: read` workflow token
cannot observe Draft Releases. Diagnostics upload with `if: always()`. Only a
dependent publish job has `contents: write`; it reads and typed-validates Draft
Release `353873922`, then reconciles the exact three expected assets by name
and downloaded SHA-256 without deletion or overwrite. It publishes the same
Draft by ID after another just-in-time validation. A rerun after a lost PATCH
response accepts only the exact verified published terminal state with all
three assets already complete and becomes a no-op; it never uploads to a
published Release. The publish job independently uploads its reconciliation
and PATCH diagnostics with `if: always()`.
