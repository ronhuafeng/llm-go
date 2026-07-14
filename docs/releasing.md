# Protected release operation

Public module tags are created only by the manually dispatched
`Release public module` GitHub Actions workflow. The first tracer deliberately
supports only `llmkit`; SDK and adapter support must add their owner-specific
API, generator, dependency, and composed-consumer gates before they become
selectable inputs. It also authorizes only the first `llmkit/v0.6.0` release:
later toolkit versions require a new mechanical API-impact baseline rather than
silently treating the first migration inventory as an evergreen allowlist.

## One-time GitHub configuration

Before the workflow is used, configure all of the following repository-hosted
controls:

1. Keep exactly one repository Deploy Key. It must be dedicated to this release
   workflow and have write access. Do not retain unrelated read-only or write
   Deploy Keys in this repository.
2. Store its private key only as the `RELEASE_DEPLOY_KEY` secret on the
   `production-release` Environment. Do not make it a repository or
   organization secret, write it to the repository, or print it in setup or
   diagnostic output.
3. Configure the Environment with `protected_branches=true` and require the
   repository owner, `ronhuafeng`, as reviewer. GitHub's setting admits any
   protected branch rather than naming only `main`, so the workflow separately
   fails unless `workflow_dispatch` originated from `refs/heads/main`. This
   personal repository has a single maintainer, so `prevent_self_review=false`
   is an explicitly accepted exception: the approval is an audit and intent
   boundary, not independent separation of duties.
4. Add the `DeployKey` actor to the formal-tag ruleset bypass list. GitHub
   represents this bypass with `actor_id: null`, so it applies to every Deploy
   Key in the repository rather than selecting the dedicated key. The
   exactly-one-key rule above is therefore part of the security boundary. Use
   `bypass_mode: always`; a pull-request-only bypass cannot authorize tag
   creation.

GitHub rejected the GitHub Actions Integration (`actor_id: 15368`) for this
personal-repository ruleset with HTTP 422: the Integration must be part of the
ruleset source or owner organization. A PAT/user bypass, removing the ruleset,
or deactivating its enforcement is not an approved substitute. The dedicated
repository-scoped Deploy Key is the narrow release identity chosen for this
constraint.

The workflow's default permission remains read-only. The protected tag job
also keeps `GITHUB_TOKEN` at `contents: read`; only the Draft Release and final
publish jobs receive `contents: write`. After Environment approval,
`actions/checkout` installs the Deploy Key into the job-local Git
configuration with strict SSH host checking, the workflow pins `origin` to the
SSH repository URL, and checkout removes the credential during post-job
cleanup. A missing or invalid Environment secret fails before tag creation.

GitHub's `${{ secrets.RELEASE_DEPLOY_KEY }}` expression does not expose which
secret scope supplied the value. If the Environment secret were absent, a
forbidden repository or organization secret with the same name could be used
as fallback. The workflow can fail closed for an empty value or invalid SSH
key, but proving Environment-only scope is a hosted-configuration precondition:
inspect all three secret scopes and ensure the name exists only on
`production-release` before dispatch.

Do not dispatch a production release until the Environment, sole Deploy Key,
Environment secret, and ruleset bypass have all been inspected. A workflow
file cannot prove or substitute for repository-hosted settings.

### Key rotation and revocation

Treat key rotation as one maintenance transaction and prefer release
unavailability over overlapping identities:

1. stop new release dispatches and let any active tag job finish or cancel it;
2. delete the old Deploy Key first, making tag pushes fail closed;
3. generate a new key pair offline, add only its public key as the sole
   write-enabled Deploy Key, and replace the `production-release` Environment's
   `RELEASE_DEPLOY_KEY` secret with the matching private key;
4. inspect the Environment scope and formal-tag ruleset again, then securely
   destroy the retired private key.

On suspected compromise, immediately cancel active release jobs, delete the
Deploy Key, and delete the Environment secret. Preserve audit logs and do not
move or recreate any tag. Restore release capability only through the rotation
procedure with a new pair. Never expose either old or new private key material
in an issue, pull request, workflow artifact, command line, or log.

## Release transaction

1. Prepare the module changelog and archive its reviewed fragments under
   `<module>/.changes/releases/<version>/` in the release commit.
2. Dispatch the workflow with the module ID, exact stable version, and exact
   current `main` commit.
3. Inspect the uploaded release plan, the minimum/current/race/checkout JSON
   evidence, and the final release-authorization digest that hashes all five
   artifacts. Approve that authorization digest in the protected tag job.
4. The workflow first requires that dispatch originated from `main`. The tag
   job then re-runs authorization against current `origin/main`. Any main
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
