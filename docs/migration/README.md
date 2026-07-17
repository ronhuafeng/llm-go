# Migration status

The complete frozen `llmkit-go`, `codexsdk-go`, and `llmcaller-codex-go`
histories are now reachable from this repository through their recorded pure
relocation commits and independent merge edges. The toolkit source now declares
`github.com/ronhuafeng/llm-go/llmkit`; its first replacement tag is
`llmkit/v0.6.0`. Its linked GitHub Release and the public Go proxy are the live
verification sources; repository prose does not pre-announce a tag as
verified. The SDK source now declares
`github.com/ronhuafeng/llm-go/codexsdk`; its first replacement tag is
`codexsdk/v0.6.0`. The adapter source now declares
`github.com/ronhuafeng/llm-go/llmcaller/codex`, is flattened at its module root,
and requires those two verified upstream modules exactly. Its first replacement
tag is `llmcaller/codex/v0.5.0`. All three replacement releases have passed the
protected release and public-Proxy gates. The three legacy repositories are
archived and read-only; this repository is now the sole active source, issue
tracker, CI owner, release owner, and private security-reporting intake.

The versioned [archival evidence](archive-evidence.json) binds the exact
pre-archive acceptance artifact, repository and issue disposition, security
intake handoff, disabled legacy automation, archived GitHub state, six fresh
public-Proxy resolutions, exact adapter tuple, typed three-layer consumer, and
post-archive acceptance artifact. Its repository contract is fail closed: a
missing category, altered digest, incomplete repository state, or lost exact
evidence makes repository verification fail.

The one-time Actions workflow that produced the cutover reports was retired
after both the pre-archive and post-archive acceptance runs completed. Later
`main` commits are governed by the normal PR and release verification paths and
do not create new migration acceptance artifacts. The typed
`repoctl migration-audit` command and its fail-closed report validators remain
available for historical verification and explicit forensic reruns.

The accepted sequencing and completion gates are defined by the
[target design](../architecture/DESIGN.md) and its
[migration Definition of Done](../architecture/adr/0023-require-complete-provenance-equivalence-and-published-evidence.md).

The toolkit's exact old-to-new import mapping is documented in the
[llmkit v0.6.0 migration guide](../../llmkit/docs/migration/v0.6.0.md). The
[`llmkit/v0.6.0` GitHub Release](https://github.com/ronhuafeng/llm-go/releases/tag/llmkit%2Fv0.6.0)
is the first verified replacement release.

The SDK's flattened old-to-new import mapping is documented in the
[codexsdk v0.6.0 migration guide](../../codexsdk/docs/migration/v0.6.0.md).
`codexsdk/v0.6.0` is the first verified replacement release.

The adapter's complete old-to-new import and dependency mapping is documented
in the [adapter v0.5.0 migration guide](../../llmcaller/codex/docs/migration/v0.5.0.md).
`llmcaller/codex/v0.5.0` is the first verified replacement release and resolves
the two upstream replacement versions exactly.

## Offline tag evidence

The committed raw tag-object payloads under `docs/migration/tag-objects/` are
durable, offline evidence for the final annotated legacy tags. Provenance
verification recomputes each Git tag object ID from those exact bytes and
requires its `tag` and `object` headers to match the manifest's tag and source
commit. These evidence files do not create Git refs and therefore cannot
collide with the new repository's path-prefixed tag namespaces.
