# Upgrade llmcaller/codex

Current module: `github.com/ronhuafeng/llm-go/llmcaller/codex`.
Install the current tag with `go get github.com/ronhuafeng/llm-go/llmcaller/codex@v0.6.0`.

## From `llmcaller-codex-go` v0.4.2 to `llmcaller/codex` v0.5.0

```text
github.com/ronhuafeng/llmcaller-codex-go/llmcaller/codex
  -> github.com/ronhuafeng/llm-go/llmcaller/codex
```

Upstream imports:

| Legacy | Current |
| --- | --- |
| `github.com/ronhuafeng/llmkit-go/llmadapter` | `github.com/ronhuafeng/llm-go/llmkit/llmadapter` |
| `github.com/ronhuafeng/codexsdk-go/codexsdk` | `github.com/ronhuafeng/llm-go/codexsdk` |
| `github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2` | `github.com/ronhuafeng/llm-go/codexsdk/protocolv2` |

Do not add a `replace`, forwarding package, or re-export.

## From v0.5.0 to v0.6.0

v0.6 removes implicit Draft 7 detection for legacy tuple schemas. Dialect
selection depends only on the root `$schema` declaration:

- no `$schema` means Draft 2020-12
- `http://json-schema.org/draft-07/schema#` selects Draft 7
- `https://json-schema.org/draft/2020-12/schema` selects Draft 2020-12
- every other explicit identifier fails with `SchemaPolicyError.Kind ==
  "invalid_schema"`

An unversioned tuple `{"type":"array","items":[{"type":"string"}]}` must
declare Draft 7 or migrate to Draft 2020-12 `prefixItems`.
