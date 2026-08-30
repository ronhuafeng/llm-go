# Architecture decisions

Only decisions that still constrain current work live here. Completed import
and lineage events are git history, not this index.

| ID | Decision |
| --- | --- |
| [0001](0001-use-one-repository-with-multiple-go-modules.md) | One source repository, three public modules |
| [0002](0002-use-independent-semver-with-one-release-orchestrator.md) | Independent SemVer, one orchestrator |
| [0003](0003-use-an-orchestration-only-repository-root.md) | Orchestration-only root |
| [0005](0005-prohibit-shared-runtime-modules.md) | No shared runtime |
| [0006](0006-use-path-prefixed-tags-and-ordered-releases.md) | Path-prefixed tags, ordered publication |
| [0009](0009-keep-main-buildable-with-the-workspace-disabled.md) | `GOWORK=off` |
| [0010](0010-use-three-verification-stages.md) | PR cannot prove an unpublished tag |
| [0011](0011-put-ci-and-release-policy-in-a-typed-repository-tool.md) | Policy in `repoctl`, thin workflows |
| [0012](0012-use-a-minimal-module-registry.md) | Minimal module registry |
| [0013](0013-let-each-module-own-its-minimum-go-version.md) | Module-owned minimum Go |
| [0014](0014-keep-generators-with-their-semantic-owner.md) | Generators stay with the owner |
| [0015](0015-allow-cross-module-prs-without-workspace-only-dependencies.md) | Cross-module PRs, staged upstream publish |
| [0016](0016-separate-repository-governance-from-module-contract-docs.md) | Root governance vs module contract |
| [0017](0017-keep-test-fixtures-with-the-facts-they-prove.md) | Fixtures stay with the facts they prove |
| [0018](0018-keep-canonical-api-inventories-module-local.md) | Inventories are module-local |
| [0019](0019-create-production-tags-only-from-protected-ci.md) | Production tags only from protected CI |
| [0020](0020-use-module-local-structured-change-fragments.md) | Structured change fragments |
| [0024](0024-require-current-evidence-for-retention.md) | Retain only current evidence |

Module-local still-binding ADRs live under `codexsdk/docs/adr/`.
