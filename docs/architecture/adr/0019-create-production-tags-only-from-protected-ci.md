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

The tag-creation job receives the minimum required write permission only after
environment approval. It verifies that the approved authorization, current
`main` commit, and tag operation still match before writing the tag.

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
- Emergency correction never mutates an existing public version.

## Considered options

Allowing maintainers to push formal tags locally was rejected because the tag
could bypass the approved authorization and preflight evidence.

Relying only on a tag-triggered secondary workflow was rejected because release
correctness would depend on event-delivery and token-trigger behavior outside
the explicit transaction.

Moving a failed tag was rejected because public module tags and proxy artifacts
are immutable identities.
