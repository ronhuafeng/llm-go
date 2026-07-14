# Third-Party Notices

Last reviewed: 2026-07-14.

The repository tools are covered by the repository's [MIT License](../../LICENSE).
This file records dependencies of the non-published tools module.

| Module | Version | License | Use |
| --- | --- | --- | --- |
| `golang.org/x/mod` | `v0.25.0` | BSD-3-Clause | Parses canonical `go.mod` syntax for dependency-boundary verification. |

The dependency list is derived from `go.mod`, `go.sum`, and the outputs of
`GOWORK=off go list -m all` and `GOWORK=off go mod graph`. Re-check it whenever
the tools module graph changes.
