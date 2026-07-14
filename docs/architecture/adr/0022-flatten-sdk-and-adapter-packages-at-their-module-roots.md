---
status: accepted
---

# Flatten SDK and adapter packages at their module roots

After the pure history relocation commits, the module-path migration
mechanically lifts the SDK and adapter public packages to their new module
roots. This removes directory repetition created by placing old repository
layouts beneath new semantic module directories.

The public import mapping is:

```text
github.com/ronhuafeng/llmkit-go/llmadapter
  -> github.com/ronhuafeng/llm-go/llmkit/llmadapter

github.com/ronhuafeng/llmkit-go/llmschema
  -> github.com/ronhuafeng/llm-go/llmkit/llmschema

github.com/ronhuafeng/llmkit-go/llmstep
  -> github.com/ronhuafeng/llm-go/llmkit/llmstep

github.com/ronhuafeng/llmkit-go/settle
  -> github.com/ronhuafeng/llm-go/llmkit/settle

github.com/ronhuafeng/codexsdk-go/codexsdk
  -> github.com/ronhuafeng/llm-go/codexsdk

github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2
  -> github.com/ronhuafeng/llm-go/codexsdk/protocolv2

github.com/ronhuafeng/llmcaller-codex-go/llmcaller/codex
  -> github.com/ronhuafeng/llm-go/llmcaller/codex
```

Without flattening, the new public paths would contain accidental repetitions
such as `codexsdk/codexsdk` and
`llmcaller/codex/llmcaller/codex`. Those repetitions carry no domain meaning.

Flattening occurs in dedicated mechanical commits after the original source
trees have been imported unchanged. Exported identifiers, Go package names, and
runtime behavior remain unchanged. API inventory equivalence is checked after
applying the declared import-path mapping.

The toolkit retains its existing `llmadapter`, `llmschema`, `llmstep`, and
`settle` package names. Renaming those packages would be a separate public API
redesign and is excluded from the semantic-freeze migration.

## Consequences

- The SDK is naturally imported from its module root as `codexsdk`.
- The adapter is naturally imported from its module root as `codex`.
- Consumers avoid permanent path segments introduced only by old repository
  layout.
- Git history contains a second, explicit mechanical move after the pure
  provenance relocation.
- Migration documentation must publish a complete old-to-new import table.

## Considered options

Keeping the nested package directories was rejected because it would preserve
meaningless duplicated path segments in the permanent public API.

Renaming toolkit packages during the same migration was rejected because that
would combine structural migration with an unreviewed API redesign.

Adding root re-export packages was rejected because it would add public surface
and long-term forwarding obligations instead of correcting the layout directly.
