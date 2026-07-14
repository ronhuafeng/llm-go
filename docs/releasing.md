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
4. Keep two active tag rulesets with identical formal-module tag targets:
   - a **creation-only** ruleset containing only the tag-creation restriction.
     Its bypass list contains exactly one entry: `DeployKey`, represented by
     `actor_id: null`, with `bypass_mode: always`;
   - an **immutability** ruleset containing tag deletion and
     `non_fast_forward`, with an empty bypass list.

The existing hosted ruleset `18924050` combines creation, deletion, and
`non_fast_forward`. Migrate without weakening immutability: first create and
activate the matching creation-only ruleset, then remove only the creation rule
from `18924050` so it becomes the immutability owner. Never add a bypass actor
to the immutability ruleset. Both rulesets must remain active and target the
same formal tag prefixes.

GitHub represents the `DeployKey` bypass with `actor_id: null`, so it applies
to every Deploy Key in the repository rather than selecting the dedicated key.
The exactly-one-key rule above is therefore part of the security boundary.
Because the bypass exists only on the creation-only ruleset, the release key
may create an absent formal tag but cannot update, force-move, or delete an
existing one.

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
Environment secret, creation-only bypass, and bypass-free immutability ruleset
have all been inspected. A workflow file cannot prove or substitute for
repository-hosted settings.

### Key rotation and revocation

Treat key rotation as one maintenance transaction and prefer release
unavailability over overlapping identities:

1. stop new release dispatches and let any active tag job finish or cancel it;
2. delete the old Deploy Key first, making tag pushes fail closed;
3. generate a new key pair offline, add only its public key as the sole
   write-enabled Deploy Key, and replace the `production-release` Environment's
   `RELEASE_DEPLOY_KEY` secret with the matching private key;
4. inspect the Environment scope and both formal-tag rulesets again, including
   the immutability ruleset's empty bypass list, then securely destroy the
   retired private key.

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
5. CI creates the one authorized annotated path-prefixed tag through the
   creation-only bypass. The bypass-free immutability ruleset prevents that key
   from moving or deleting the tag. CI then creates a Draft GitHub Release. A
   rerun discovers releases through the authenticated, fully paginated
   Releases list because GitHub's get-by-tag endpoint returns 404 for Draft
   Releases. Zero exact tag matches permits creation; one is reusable only when
   it is Draft, not a prerelease, and its `target_commitish` equals the
   authorized commit. Duplicate matches, a published release, or a mismatched
   target fail closed. After creation CI lists and validates again. The remote
   annotated tag's peeled ref remains the independent immutable commit evidence
   and is checked before and after Draft lookup or creation.
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
Draft job. Paginated exact-tag discovery finds and validates that Draft even
though GitHub's get-by-tag endpoint hides it; it never attempts a second
release or turns an already published Release back into mutable state.

### Exact recovery for the first tracer

Run `29342863026` used the earlier get-by-tag lookup and failed after it had
already created immutable tag `llmkit/v0.6.0` and Draft Release `353873922`.
GitHub reruns use the workflow definition captured by the original run, so
rerunning that failed job would repeat the same 404. A new production release
dispatch is also invalid because the tag already exists.

Use only the manually dispatched `Recover llmkit v0.6.0 release` workflow from
`main`, with its sole source-run option `29342863026`. This is an exact,
forward-only recovery transaction, not a second release authorization. Its
read-only verification job:

- downloads original artifact `8314814782` from that failed run;
- validates the original plan, authorization, and all four preflight evidence
  hashes without rebuilding any of them;
- binds the failed workflow run, artifact, annotated tag and authorization
  message, and peeled commit
  `14f28b0dd4727f079c02ba3139c326ed249bb86a`;
- builds `repoctl` from the authorized commit and reruns the same public Proxy,
  checksum, origin, and typed-consumer verification; and
- uploads diagnostics even when verification fails.

The read-only token deliberately does not query a Draft Release: GitHub's
Release API does not expose Drafts to this workflow token with only
`contents: read`. Only after source, tag, authorization, and public-artifact
verification succeeds does a separate job receive `contents: write`. That job
first reads and typed-validates Draft Release `353873922`, including its exact
tag and target, before it reconciles exactly three assets: the original release
plan, release authorization, and new published evidence.
Expected assets already present are downloaded by asset ID and must have the
same SHA-256 as this run's evidence; only missing assets are uploaded. An
unexpected name, duplicate, incomplete upload, or hash mismatch fails closed,
and no asset is deleted or overwritten. After upload, all three are listed,
downloaded, and verified again before publishing that same Draft. The publish
job uploads its own `if: always()` diagnostic artifact, distinct from verify
diagnostics, so partial reconciliation, upload responses, pre-PATCH state, and
typed PATCH validation remain inspectable on failure.

Missing asset names are URL-encoded and posted by exact Draft ID to the
absolute `https://uploads.github.com/repos/.../releases/{id}/assets` endpoint.
Do not derive this endpoint with `gh api --hostname uploads.github.com`: that
form targets the wrong `api.uploads.github.com` host.

If the publish PATCH succeeds but its response is lost, a rerun accepts only
the exact already-published terminal state: the same release ID, tag, target,
non-prerelease status, verified title, and three content-matching assets. It
then succeeds without another PATCH. A published Release with a missing or
mismatched asset fails closed; recovery never repairs assets after publication.
The recovery workflow has no Deploy Key,
tag-write step, release-plan construction, or tag authorization. It must never
create, move, or delete a tag, replace the Draft, delete an asset, or use local
proxy evidence as completion proof.
