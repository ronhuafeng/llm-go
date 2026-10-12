# One native upstream synchronization goal

Tracking: [#420](https://github.com/ronhuafeng/llm-go/issues/420).

Status: accepted CR-01–03 direction; implementation pending. The complete prompt
below is submitted for user review before formal goal execution. This document
is an implementation plan, not evidence of runtime behavior or a successful
protocol upgrade. Inspected baseline: `9769ba362a5e946c17a6eb78d8e2155e19bb4f28`.

## Smallest complete replacement

- **CR-01:** use the existing SDK to start one persistent thread and set one
  native goal. Codex owns its autonomous continuation. The same goal owns
  generation, implementation, verifier corrections, every check and repairs.
- **CR-02:** use one ordinary synchronization PR for implementation and
  contract-grounded verifier changes. Remove the separate verifier PR,
  proof/publication/merge-continuation chain and artifacts with no remaining
  consumers. Independent review and the four exact-head gates remain.
- **CR-03:** observe native goal/turn completion rather than parsing the entire
  `codex exec` output. Keep exec consumers that still serve real runtime tests.
  Complete failure diagnostics remain JSONL files; the same goal receives paths.

The thin runner installs handlers before GoalSet, then waits for terminal goal
state, no active turn, and an observed completed turn belonging to this goal invocation. Only `complete` is a
completion candidate. Blocked, paused, limited, cancellation, disconnect and
cleanup failures cannot imply acceptance. Keep a finite runtime deadline; do
not reproduce Codex's goal loop or add a replacement round counter.

The existing SDK has GoalSet/Get and goal notifications. It lacks a public
connection-termination observer; select the smallest supported observation path
while implementing and test silent disconnect, rather than silently waiting to
claim completion. No new public lifecycle API is assumed by this plan.

Official source establishes [automatic goal startup and continuation](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/codex-rs/ext/goal/src/runtime.rs#L425-L491),
[continuation on idle](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/codex-rs/ext/goal/src/extension.rs#L180-L190),
and the [SDK completion condition](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/sdk/python/src/openai_codex/_goal.py#L54-L86).
An ordinary exec prompt containing `/goal` does not invoke this API.

## Complete proposed goal prompt

`${BASE_SHA}` is injected by the trusted launcher. The launcher also owns the
exact target, isolated environment, `gpt-6.1-sol/high`, run-resolved official
latest CLI and finite deadline. The model does not select or override them.

> 使用仓库的 `.agents/skills/codexsdk-sync-upstream/SKILL.md`，将 llm-go 从
> `${BASE_SHA}` 完整升级到 `openai/codex` 的 `rust-v0.162.1`，精确提交
> `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d`。
>
> 独立完成该 skill 定义的全部工作和验收，包含生成、适配、必要的验证器修正、
> 全部验证及失败修复，直到同一最终候选通过完整验收。自行安排工作，
> 不等待外层分配下一步。
>
> 保留已有无关改动。遵守 skill 的来源、权限和发布边界；完成后简述改动与
> 实际验证结果。遇到需要用户决定的契约冲突或外部阻碍，给出具体证据并报告
> blocked，不把部分成功当作完成。

## Skill responsibilities and current verification entrypoints

The revised skill supplies the complete task contract and a single acceptance
entrypoint: `go run ./codexsdk/internal/cmd/protocolupgrade verify`. That thin
composition entrypoint is not implemented yet; this Draft must not be run as a
formal goal until it is. CI and the skill will share its checks, with each CI
context selecting its existing gate. The prompt does not duplicate that contract.
The skill no longer requires typed unresolved admission, external repair rounds
or a separate verifier PR. It is not a second scheduler.

The following are existing commands, not newly invented acceptance shortcuts:

- Source: `go mod tidy -diff`, `go vet ./...`, `go test -race ./...`.
  Required workflow syntax and actual sandbox checks remain owned by
  [PR verification](../.github/workflows/pr-verification.yml); remove only checks
  whose implementation consumers are actually removed by this replacement.
- Generated: from `codexsdk`, `go run ./internal/cmd/generatedcheck`; additionally
  reconstruct and compile the generated package in the isolated directory used
  by [generated verification](../.github/workflows/verify-generated.yml).
- Provenance: from `codexsdk`, `go run ./internal/cmd/protocolupgrade sync
  -repo-root "$REPO" -module-root .
  -upstream-repo https://github.com/openai/codex.git
  -force-compare -validation-only`.
- Live: `go run ./internal/tools/cmd/livecodex run`, using the existing provider
  environment and exact run-selected CLI. Preserve the current Live fixture's
  separate model configuration; do not substitute the upgrade Agent's model.

Provenance and CI clean-tree checks require an immutable final candidate
snapshot. The same goal must invoke complete verification on such a matching
snapshot; it must not skip checks because its proposal workspace is dirty, or
commit to the working branch merely to bypass that prerequisite. Reuse existing
snapshot/isolation mechanisms without adding a second workflow or evidence
registry. The final post-goal checks verify the same standards and exact bytes.

## Completion evidence

The issue is complete only after one implementation PR has independent review,
all four exact-head checks and protected linear integration, followed by a new
formal sync that integrates the exact `rust-v0.162.1` target through its own
protected PR and four checks. Record actual run/PR/base/head/target outcomes in
[#420](https://github.com/ronhuafeng/llm-go/issues/420); avoid a second status ledger.
A green refactor PR or a goal's self-reported completion is not that evidence.
