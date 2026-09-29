# Auto-forwardable pull requests

An auto-forwardable pull request uses one immutable candidate identity from
verification through integration.

## Contract

Let:

- `H` be the current PR head;
- `B` be the current main head at integration time;
- `U` be H's exact upstream protocol identity when relevant.

The PR is auto-forwardable when:

```text
B ∈ ancestors(H)
AND
all required checks for H succeeded
AND
repository review policy for H is satisfied
```

The integration effect is only:

```text
main: B ──fast-forward──> H
```

No merge commit, squash commit, merge-time rebase, cherry-pick, force push, or
other commit creation is part of integration.

## Why

The repository wants this invariant:

```text
tested H == reviewed H == integrated H == new main
```

GitHub's default synthetic merge revision is useful for conventional merge
workflows, but it creates a second candidate identity. Rebase and squash merge
also create new commit identities after CI. Auto-forward removes that split.

## Stale PRs

If main moves after H is verified, Git decides whether H can still be
integrated.

A normal push:

```sh
git push origin H:refs/heads/main
```

succeeds only if it is fast-forward. A non-fast-forward rejection means the PR
is stale. The integration workflow stops.

Recovery is:

```text
rebase PR onto current main
→ new H
→ rerun current-H checks
→ reevaluate review policy
→ try fast-forward again
```

The integration workflow never performs that recovery itself.

## Current-head proofs

Required PR checks are:

- `Root source verification`
- `Codex generated reproducibility / Generated reproducibility`
- `Codex protocol provenance`

All verify current H. Provenance performs exact H/U reconstruction when
relevant, otherwise it completes as not applicable.

## Integration workflow

The trusted `Auto-forward PR` workflow remains manually dispatchable with a PR
number. Successful `PR verification` runs for the repository's protocol-sync
bot are also routed through `Dispatch auto-forward`, which validates the PR
identity, exact verified head, controlled branch namespace, and publication
marker before dispatching `Auto-forward PR` from trusted main.

The read-only phase:

1. verifies it was dispatched from trusted main;
2. reads PR object identity;
3. resolves H from the same-repository PR branch Git ref;
4. resolves current main B from its Git ref;
5. proves B is an ancestor of H;
6. verifies the three required current-H check contexts;
7. verifies GitHub does not report an unsatisfied review state.

The effect phase repeats the identity/ancestry/check/review observations before
minting write credentials.

A dedicated repository-scoped GitHub App then receives only Contents: write for
the final push. The effect is a normal non-force push of H to main, followed by
an exact main-ref readback.

## Configuration

Configure:

- repository Actions variable `AUTO_FORWARD_APP_CLIENT_ID`;
- repository Actions secret `AUTO_FORWARD_APP_PRIVATE_KEY`.

The App should be installed only on this repository with the minimum permission
needed to advance protected main. The main ruleset blocks updates and deletion
for everyone except this App. The App bypasses that ruleset so it can
fast-forward main directly.

Do not reuse a broader publication/release credential merely for convenience.

## Repository policy

The main ruleset has two rules: only the Auto-forward App can update `main`, and
only that App can delete `main`. Checks, review policy, and the non-force
fast-forward are workflow gates, not ruleset rules.

## Production acceptance

A repository-policy change is not complete until the installed Auto-forward App
has performed a real integration through the trusted workflow.

Use a small same-repository PR based on current main and require this sequence:

1. the PR's exact H completes all three required checks successfully;
2. for a trusted protocol-sync PR, confirm `Dispatch auto-forward` dispatches
   `Auto-forward PR` from trusted main; for other acceptance tests, dispatch it
   manually;
3. the read-only phase resolves current H and current main B and confirms
   `B ∈ ancestors(H)`;
4. the effect phase rechecks PR identity, H, B, required checks, and review
   policy before minting the App token;
5. the App performs one normal non-force `git push H:refs/heads/main`;
6. workflow readback proves `main == H`;
7. GitHub recognizes the PR as merged at that same H;
8. the resulting main push verification succeeds on that same commit.

Acceptance fails closed if the App configuration is missing, H changes, main is
no longer an ancestor of H, a required check is not successful, review policy
is unsatisfied, or the push is not fast-forward. The workflow must not repair
any of those states.

This production acceptance is also the proof that repository ruleset bypass
scope and the Actions App credentials are wired correctly. Configuration should
not be considered complete merely because the App is installed or the ruleset
looks correct in the UI.

## Scope

The first implementation supports same-repository PR heads. This covers the
repository's controlled development and protocol-sync branches and keeps one
Git-ref namespace authoritative.

Supporting fork PRs later would require an explicit trust and fetch model; it is
not implicit in this workflow.
