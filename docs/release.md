# Release

A release is a tag on code that already passed deterministic verification. It
must not introduce a second model of API shape, README state, or authorization.

## Before dispatch

1. Merge the source change to `main` through the required `PR verification`
   check.
2. For user-visible changes, update the owning module's `CHANGELOG.md` in that
   source PR.
3. Choose the module's next stable SemVer.

README install commands use `@latest`; they are not release-version state and
are never stamped during publication.

### Breaking module cohorts

This repository is pre-v1 and may intentionally make clean breaking changes
across independently tagged modules in one source commit. A source cohort may
therefore commit a downstream `go.mod` that names the next upstream module
version before that upstream tag exists. Required PR verification may prove the
cohort against repository current source through temporary, uncommitted module
replacements; that proof does not claim the future dependency version is already
published.

Publish dependency modules before their dependents. Do not preserve obsolete
source APIs merely so a downstream module compiles against an older tag. Before
a dependent module is tagged, every dependency version named by its committed
`go.mod` must already exist and the exact committed dependency closure must
resolve with `GOWORK=off` and without replacement. Current-source composition
and published-closure resolution are deliberately separate proofs.

A `codex-adapter` release additionally verifies its committed dependency closure
with `GOWORK=off`, so every dependency version named by its `go.mod` must already
exist before the adapter tag is created. A missing dependency tag is a release
blocker, not a reason to commit `replace`, retain a migration alias, or weaken
the source cohort.

## Publish

Dispatch **Release public module** from `main` with only:

- `module`: `llmkit`, `codexsdk`, or `codex-adapter`;
- `version`: a stable version such as `v0.13.0`.

The release commit is the trusted `github.sha` captured when the workflow is
dispatched. After the protected `production-release` approval, the workflow:

1. runs explicit Go-native proofs on that exact commit: formatting/whitespace,
   owner-local verification, current-source composition, and, when publishing
   `codex-adapter`, the committed published-dependency closure;
2. verifies remote `main` still points to that commit;
3. refuses to reuse an existing version tag;
4. creates the module-prefixed annotated tag with the dedicated release key;
5. creates the GitHub Release for that tag.

There is intentionally no repository verification wrapper between the release
workflow and these Go proofs. The Python/Bash Codex upstream-sync control plane
is not a release gate for unrelated source; Codex protocol source correctness
needed for publication is protected by owner-local Go tests.

If `main` advances while approval or tests are in progress, the release fails;
dispatch it again from the new main. If tag creation succeeds but GitHub
Release creation fails, create the GitHub Release for that existing immutable
tag; never move or recreate the tag.

Module prefixes remain independent:

```text
llmkit/vX.Y.Z
codexsdk/vX.Y.Z
llmcaller/codex/vX.Y.Z
```

There is no README version stamping, release-plan digest,
release-authorization artifact, published-evidence artifact,
Draft/verify/publish state machine, structured `.changes` release ledger, or
public-proxy polling gate.

## Hosted configuration

Tag creation keeps the existing `production-release` Environment and
`RELEASE_DEPLOY_KEY`, because the current formal-tag rules delegate tag
creation to that dedicated Deploy Key. The post-release observation
dispatch reuses `github.token` with `actions: write` on the release job.
No new release secret or Environment is required.

The non-gating live Codex smoke is separate from release. It reuses the existing
Responses-proxy credentials and requires no additional Environment or secret;
see [`verify.md`](verify.md).

## Post-release module resolution smoke

After the immutable tag and GitHub Release exist, **Release public module**
dispatches `post-release-module-smoke.yml` once with that exact tag. The
handoff uses `workflow_dispatch` through the existing repository token so
it does not depend on `release: published` from `GITHUB_TOKEN`. The dispatch
step continues on error: a failed observation start does not mark the
release job failed after publication already succeeded. Manual
`workflow_dispatch` can repeat that single observation if proxy propagation
had not finished.

The smoke does not decide whether the tag is valid, does not move or
recreate tags, and is not a prerequisite for **Release public module**.
A green result means only that the consumer resolution path worked at that
time. A red result is an ecosystem observation to rerun later, not a
release-state change.
