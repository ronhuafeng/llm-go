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
public module. The canary now uses the current package identities because the
adapter requires both repository upstream modules. Each consumer
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

The release tracer is intentionally exact-scope: only `llmkit/v0.6.0`,
`codexsdk/v0.6.0`, and `llmcaller/codex/v0.5.0` are authorized. The SDK path
additionally verifies its module-owned generated facts and runs a
public-artifact consumer over both a generated facade and the exact lifecycle
API. The adapter path reads the declared upstream tuple from the proxy
artifact's `go.mod`, compares it with an isolated consumer's resolved graph,
records official sums for all three internal modules, and executes a typed
three-layer call that checks neutral evidence and the complete exact SDK result.
Because Go's lazy module loading can list an indirect module before fetching
its zip, the adapter consumer first materializes the entire versioned graph
with `go mod download -json all` through the exclusive public Proxy. Every
download must carry both official sums, and its complete path, version, and sum
set must exactly equal the subsequent `go list -m -json all` result; missing,
extra, replaced, excluded, or non-stable modules fail closed. Public-network
commands remain bounded, with a five-minute per-command limit.
First-tag API mapping is generic across migrated modules; while a first
replacement tag is still absent, repository verification requires the current
canonical inventory to equal its legacy inventory after the declared flattened
import mapping. That migration baseline is not an allowlist for later versions.
The workflow also validates any
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

The one-time `llmcaller/codex/v0.5.0` recovery workflow binds failed run
`29383675440`, original preflight artifact `8330664749`, immutable tag commit
`5fd612b358292ee587c558dbd8041c5a75aea0d7`, Draft Release `354170731`, and the
original authorization digest. It builds the corrected typed verifier from
current `main`, but verifies the separately checked-out authorized source and
the original immutable preflight. This is a verifier recovery, not a new
release authorization: it has no tag-write identity, and only its dependent
write-scoped job may reconcile the exact three assets and publish the same
Draft after the corrected full-graph checksum and typed-consumer checks pass.

## Migration acceptance audit

The manually dispatched `Migration acceptance audit` workflow is the single
cutover gate for ADR-0023. It checks out the exact current `main` commit and
produces all-module minimum-Go, current-Go, race, and checkout-composition
evidence before invoking `repoctl migration-audit`. The typed command emits one
versioned JSON report with six mandatory categories:

- imported-history provenance;
- old-to-new API and behavior equivalence;
- architecture boundaries;
- independent module consumption;
- the published dependency chain;
- cutover readiness.

Every check names the exact files, Git objects, Release assets, or Proxy module
artifacts it inspected. The report is incomplete when a category, source
evidence file, Release asset, checksum, typed consumer, or required document is
missing. The workflow still uploads an incomplete report when an upstream
evidence job failed, so absence cannot collapse into an ambiguous skipped gate.
Inputs downloaded into a source-verification job live under the runner
temporary directory, outside the checkout. Checkout verification treats a
downloaded audit input materialized in the repository tree as untracked source
and fails closed.

Published-chain evidence is independently observed at audit time. The command
downloads and digest-checks the three evidence assets from each stable GitHub
Release, resolves all three replacement tags through fresh isolated public
Proxy caches, and runs the adapter's exact declared/resolved tuple consumer.
Each immutable evidence asset is validated against the schema version it was
published with; a later verifier schema must not make an earlier valid Release
unverifiable.
It also resolves the three final legacy migration releases from fresh public
Proxy state and binds their origins to `migration-provenance.json`.

The authoritative result is the post-merge Actions artifact named
`migration-acceptance-<main commit>`. Its upload digest and the report's own
digest identify the exact proof used to authorize archival. A report is not
checked into the source tree because doing so would falsely claim to have
audited the commit that contains itself. The command and schema are checked in;
the immutable workflow artifact is the evidence for the exact post-merge tree.
