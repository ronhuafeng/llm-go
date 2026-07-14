# Migration status

The complete frozen `llmkit-go`, `codexsdk-go`, and `llmcaller-codex-go`
histories are now reachable from this repository through their recorded pure
relocation commits and independent merge edges. The toolkit source now declares
`github.com/ronhuafeng/llm-go/llmkit`; its first replacement tag is
`llmkit/v0.6.0`. Its linked GitHub Release and the public Go proxy are the live
verification sources; repository prose does not pre-announce a tag as
verified. The SDK source now declares
`github.com/ronhuafeng/llm-go/codexsdk`; its first replacement tag is
`codexsdk/v0.6.0`. The adapter still declares its legacy module identity until
its own migration ticket is complete.

The accepted sequencing and completion gates are defined by the
[target design](../architecture/DESIGN.md) and its
[migration Definition of Done](../architecture/adr/0023-require-complete-provenance-equivalence-and-published-evidence.md).

The toolkit's exact old-to-new import mapping is documented in the
[llmkit v0.6.0 migration guide](../../llmkit/docs/migration/v0.6.0.md).
Consumers must continue using the legacy release until the
[`llmkit/v0.6.0` GitHub Release](https://github.com/ronhuafeng/llm-go/releases/tag/llmkit%2Fv0.6.0)
is marked verified by the protected tag and public-proxy verification gates.

The SDK's flattened old-to-new import mapping is documented in the
[codexsdk v0.6.0 migration guide](../../codexsdk/docs/migration/v0.6.0.md).
Consumers must continue using `codexsdk-go@v0.5.1` until the protected SDK
release gate marks `codexsdk/v0.6.0` verified.

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
