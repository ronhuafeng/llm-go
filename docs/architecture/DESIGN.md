# llm-go design

Status: current

## North Star

Every layer reduces the cognitive cost of correct use without reducing the
expressive power of the layer beneath it. Abstractions hide complexity, not
facts.

Shared source, review, CI, and release coordination do not collapse the three
semantic owners or their runtime APIs.

## Goals

- Keep `github.com/ronhuafeng/llm-go` as the only active source and release
  repository.
- Publish three independently versioned Go modules from one repository.
- Preserve the provider-neutral toolkit, exact Codex SDK, and Codex adapter as
  separate semantic owners.
- Make the adapter the only runtime dependency join.
- Coordinate releases through one typed, auditable orchestrator.
- Prove published compatibility through real, proxy-resolved module artifacts.

## Boundary constraints

- Creating a root facade, umbrella module, shared runtime module, or shared test
  utility package.
- Preserving old import paths through mirrors, forwarding packages, or
  re-exports.
- Claiming atomic publication across independent Go modules.

## Repository layout

```text
github.com/ronhuafeng/llm-go
├── .github/workflows/               thin GitHub platform wiring
├── docs/
│   └── architecture/                North Star, context map, ADRs
├── module-registry.json             minimal release-unit registry
├── go.work                          committed development workspace
├── go.work.sum
├── llmkit/                          public Go module
│   ├── go.mod
│   ├── README.md
│   ├── CHANGELOG.md
│   ├── .changes/
│   ├── llmadapter/
│   ├── llmschema/
│   ├── llmstep/
│   └── settle/
├── codexsdk/                        public Go module and root package
│   ├── go.mod
│   ├── README.md
│   ├── CHANGELOG.md
│   ├── .changes/
│   ├── protocolv2/
│   └── internal/cmd/protocolv2gen/
├── llmcaller/codex/                 public Go module and root package
│   ├── go.mod
│   ├── README.md
│   ├── CHANGELOG.md
│   └── .changes/
└── internal/tools/                  non-published workspace module
    ├── go.mod
    ├── cmd/repoctl/
    └── integration/                 genuine cross-module canaries
```

The repository root is orchestration-only and has no public `go.mod`. The
committed workspace includes all four registered modules. Public-module and
release evidence always runs with `GOWORK=off`.

## Public modules and ownership

| Directory | Module path | Stable owner |
| --- | --- | --- |
| `llmkit` | `github.com/ronhuafeng/llm-go/llmkit` | Provider-neutral typed schemas, decode, validation, retry, and toolkit evidence |
| `codexsdk` | `github.com/ronhuafeng/llm-go/codexsdk` | Codex transport, protocol, lifecycle, streaming, generated facts, and exact evidence |
| `llmcaller/codex` | `github.com/ronhuafeng/llm-go/llmcaller/codex` | Codex schema/safety policy and projection of exact facts into neutral evidence |
| `internal/tools` | non-published | Repository verification, release planning, and black-box integration orchestration |

```text
llmkit         ---\
                 +--> llmcaller/codex
codexsdk       ---/

internal/tools ----> all three public modules

llmkit         <-X-> codexsdk
public modules  -X-> internal/tools
```

A fourth runtime owner named `common`, `shared`, `core`, `types`, `testutil`, or
equivalent is prohibited. Similar implementation shapes do not establish shared
semantics.

## Public module identities

The current public package roots are:

```text
github.com/ronhuafeng/llm-go/llmkit/...
github.com/ronhuafeng/llm-go/codexsdk
github.com/ronhuafeng/llm-go/codexsdk/protocolv2
github.com/ronhuafeng/llm-go/llmcaller/codex
```

The SDK and adapter live at their module roots. Consumer migration mappings
remain in the module-owned migration guides rather than the repository design.

## Facts and their evidence owners

| Fact | Authoritative evidence |
| --- | --- |
| Current module identity and requirements | Module-local `go.mod` |
| Current exported API | Module-local canonical API inventory and exported code |
| Runtime semantics | Module-local public behavior tests |
| Generated facts | Owner-local generator inputs, manifests, and committed output |
| Release impact intent | Module-local structured change fragments |
| Release units | Minimal root module registry |
| Planned release | Generated digest-bound release-plan artifact |
| Published version and tuple | Immutable tags, public Proxy graph, checksums, and clean-consumer evidence |
| Architectural rationale | Accepted ADRs for decisions that still shape the repository |

Facts derivable from `go.mod`, Git tags, or the public proxy are not copied into
the module registry. Root architecture documents do not become API allowlists.

## Development contract

`main` is always independently buildable. Every public module passes with
`GOWORK=off` against already published requirements. The workspace provides a
second, current-checkout integration view but cannot repair a published module
graph.

A pull request may touch multiple modules when all affected modules remain
independently buildable. If a downstream change requires a new upstream API,
delivery uses linked stages:

```text
upstream expand
  -> publish and proxy-verify upstream
downstream migrate and update go.mod
  -> publish downstream
optional upstream contract/removal
```

Each module owns its minimum Go version. All three first releases preserve
`go 1.23.0`; later baseline changes are module-specific. The adapter and root
workspace must satisfy the derived dependency maximum.

Generators remain with their semantic modules and commit their output.
`repoctl` invokes generation checks and requires a clean diff; it does not own
protocol or runtime generation policy.

Tests remain module-local unless they genuinely observe the black-box composed
system. Only three-layer canaries, clean consumers, release fixtures, and proxy
artifact tests belong in `internal/tools`.

## Verification stages

### Pull request

- Compute the affected module closure.
- Run minimum-Go and current-Go module tests with `GOWORK=off`.
- Run race tests.
- Run relevant workspace composition canaries.
- Verify module graph boundaries, API inventories, generators, change
  fragments, documentation locations, and repository metadata.
- Run an ephemeral checkout-source consumer, labeled as source evidence.

### Release preflight

- Repeat the complete applicable PR suite.
- Verify tidy module files and module archive contents.
- Verify the requested path-prefixed tag and SemVer impact.
- Verify every adapter upstream requirement is already public-proxy resolvable.
- Produce a digest-bound plan containing commit, module, version, dependencies,
  order, and evidence inputs.

### Post-tag

- Use empty caches, `GOWORK=off`, and `GOVCS=*:off`.
- Resolve only the published module through the public Go proxy and checksum
  database.
- Compile and run an external clean consumer.
- For the adapter, verify the exact declared and resolved three-module tuple.
- Mark the GitHub Release verified only after success.

`internal/tools/cmd/repoctl` owns deterministic policy and machine-readable JSON
evidence. GitHub Actions owns triggers, permissions, protected environments,
network execution, and artifact publication. Workflows remain thin and are not
validated through custom YAML-structure policy parsers.

## Version and release contract

The modules use independent SemVer and directory-prefixed tags:

```text
llmkit/vX.Y.Z
codexsdk/vX.Y.Z
llmcaller/codex/vX.Y.Z
```

Formal tags are created only by protected CI from an approved, digest-bound
release plan. Publication is ordered and non-atomic. A failed immutable tag is
never moved. A public artifact, checksum, provenance, or behavior defect uses a
new version. A verifier or observation defect may only complete the same
artifact through an incident-specific, typed, forward-only recovery that
reuses its original authorization and cannot write tags.

Module-local structured change fragments express human-reviewed release impact.
API inventories provide a mechanical impact floor. Release tooling consumes the
fragments into module-local changelogs and release notes.

## API change boundary

Package renaming, new facades, lifecycle changes, or other semantic changes
require their own contracts, migration guidance, and SemVer decisions. A
repository-level refactor does not authorize changes to module-owned behavior.
