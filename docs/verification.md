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
  SemVer impact, archived change fragments, the canonical API inventory diff
  from the latest stable module tag, module archive, documentation,
  dependencies, and input digests;
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

The release workflow accepts the exact next stable SemVer derived from the
module's latest stable tag and archived fragments. A metadata-only API diff has
a patch floor, an additive diff has a minor floor, and a breaking diff requires
at least a pre-v1 minor or post-v1 major release plus an explicit breaking
fragment. The SDK path combines its handwritten inventory diff with the
module-owned generated compatibility report, verifies generated facts, and
runs a public-artifact consumer over both a generated facade and the exact
lifecycle API. The adapter path reads the declared upstream tuple from the proxy
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

## Retired migration acceptance audit

The one-time `Migration acceptance audit` workflow was the cutover gate for
ADR-0023. It checked out an exact `main` commit and produced all-module
minimum-Go, current-Go, race, and checkout-composition evidence before invoking
the typed acceptance builder. It emitted one versioned JSON report with six
mandatory categories:

- imported-history provenance;
- old-to-new API and behavior equivalence;
- architecture boundaries;
- independent module consumption;
- the published dependency chain;
- cutover readiness.

Every check named the exact files, Git objects, Release assets, or Proxy module
artifacts it inspected. A report was incomplete when a category, source
evidence file, Release asset, checksum, typed consumer, or required document was
missing. The workflow uploaded an incomplete report when an upstream evidence
job failed, so absence could not collapse into an ambiguous skipped gate.
Inputs downloaded into a source-verification job lived under the runner
temporary directory, outside the checkout. Checkout verification treated a
downloaded audit input materialized in the repository tree as untracked source
and failed closed.

Published-chain evidence was independently observed at audit time. The command
downloaded and digest-checked the three evidence assets from each stable GitHub
Release, resolved all three replacement tags through fresh isolated public
Proxy caches, and ran the adapter's exact declared/resolved tuple consumer.
Each immutable evidence asset was validated against the schema version it was
published with. It also resolved the three final legacy migration releases from
fresh public Proxy state and bound their origins to `migration-provenance.json`.

The authoritative pre-archive and post-archive Actions artifacts, their upload
digests, report digests, exact commits, and exact trees are permanently bound by
`docs/migration/archive-evidence.json`. The workflow was retired after cutover
and archival completed; later `main` commits do not produce new migration
acceptance reports. Current repository verification validates the committed
immutable provenance and archival evidence without retaining a report builder
or replay path.
