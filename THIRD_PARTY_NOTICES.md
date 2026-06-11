# Third-Party Notices

Last reviewed: 2026-06-10.

This project is licensed under the MIT License. See [LICENSE](LICENSE).

The dependency list below is derived from `go.mod`, `go.sum`, and:

```sh
GOWORK=off go list -m all
GOWORK=off go mod graph
```

## Direct dependencies

| Module | Version | License | Use |
| --- | --- | --- | --- |
| `github.com/google/jsonschema-go` | `v0.4.3` | MIT | Projects Go output types into JSON Schema in `llmschema`. |

## Transitive module graph

| Module | Version | License | Use |
| --- | --- | --- | --- |
| `github.com/google/go-cmp` | `v0.7.0` | BSD-3-Clause | Module dependency of `github.com/google/jsonschema-go`; not imported directly by `llmkit-go` packages. |

No additional third-party NOTICE file is currently required by these
dependencies. Re-check this file whenever `go.mod` or `go.sum` changes.
