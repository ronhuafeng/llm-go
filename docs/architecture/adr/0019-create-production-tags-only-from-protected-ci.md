---
status: accepted
---

# Create production tags only from protected CI

Formal public-module tags are created only by a protected GitHub Actions release
workflow. Local `repoctl` executions may construct and verify dry-run release
plans but cannot create a production release identity.

The release authorization sequence is:

```text
explicit workflow dispatch
    -> repoctl release plan
    -> deterministic preflight
    -> protected-environment approval
    -> immutable path-prefixed tag
    -> explicit post-tag proxy verification
    -> verified GitHub Release
```

The machine-readable release plan binds:

- repository commit;
- module identity and module path;
- target version and exact tag;
- declared dependency tuple;
- ordered release operations;
- deterministic release inputs.

After preflight completes, a self-digesting release authorization binds that
plan and the exact SHA-256 of every minimum/current/race/checkout evidence
file. The protected approval applies to this complete authorization envelope,
not to the plan alone.

The tag-creation job receives its narrowly scoped release identity only after
environment approval; its `GITHUB_TOKEN` remains read-only. It verifies that
the approved authorization, current `main` commit, and tag operation still
match before writing the tag.

For this personal repository, production tag pushes use one dedicated,
repository-scoped write Deploy Key whose private key exists only as the
`production-release` Environment secret. The tag job's `GITHUB_TOKEN` remains
read-only. GitHub does not permit the GitHub Actions Integration to be added as
a bypass actor when that Integration is not part of the personal repository's
ruleset source or an owner organization; the observed attempt with Integration
`actor_id: 15368` returned HTTP 422. PAT/user bypasses and disabling the ruleset
are rejected.

GitHub's `DeployKey` ruleset bypass has no per-key actor ID and therefore
applies to every Deploy Key attached to the repository. Consequently, the
repository must have exactly one Deploy Key: the dedicated release key. Its
ruleset entry uses `bypass_mode: always`, which is required for tag creation.
The Environment admits protected branches, while the workflow separately
requires dispatch from `refs/heads/main`. Because the repository has only one
maintainer, self-review is an accepted exception and records intent without
claiming independent separation of duties. Environment-only secret scope is a
hosted precondition: GitHub secret expressions do not reveal whether a missing
Environment value fell back to a same-named repository or organization secret.

Tag namespaces are protected against ordinary local creation, movement, and
reuse. A post-tag failure marks the immutable tag unverified; recovery is a
forward release with a new version.

Post-tag verification is explicitly scheduled by the release workflow. It does
not assume that a secondary workflow will happen to run in response to an event
created by automation. A separate tag-push audit may detect unexpected tag
creation but is not the primary release transaction.

A GitHub Release remains draft or visibly unverified until proxy verification
succeeds.

## Consequences

- Human approval applies to an inspectable, evidence-bound authorization
  rather than manual release commands.
- The commit, tag, dependency tuple, and verification evidence are bound.
- Pull-request workflows remain read-only.
- Local environments can reproduce preflight without holding publication
  authority.
- Deploy Key rotation revokes the old key before installing the replacement,
  deliberately failing closed rather than overlapping bypass identities.
- Emergency correction never mutates an existing public version.

## Considered options

Allowing maintainers to push formal tags locally was rejected because the tag
could bypass the approved authorization and preflight evidence.

Relying only on a tag-triggered secondary workflow was rejected because release
correctness would depend on event-delivery and token-trigger behavior outside
the explicit transaction.

Moving a failed tag was rejected because public module tags and proxy artifacts
are immutable identities.

Using a PAT or user bypass was rejected because it grants release authority to
a reusable person-level credential instead of one repository-scoped release
identity. Keeping unrelated Deploy Keys was rejected because GitHub cannot
scope the ruleset bypass to only one of them.
