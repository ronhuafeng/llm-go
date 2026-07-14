# Migration status

The complete frozen `llmkit-go`, `codexsdk-go`, and `llmcaller-codex-go`
histories are now reachable from this repository through their recorded pure
relocation commits and independent merge edges. No replacement module tag is
available from this repository yet.

The accepted sequencing and completion gates are defined by the
[target design](../architecture/DESIGN.md) and its
[migration Definition of Done](../architecture/adr/0023-require-complete-provenance-equivalence-and-published-evidence.md).

Consumer-facing old-to-new import guidance will be published only when the
corresponding replacement module is ready and its planned tag is explicit.

## Imported-history issue audit

GitHub evaluates historical closing keywords when previously unrelated commits
become reachable from the default branch. A legacy commit that referred to an
issue number in its source repository can therefore close the same number in
this repository even though the tickets are unrelated.

This occurred when imported adapter commit
[`c4cdcb5`](https://github.com/ronhuafeng/llm-go/commit/c4cdcb5de99406013a98b072186b758981f0a834)
closed [`#5`](https://github.com/ronhuafeng/llm-go/issues/5). The issue-event
audit exposed the imported commit as the closure source, and the issue was
reopened after verifying that the toolkit destination and provenance entry did
not exist.

After every history-import merge, inspect issue events for closures attributed
to imported commits and reopen any ticket whose own completion evidence is
absent. Do not treat tracker state alone as proof that an import ticket or its
dependents are complete; verify the destination tree, provenance entry, and
merge ancestry first.

## Legacy tag namespace hygiene

Fetch a source tag into its custom provenance ref with `git fetch --no-tags`.
A custom destination ref alone does not disable Git's tag auto-follow behavior
and can copy colliding legacy `v*` refs into this repository. After each fetch,
verify that no unplanned root `v*` tag exists before any push. The source tag
object and commit remain reachable through the custom provenance ref and merge
ancestry; only the colliding tag ref is excluded.

The committed raw tag-object payloads under `docs/migration/tag-objects/` are
durable, offline evidence for the final annotated legacy tags. Provenance
verification recomputes each Git tag object ID from those exact bytes and
requires its `tag` and `object` headers to match the manifest's tag and source
commit. These evidence files do not create Git refs and therefore cannot
collide with the new repository's path-prefixed tag namespaces.
