# Three-Repository Refactor Index

Status: split into repository-local plans on 2026-07-11

This file is only the cross-repository index and release-order contract. It is
not an API specification. Each repository-local plan is authoritative for that
repository's ownership, handwritten API, behavior, migration, tests, and
acceptance.

## Repository Plans

- `llmkit-go`:
  [v0.2-refactor-plan.md](https://github.com/ronhuafeng/llmkit-go/blob/main/docs/v0.2-refactor-plan.md)
- `codexsdk-go`:
  [v0.2-refactor-plan.md](https://github.com/ronhuafeng/codexsdk-go/blob/main/docs/v0.2-refactor-plan.md)
- `llmcaller-codex-go`:
  [v0.2-refactor-plan.md](https://github.com/ronhuafeng/llmcaller-codex-go/blob/main/docs/v0.2-refactor-plan.md)

## Execution Prompts

- `llmkit-go`:
  [v0.2-refactor-execution-prompt.md](https://github.com/ronhuafeng/llmkit-go/blob/main/docs/v0.2-refactor-execution-prompt.md)
- `codexsdk-go`:
  [v0.2-refactor-execution-prompt.md](https://github.com/ronhuafeng/codexsdk-go/blob/main/docs/v0.2-refactor-execution-prompt.md)
- `llmcaller-codex-go`:
  [v0.2-refactor-execution-prompt.md](https://github.com/ronhuafeng/llmcaller-codex-go/blob/main/docs/v0.2-refactor-execution-prompt.md)

## Co-Design Invariants

> Each layer reduces the cognitive cost of correct use without reducing the
> expressive capability of the layer below it. Abstractions hide complexity,
> not facts.

- `llmkit-go` remains provider-neutral.
- `codexsdk-go` only provides Go interfaces for Codex interaction.
- `llmcaller-codex-go` is the only dependency join and owns Codex adapter policy.
- Generated Codex protocol declarations are facts, not handwritten API design.
- Simplified paths are projections of detailed paths.
- Partial results and exact provider facts remain accessible.
- Public helpers require unique semantics; syntax-only helpers remain examples.

## Contract Consistency

The caller plan mirrors two marked upstream contract blocks:

- `contract:llmkit-caller`
- `contract:codexsdk-caller`

Each mirror must be byte-identical to its authoritative upstream block before
implementation. A mirrored block describes a target contract; it is not proof
that the currently resolved module version implements it. The caller must also
compile against real resolved RC/stable tags before claiming compatibility.

No central API declarations are duplicated in this index.

## Release Order

1. Implement and validate `llmkit-go` and `codexsdk-go` independently.
2. Publish authorized RC/stable tags for both foundations.
3. Verify the caller resolves those real tags with `GOWORK=off` and no replace.
4. Implement and validate the caller join.
5. Publish the caller last.

A temporary all-heads `go.work` is useful integration evidence but is not
released-version evidence. Do not tag, push, open remote PRs, or publish releases
without explicit authorization.
