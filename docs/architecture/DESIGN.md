# llm-go design

Status: current

Destinations and why the three owners share one repository:
[Northstar.md](Northstar.md).

## Constraints

The repository must not:

- create a root facade, umbrella module, shared runtime module, or shared test
  utility package
- preserve old import paths through mirrors, forwarding packages, or re-exports
- claim atomic publication across independent Go modules

`main` is independently buildable. Every public module and every release
evidence path runs with `GOWORK=off`. Formal tags are independent SemVer
identities created only by protected CI. Compatibility is what a clean
consumer resolves from the public proxy.

## Layout

```text
github.com/ronhuafeng/llm-go
├── CONTEXT-MAP.md
├── docs/architecture/Northstar.md
├── docs/architecture/DESIGN.md      this file
├── docs/architecture/adr/           decisions that still constrain
├── docs/verification.md
├── docs/releasing.md
├── module-registry.json
├── go.work
├── llmkit/
├── codexsdk/
├── llmcaller/codex/
└── internal/tools/                  non-published
```

The root has no public `go.mod`. The committed workspace includes all four
registered modules.

## Owners

| Directory | Module path | Owns |
| --- | --- | --- |
| `llmkit` | `github.com/ronhuafeng/llm-go/llmkit` | Provider-neutral typed operations and toolkit evidence |
| `codexsdk` | `github.com/ronhuafeng/llm-go/codexsdk` | Exact Codex transport, generated protocol, and Exact Runs |
| `llmcaller/codex` | `github.com/ronhuafeng/llm-go/llmcaller/codex` | Codex policy and projection into toolkit evidence |
| `internal/tools` | non-published | Verification, release planning, black-box canaries |

```text
llmkit         ---                 +--> llmcaller/codex
codexsdk       ---/
internal/tools ----> all three public modules
llmkit         <-X-> codexsdk
public modules  -X-> internal/tools
```

A fourth runtime owner named `common`, `shared`, `core`, `types`, `testutil`,
or equivalent is prohibited.

Public package roots:

```text
github.com/ronhuafeng/llm-go/llmkit/...
github.com/ronhuafeng/llm-go/codexsdk
github.com/ronhuafeng/llm-go/codexsdk/protocolv2
github.com/ronhuafeng/llm-go/llmcaller/codex
```

## Fact owners

| Fact | Authority |
| --- | --- |
| Language | Module `CONTEXT.md` |
| Destination | `Northstar.md` |
| Current API | Exported code, package docs, module README, canonical inventory |
| Runtime semantics | Module-local public behavior tests |
| Generated facts | Owner-local inputs, manifests, committed output |
| Release impact intent | Module-local `.changes/` fragments |
| Release units | `module-registry.json` |
| CI and published evidence | `docs/verification.md`, `docs/releasing.md` |
| Still-binding why | Accepted ADRs in `docs/architecture/adr/` and module `docs/adr/` |

Root documents do not become API allowlists. Facts derivable from `go.mod`,
Git tags, or the public proxy are not copied into the module registry.

## Development

A pull request may touch multiple modules when each remains independently
buildable against already published requirements. If a downstream change needs
a new upstream API:

```text
upstream expand -> publish and proxy-verify
downstream migrate and update go.mod -> publish downstream
optional upstream contract/removal
```

Each module owns its minimum Go version. Generators remain with their semantic
modules; `repoctl` checks a clean diff and does not own generation policy.
Tests stay module-local unless they observe the black-box composed system.

Verification procedure: [docs/verification.md](../verification.md).
Tag identity and hosted release controls: [docs/releasing.md](../releasing.md).

A repository-level refactor does not authorize changes to module-owned
behavior. Package or lifecycle changes need their own contract, migration
guidance, and SemVer decision.
