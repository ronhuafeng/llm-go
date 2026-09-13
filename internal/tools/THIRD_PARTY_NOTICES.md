# Third-Party Notices

Last reviewed: 2026-07-14.

The repository tools are covered by the repository's [MIT License](../../LICENSE).
This file records dependencies of repository-local tools in the root module.

| Module | Version | License | Use |
| --- | --- | --- | --- |
| `golang.org/x/mod` | `v0.25.0` | BSD-3-Clause | Parses canonical `go.mod` syntax for dependency-boundary verification. |

The dependency list is derived from `go.mod`, `go.sum`, and the outputs of
`go list -m all` and `go mod graph`. Re-check it whenever the module graph
changes.
