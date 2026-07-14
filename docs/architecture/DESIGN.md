# llm-go target design

Status: bootstrap repository created; module migration not started

## North Star

Every layer reduces the cognitive cost of correct use without reducing the
expressive power of the layer beneath it. Abstractions hide complexity, not
facts.

The repository move changes source, review, CI, and release coordination. It
does not collapse the three semantic owners or redesign their runtime APIs.

## Goals

- Make `github.com/ronhuafeng/llm-go` the only active source and release
  repository.
- Publish three independently versioned Go modules from one repository.
- Preserve the provider-neutral toolkit, exact Codex SDK, and Codex adapter as
  separate semantic owners.
- Make the adapter the only runtime dependency join.
- Replace cross-repository release coordination with one typed, auditable
  orchestrator.
- Preserve complete Git provenance and public behavior during migration.
- Prove final compatibility through real, proxy-resolved module artifacts.

## Non-goals for the first release

- Redesigning exported APIs, lifecycle contracts, evidence models, schema
  policy, retry policy, or safety profiles.
- Renaming toolkit packages.
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
│   ├── architecture/                North Star, context map, ADRs
│   └── migration/                   old-repository cutover guide
├── module-registry.json             minimal release-unit registry
├── migration-provenance.json        immutable source-history evidence
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
    └── cmd/repoctl/
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

## Public import migration

The SDK and adapter are flattened to their module roots after their original
repository trees have been imported unchanged:

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

Exported identifiers, package names, generated facts, and runtime behavior stay
unchanged. Toolkit package renaming is deferred to a separately reviewed API
redesign.

## Facts and their evidence owners

| Fact | Authoritative evidence |
| --- | --- |
| Current module identity and requirements | Module-local `go.mod` |
| Current exported API | Module-local canonical API inventory and exported code |
| Runtime semantics | Module-local public behavior tests |
| Generated facts | Owner-local generator inputs, manifests, and committed output |
| Release impact intent | Module-local structured change fragments |
| Release units | Minimal root module registry |
| Source migration provenance | Immutable migration provenance manifest and Git DAG |
| Planned release | Generated digest-bound release-plan artifact |
| Published version and tuple | Immutable tags, public Proxy graph, checksums, and clean-consumer evidence |
| Historical rationale | Non-normative proposal or ADR history |

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

The first new releases are:

```text
llmkit/v0.5.0
codexsdk/v0.6.0
llmcaller/codex/v0.5.0
```

Formal tags are created only by protected CI from an approved, digest-bound
release plan. Publication is ordered and non-atomic. A failed immutable tag is
never moved; recovery uses a new version.

Module-local structured change fragments express human-reviewed release impact.
API inventories provide a mechanical impact floor. Release tooling consumes the
fragments into module-local changelogs and release notes.

## History migration and cutover

The final legacy migration versions are:

```text
llmkit-go@v0.4.2
codexsdk-go@v0.5.1
llmcaller-codex-go@v0.4.2
```

Each final source tip receives a pure directory-relocation child commit. Each
relocated history is joined to the monorepo by its own no-fast-forward merge
node. Original source commit IDs remain reachable; old colliding tag refs are
recorded in provenance rather than copied.

```text
legacy final tip -> pure relocation --\
                                      +--> independent merge node -> main
monorepo bootstrap ------------------/
```

The execution order is:

1. Bootstrap the new repository.
2. Freeze the legacy repositories.
3. Publish their final migration patches; each migration-specific release
   commit is documentation-only, while pre-freeze unreleased changes remain
   visible in changelog and provenance evidence.
4. Import each complete history through its pure relocation commit and merge
   node.
5. Establish root governance, the workspace, registry, provenance, and
   repository tooling.
6. Migrate, independently verify, publish, and proxy-verify `llmkit` and
   `codexsdk`.
7. Flatten and migrate the adapter only after both new upstream tags are
   available; commit their exact requirements.
8. Publish and proxy-verify the adapter and complete three-layer consumer.
9. Archive the legacy repositories only after every completion gate passes.

The migration is complete only under the full provenance, equivalence,
architecture, independent-consumption, published-chain, and cutover evidence in
[ADR-0023](./adr/0023-require-complete-provenance-equivalence-and-published-evidence.md).

## Follow-up boundary

After the three new modules are published and verified, API redesigns may be
proposed independently. Package renaming, new facades, lifecycle changes, or
other semantic improvements require their own contracts, migration guidance,
and SemVer decisions. The monorepo migration is not authorization to make them.
