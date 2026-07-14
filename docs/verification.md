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
contract, the fast three-layer workspace canary, and one isolated consumer per
affected public module. Each consumer replaces only the module under test with
its checkout directory; its upstream requirements remain those declared by the
module.

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
