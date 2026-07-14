---
status: accepted
---

# End legacy repositories with final migration patches

Each legacy repository publishes one final migration patch before its final
commit is imported into `llm-go`:

```text
github.com/ronhuafeng/llmkit-go             v0.4.2
github.com/ronhuafeng/codexsdk-go            v0.5.1
github.com/ronhuafeng/llmcaller-codex-go     v0.4.2
```

These final versions:

- put a repository-moved and maintenance-ended notice at the top of the README;
- add standard `Deprecated:` migration guidance to package documentation;
- record final support status in the changelog;
- map old imports to new imports and name the first new module version;
- state that existing immutable versions remain proxy-accessible but the legacy
  paths receive no further feature or security fixes.

The migration-specific change that prepares each version is documentation-only.
The version may also contain unreleased changes that were already merged before
the migration freeze. Those changes remain in the provenance baseline and must
be listed truthfully in the changelog and release evidence. Migration work does
not add new runtime changes, forwarding packages, re-exports, or compatibility
layers.

The cutover sequence is:

1. Freeze all three legacy repositories.
2. Merge the migration guidance and publish the final patch tags.
3. Use those final commits and tags as the relocation and provenance baselines.
4. Publish and verify the new modules in dependency order.
5. After all new paths pass proxy and clean-consumer verification, archive the
   legacy repositories as read-only and redirect or close unfinished issues.

Archival cannot precede verified availability of all three replacement modules.

## Consequences

- Existing consumers can discover the move through README, package
  documentation, changelog, and pkg.go.dev.
- Imported histories include their own final migration notice.
- Existing merged fixes are neither discarded nor mislabeled as documentation.
- There is a bounded and explicit end to dual-repository maintenance.
- Old versions remain reproducible without suggesting continuing support.
- The new repository becomes the sole source only after its replacement
  artifacts are proven available.

## Considered options

Archiving immediately without final releases was rejected because consumers of
the latest old module versions would lack package-level migration guidance.

Maintaining forwarding modules was rejected because it would preserve dual
release infrastructure and obscure the intentional import-path break.

Continuing security-only maintenance on old paths was rejected because it would
create an indefinite second source of truth and release obligation.
