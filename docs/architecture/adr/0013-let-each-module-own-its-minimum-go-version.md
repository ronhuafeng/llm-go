---
status: accepted
---

# Let each module own its minimum Go version

The first monorepo releases preserve the current `go 1.23.0` directive in all
three public modules. After migration, each module independently owns and
advances its minimum supported Go version as part of that module's compatibility
contract.

The repository enforces these derived constraints:

- a module raises its `go` directive only when its own implementation or
  contract requires the newer baseline;
- `llmcaller/codex` cannot declare a version lower than either direct public
  dependency requires;
- the root `go.work` version cannot be lower than any workspace module;
- pull-request minimum-version checks read each module's `go.mod` rather than a
  duplicated workflow or registry setting;
- `internal/tools` may use a newer Go version because it is not a published
  runtime dependency and is tested with its own declared toolchain contract.

A minimum-Go increase is reviewed and released for the affected module. It does
not force unrelated modules to change versions or publish empty releases.

## Consequences

- The initial migration does not combine import-path changes with a toolchain
  compatibility change.
- Public modules may acquire different minimum Go versions over time.
- CI selects minimum-version matrices from module facts rather than a global
  constant.
- Adapter compatibility checks include dependency Go-version constraints.
- The workspace remains usable by declaring at least the maximum module
  baseline.

## Considered options

Keeping every public module permanently on one repository-wide Go version was
rejected because one module's implementation choice would change unrelated
modules' consumer contracts.

Letting workflows define the minimum versions was rejected because `go.mod` is
the Go toolchain's authoritative module-level contract.
