# Third-Party Notices

Last reviewed: 2026-07-11.

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
| `github.com/santhosh-tekuri/jsonschema/v6` | `v6.0.2` | MIT | Compiles generated schemas and returns typed validation trees used by `llmschema.Decode`. |

## Transitive module graph

| Module | Version | License | Use |
| --- | --- | --- | --- |
| `github.com/google/go-cmp` | `v0.7.0` | BSD-3-Clause | Module dependency of `github.com/google/jsonschema-go`; not imported directly by `llmkit-go` packages. |
| `github.com/dlclark/regexp2` | `v1.11.0` | MIT | Transitive regular-expression engine used by `github.com/santhosh-tekuri/jsonschema/v6`. |
| `golang.org/x/text` | `v0.14.0` | BSD-3-Clause | Transitive localization support used by `github.com/santhosh-tekuri/jsonschema/v6`. |
| `golang.org/x/mod` | `v0.8.0` | BSD-3-Clause | Module-graph-only dependency declared by `golang.org/x/text`; not needed by this module's packages. |
| `golang.org/x/sys` | `v0.5.0` | BSD-3-Clause | Module-graph-only dependency declared by `golang.org/x/text`; not needed by this module's packages. |
| `golang.org/x/tools` | `v0.6.0` | BSD-3-Clause | Module-graph-only dependency declared by `golang.org/x/text`; not needed by this module's packages. |

No additional third-party NOTICE file is currently required by these
dependencies. Re-check this file whenever `go.mod` or `go.sum` changes.
