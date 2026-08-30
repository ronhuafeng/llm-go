# Protected release operation

Tags are created only by the manually dispatched `Release public module`
workflow. It accepts the next stable SemVer for one registered public module.

The plan diffs the module's canonical API inventory against the latest stable
tag. Fragments may raise impact; they cannot lower the mechanical floor.
The adapter's published `go.mod` owns the `llmkit`/`codexsdk` tuple.

Seams: `release-plan` → `finalize-release` → Environment approval →
`authorize-tag` → empty-expectation tag create → Draft Release → `verify-tag`
against the public proxy.

## Hosted identity

Inspect these before every dispatch. A workflow file cannot prove them.

1. Exactly one repository Deploy Key, write-capable, dedicated to this
   workflow. GitHub's `DeployKey` bypass uses `actor_id: null`, so it matches
   every Deploy Key. Extra keys enlarge the tag-create identity.
2. Private key only as `RELEASE_DEPLOY_KEY` on the `production-release`
   Environment. Never a repository or organization secret.
3. Environment: `protected_branches=true`, reviewer `ronhuafeng`,
   `prevent_self_review=false` (single maintainer). The workflow still rejects
   any ref other than `refs/heads/main`.
4. Two active tag rulesets on the same formal prefixes:
   - creation-only `18935608`: `DeployKey` bypass, `bypass_mode: always`
   - immutability `18924050`: delete + `non_fast_forward`, empty bypass

The release key may create an absent tag. It cannot move or delete one.

Rotate by stopping dispatches, deleting the old key first, then installing a
new pair. On compromise, delete the key and secret; never move a tag.

## Transaction

1. Archive `.changes` fragments and give `CHANGELOG.md` a `## [version]`
   section.
2. Dispatch from `main` with module ID, version, and current commit.
3. Approve the authorization digest that hashes the plan plus
   minimum/current/race/checkout evidence.
4. After approval, `authorize-tag` re-derives the plan on freshly fetched
   `main`. Git cannot make tag create conditional on `main` staying still
   without updating `main`; the job reads `main` and creates the tag with an
   empty-expectation lease instead.
5. `verify-tag` binds the immutable tag to the proxy artifact, official sums,
   and an isolated consumer. The GitHub Release stays Draft until that
   evidence is uploaded.

## Failure

Before the tag exists: fix source and run a new preflight. An old digest never
authorizes a new plan.

After the tag exists: never delete, move, or recreate it. Defects get a new
version. A lost Draft response is recovered by rerunning the Draft job; do
not create a second release.
