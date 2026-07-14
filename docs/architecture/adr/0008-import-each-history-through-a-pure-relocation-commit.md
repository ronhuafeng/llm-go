---
status: accepted
---

# Import each history through a pure relocation commit

The new repository preserves the complete reachable history of each source
repository. Each history is attached through an explicit relocation commit and
an independent merge node:

```text
source repository tip
        |
        v
pure directory relocation commit
        |\
        | \
        |  v
monorepo main --> no-ff merge node
```

The migration sequence is:

1. Create the `llm-go` repository bootstrap commit.
2. From the final `llmkit-go` source tip, create a commit that only relocates
   the complete tree into `/llmkit`; merge it with `--no-ff` and
   `--allow-unrelated-histories`.
3. Repeat independently for `codexsdk-go` into `/codexsdk`.
4. Repeat independently for `llmcaller-codex-go` into
   `/llmcaller/codex`.
5. After all histories are attached, migrate and publish the independent
   `llmkit` and `codexsdk` modules first.
6. After those new tags are proxy-visible, migrate the adapter module and its
   exact upstream requirements.
7. Validate the semantic freeze before each new path-prefixed tag.

The relocation commits must not change module declarations, imports, APIs,
runtime behavior, or file contents beyond the directory relocation. Git stores
snapshots rather than an intrinsic rename operation, so keeping these commits
pure provides the strongest practical rename detection for `git log --follow`,
blame, and audit.

The three imports use separate merge nodes rather than one octopus merge. This
makes each provenance edge independently inspectable and permits isolated
validation and recovery.

The source repositories are frozen while their final tips are imported. They
must not receive parallel feature changes during cutover.

The staged module-path cutover preserves the `GOWORK=off` invariant established
by ADR-0009.

A machine-readable provenance manifest records, for each imported module:

- source repository URL;
- final source commit;
- final source tag and its commit;
- destination directory;
- new module path;
- first new path-prefixed tag.

The legacy `vX.Y.Z` tag refs are not copied into the new repository because the
three source repositories contain colliding tag names. Their commit objects
remain reachable through the merge ancestry, and the provenance manifest
preserves the tag mapping.

## Consequences

- Original source commit IDs remain reachable from the monorepo history.
- The repository DAG truthfully represents when each independent history joined
  the new repository.
- Relocation and semantic migration have separate review surfaces.
- The one-time DAG contains unrelated roots and explicit merge commits.
- Migration tooling must verify that every relocation commit is tree-equivalent
  to its recorded source commit under the declared directory prefix.

## Considered options

Squashing or copying snapshots was rejected because it would make the archived
repositories the only source of line-level history.

Rewriting source history into subdirectories was rejected because it would
replace original source commit IDs with synthetic identities.

One octopus merge was rejected because it makes the three provenance joins less
independently reviewable.

Submodules were rejected because the new repository, not external repository
pointers, is the sole source of future development and publication.
