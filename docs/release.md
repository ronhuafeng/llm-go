# Release Checklist

This document records the public API, SemVer policy, and release tag process
for `llmkit-go`.

## Public API

The public API is the set of exported identifiers in these packages:

- `github.com/ronhuafeng/llmkit-go/settle`
- `github.com/ronhuafeng/llmkit-go/llmschema`
- `github.com/ronhuafeng/llmkit-go/llmadapter`
- `github.com/ronhuafeng/llmkit-go/llmstep`

The canonical declaration and method allowlist is stored in
`internal/architecture/testdata/handwritten-api.txt`. Any update must first be
reflected in the normative local refactor plan and then reviewed as API design,
not accepted as incidental test churn.

The following are not public API:

- `internal/` packages.
- Test helpers and fake callers.
- README snippets, except where they describe exported API behavior.
- CI workflow internals.

## SemVer policy

Use standard Go module SemVer tags: `vMAJOR.MINOR.PATCH`.

Before v1.0.0:

- Patch releases should contain bug fixes, documentation, CI, and dependency
  hygiene only.
- Minor releases may add public API.
- Breaking public API changes are allowed only when necessary and must be
  documented in `CHANGELOG.md` with migration notes.

At and after v1.0.0:

- Breaking public API changes require a new major version.
- Additive public API changes require a minor version.
- Bug fixes and documentation-only changes require a patch version.

## Release checklist

1. Confirm the worktree is clean or only contains intended release changes.
2. Review public API changes and update `README.md` and package docs as needed.
3. Update `CHANGELOG.md`.
4. Review dependency provenance:

   ```sh
   GOWORK=off go mod tidy
   GOWORK=off go list -m all
   GOWORK=off go mod graph
   ```

5. Update `THIRD_PARTY_NOTICES.md` if dependencies changed.
6. Run checks:

   ```sh
   gofmt -w $(find . -name '*.go' -not -path './vendor/*')
   GOWORK=off go mod download
   ./scripts/ci/verify-module-hygiene.sh
   GOWORK=off go vet ./...
   GOWORK=off go test ./...
   GOWORK=off go test -race ./...
   GOWORK=off go test ./internal/architecture
   ./scripts/ci/verify-clean-consumer.sh local "$PWD"
   ```

7. Create and push the annotated release tag for the intended version:

   ```sh
   VERSION=v0.3.0
   git tag -a "$VERSION" -m "llmkit-go $VERSION"
   git push origin "$VERSION"
   ```

8. Create a GitHub release from the tag and paste the relevant `CHANGELOG.md`
   entry.

Pushing a `v*` tag starts the `Tag verification` workflow. It resolves the tag
from a fresh external module through normal module resolution, runs synthetic
`ValueDetailed`, `settle.RunDetailed`, and `llmstep.RunDetailed` scenarios, and
records the resolved module metadata, module sums, and exact tag commit as a
workflow artifact. The workflow must pass before publishing the GitHub release.

## Required branch protection checks

Require these `CI` jobs on `main` pull requests:

- `Minimum Go (1.23.0)`
- `Stable Go (ordinary)`
- `Race`
- `Module hygiene`
- `API and clean consumer`

The tag-only `Tag clean consumer` job is a release gate and is not a pull
request branch-protection check.

For v0.3, also verify that the migration table, changelog, README, package
documentation, and handwritten API allowlist list the same removed surface.
Before merging the removal, verify a clean export of caller `main` against the
candidate source with `GOWORK=off` and no committed `replace` or `go.work`.
Validate real module-tag compatibility during the v0.3 RC and stable release
gates.

## Supply-chain hygiene

- Keep dependencies provider-neutral and minimal.
- Do not commit credentials, private prompts, customer data, local absolute
  paths, or generated build artifacts.
- Use `GOWORK=off` before release to verify the module builds outside any local
  workspace.
- Before making the repository public, enable GitHub branch protection,
  Dependabot alerts/security updates, private vulnerability reporting, and
  secret scanning. Add CodeQL or OpenSSF Scorecard workflows once those features
  are available for the repository visibility and organization plan.
