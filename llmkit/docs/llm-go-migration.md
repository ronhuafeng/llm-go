# Migrating from llmkit-go to llm-go

`github.com/ronhuafeng/llmkit-go@v0.5.0` is the final release from the legacy
module path. Toolkit development continues in the `llm-go` monorepo as the
independently versioned module `github.com/ronhuafeng/llm-go/llmkit`, beginning
with tag `llmkit/v0.6.0`.

## Module and import mappings

The module requirement moves as follows:

| Legacy module | Replacement module |
| --- | --- |
| `github.com/ronhuafeng/llmkit-go` | `github.com/ronhuafeng/llm-go/llmkit` |

The four public package imports move as follows:

| Legacy import | Replacement import |
| --- | --- |
| `github.com/ronhuafeng/llmkit-go/llmadapter` | `github.com/ronhuafeng/llm-go/llmkit/llmadapter` |
| `github.com/ronhuafeng/llmkit-go/llmschema` | `github.com/ronhuafeng/llm-go/llmkit/llmschema` |
| `github.com/ronhuafeng/llmkit-go/llmstep` | `github.com/ronhuafeng/llm-go/llmkit/llmstep` |
| `github.com/ronhuafeng/llmkit-go/settle` | `github.com/ronhuafeng/llm-go/llmkit/settle` |

Update imports and require the first toolkit release from the monorepo:

```sh
go get github.com/ronhuafeng/llm-go/llmkit@v0.6.0
```

The migration preserves the toolkit API and behavior shipped in legacy
`v0.5.0`, except that Go import paths change. It does not provide forwarding
packages, re-exports, a committed `replace`, or a `go.work` compatibility path.
The new tag is announced as a verified release only after its clean,
proxy-backed module gates pass.

Published legacy versions remain immutable and resolvable through the public
Go proxy, but this repository receives no feature or security maintenance after
cutover. It is archived only after the complete three-module migration and
real-tag compatibility audit succeeds.
