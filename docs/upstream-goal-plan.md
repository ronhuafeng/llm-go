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
state, no active turn, and an observed completed turn. Only `complete` is a
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

> 使用仓库的 `.agents/skills/codexsdk-sync-upstream/SKILL.md`，将 llm-go 从基线
> `${BASE_SHA}` 完整同步到 openai/codex 的 `rust-v0.162.1`，精确提交
> `092d3acd6bec3e3a14bdc7e7a2810ab628ab759d`。
>
> 你负责完成整个目标：生成目标协议、适配实现、修正有上游契约依据的验证器假设、
> 运行全部验证，并根据失败自行修复，直到通过。自行决定工作顺序，
> 不等待外层流程分配下一轮。
>
> 完成标准是同一份最终候选通过仓库的四项验收：Root source verification；
> Codex generated reproducibility / Generated reproducibility；
> Codex protocol provenance；Live Codex integration / Live scenarios。
> 包括源码检查、生成物可复现与隔离编译、精确上游来源验证、真实 Codex 场景。
> 后续 CI 会复跑这些相同标准，不能留给 CI 才首次验证。
>
> 需要调整验证器时，以固定上游源码/schema 和已接受的 SDK 契约为依据，补上
> 合法行为通过、真实错误仍被拒绝的测试。不能通过删测试、缩小验证范围或放松
> 未知字段/类型等既有边界取得通过；契约确实冲突时明确报告需要决定的事项。
>
> 验证失败的完整诊断写入 JSONL 文件，检查命令只返回文件路径；你读取文件、
> 定位原因、继续修复。不把截断摘要当完整证据，不输出凭据或私有会话。
>
> 保留已有无关改动。生成物由正式生成命令产生。不要更改目标身份、发布权限、
> 工作流保护或验收门禁；不要提交、推送、发布或合入。只有最终候选已实际通过
> 全部标准才将 goal 标记 complete，并简述改动和验证结果；遇到凭据、环境或
> 必须由用户决定的契约阻碍，报告具体证据并标记 blocked，不冒充通过。

## Skill responsibilities and current verification entrypoints

The skill supplies current generation commands, contract sources and complete
verification instructions. It must stop requiring a typed unresolved admission,
external repair rounds or a separate verifier PR. It is not a second scheduler.

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
