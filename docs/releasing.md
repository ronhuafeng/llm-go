# Protected release operation

Public module tags are created only by the manually dispatched
`Release public module` GitHub Actions workflow. The first tracer deliberately
supports only `llmkit`; SDK and adapter support must add their owner-specific
API, generator, dependency, and composed-consumer gates before they become
selectable inputs. It also authorizes only the first `llmkit/v0.6.0` release:
later toolkit versions require a new mechanical API-impact baseline rather than
silently treating the first migration inventory as an evergreen allowlist.

## One-time GitHub configuration

Before the workflow is used, create a `production-release` Environment in the
repository settings. Require designated release reviewers, prevent the actor
from self-approving, and restrict deployment branches to the protected `main`
branch. The workflow's default permission is read-only; only the approved tag
job and GitHub Release jobs receive `contents: write`.

The formal-tag ruleset must also grant the GitHub Actions release identity a
narrow creation bypass for the protected module prefixes. An empty bypass list
blocks the approved workflow as well as local users; do not weaken the ruleset
for general maintainers to compensate.

Do not dispatch a production release until that Environment protection is
configured. A workflow file cannot prove or substitute for repository-hosted
reviewer policy.

## Release transaction

1. Prepare the module changelog and archive its reviewed fragments under
   `<module>/.changes/releases/<version>/` in the release commit.
2. Dispatch the workflow with the module ID, exact stable version, and exact
   current `main` commit.
3. Inspect the uploaded release plan, the minimum/current/race/checkout JSON
   evidence, and the final release-authorization digest that hashes all five
   artifacts. Approve that authorization digest in the protected tag job.
4. The tag job re-runs authorization against current `origin/main`. Any main
   advance observed before tag push or any changed evidence invalidates the
   approval. The job builds `repoctl` before this boundary, fetches `main`
   immediately before authorization, fetches it again afterwards, and checks
   the remote head once more immediately before pushing.
5. CI creates the one authorized annotated path-prefixed tag and then a Draft
   GitHub Release. A rerun reuses an existing Release only when it is still
   Draft and its tag matches exactly; 404 creates it, while a published or
   mismatched Release fails closed. The Release API's `target_commitish` is not
   commit evidence for an existing tag, so CI independently requires the
   remote annotated tag's peeled ref to equal the authorized commit before and
   after Draft lookup or creation.
6. Post-tag verification waits for public-proxy propagation with bounded
   retries. Every retry uses a disposable probe cache; artifact validation and
   the external typed consumer use separate fresh caches. Module caches are
   writable solely so teardown is reliable, and a cleanup failure fails the
   evidence rather than silently leaving reusable state behind.
7. CI attaches the plan, release authorization, and published evidence before
   changing the GitHub Release from Draft to `verified`.

The plan is deterministic and binds the commit, tree, module identity, target
version, tag, declared requirements, mapped API baseline, change fragments,
module archive, and release inputs. Human-readable notes are rendered from the
same archived fragments.

The authorization envelope binds the plan digest and the exact SHA-256 of all
four preflight evidence files. The module archive's canonical `h1:` content sum
is the cross-stage packaging identity. Raw zip SHA-256 values are retained as
observations only because equivalent ZIP compression and metadata can produce
different bytes.

Git cannot atomically make creation of one ref conditional on an unrelated ref
remaining unchanged without also attempting to update that unrelated ref. The
workflow therefore does not push or no-op-update protected `main`: it minimizes
the remaining host-level observation window with repeated authoritative reads,
then creates the tag with an empty-expectation lease so concurrent creation or
reuse of that tag fails closed. Repository branch protection remains the owner
of `main`; release automation never weakens it to manufacture a cross-ref
transaction.

## Failure and recovery

Before tag creation, fix the source or configuration and run a new preflight.
An approval for an older digest never authorizes the replacement plan.

After tag creation, never delete, move, or recreate the tag. A failed post-tag
job leaves the GitHub Release Draft. A transient observation failure may be
rechecked against that same immutable artifact, but it does not authorize a tag
write. Any source, archive, checksum, provenance, or behavior defect is fixed
with a new patch or minor version. The failed version remains visibly
unverified; it is never made to point at the correction.

If Draft creation succeeds but its client loses the response, rerun the failed
Draft job. The exact-match validation reuses that Draft; it never attempts a
second release or turns an already published Release back into mutable state.
