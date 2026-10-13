# One native upstream synchronization goal

Tracking: [#420](https://github.com/ronhuafeng/llm-go/issues/420).

Status: CR-01–03 deployed through [#421](https://github.com/ronhuafeng/llm-go/pull/421)
at `30d86bea257da874165b6df1d0de7ba32a2b604f`, with independent review and four
exact-head checks passed. The target upgrade still requires a real authorized
goal and its own accepted Draft PR. This document defines that acceptance; it
is not evidence that the target upgrade has completed.

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

The runner uses the existing GoalSet/Get API and goal notifications. A bounded
read-only GoalGet probe detects silent disconnect; its response cannot establish
completion. No additional public lifecycle API is required.

Official source establishes [automatic goal startup and continuation](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/codex-rs/ext/goal/src/runtime.rs#L425-L491),
[continuation on idle](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/codex-rs/ext/goal/src/extension.rs#L180-L190),
and the [SDK completion condition](https://github.com/openai/codex/blob/092d3acd6bec3e3a14bdc7e7a2810ab628ab759d/sdk/python/src/openai_codex/_goal.py#L54-L86).
An ordinary exec prompt containing `/goal` does not invoke this API.

## Complete goal prompt for review

`${BASE_SHA}`, `${PR_URL}` and `${BRANCH}` are injected by the trusted launcher.
It also owns the exact target, isolated environment, `gpt-6.1-sol/high`,
run-resolved official latest CLI and finite deadline. The model cannot choose
another repository, branch or PR. PR #421 deployed this workflow implementation;
the target upgrade belongs in a separately prepared, fixed synchronization Draft
PR launched from accepted main.

> 使用仓库的 `.agents/skills/codexsdk-sync-upstream/SKILL.md`，将 llm-go 从
> `${BASE_SHA}` 完整升级到 openai/codex 的 rust-v0.162.1，精确提交
> 092d3acd6bec3e3a14bdc7e7a2810ab628ab759d。
>
> 你负责生成、适配、必要的契约依据验证器修正、全部验证及失败修复。
> 自行安排工作，直到同一最终候选达到 skill 的完整通过标准。
>
> 调用提供的 `verify_upstream` 工具完成验收：它先执行无密钥本地检查，
> 通过后只能正常更新已绑定的 Draft PR `${PR_URL}` / 分支 `${BRANCH}`，
> 并等待该精确 head 的现有 CI（包括真实 Live）。读取工具返回的 JSONL
> 诊断文件，在这个 goal 内继续修复。不得绕过该入口自行操作 GitHub。
>
> 保留无关改动，不更换目标或验收标准。只有本地检查及该最终 head 的
> 四门 CI 都真实通过，才标记 complete 并简述改动、验证和 PR。
> 不得合入；PR 保持 Draft。外部阻碍或必须由用户决定的契约冲突，
> 报告具体证据并标记 blocked。

## Skill responsibilities and current verification entrypoints

The goal invokes one fixed, no-argument `verify_upstream` tool. Its local check
composition reuses existing checks on an immutable, credential-free candidate
snapshot. Only after local success may the trusted handler append to the bound
Draft PR branch, then wait for exact-head CI including existing real Live. CI
failure diagnostics return by JSONL path to the same goal. No outer repair turn,
verifier role or merge continuation is added. Normal cancellation/deadline bounds
the outstanding tool call.

The implementation has passed exact-head verification and independent review.
Formal execution requires operator review of the complete prompt and authorization
of the fixed-target run. Local-only verification explicitly reports that limited
scope; it cannot stand in for the four-gate result. Existing
CI Live retains its current credential/trust arrangement. Candidate code in that
job already has provider access; this plan does not claim otherwise or introduce
a new host credential broker.

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

## Completion evidence and current execution ceiling

Record implementation evidence and target acceptance separately in
[#420](https://github.com/ronhuafeng/llm-go/issues/420):

1. PR #421 has independent review, lifecycle/security regressions and four
   exact-head CI results, and is deployed on main. Fixture success establishes
   only the tested behavior.
2. After prompt approval, one real goal upgrades the fixed target in its own
   bound Draft PR, observes a real failure and correction where applicable,
   and reaches the four actual checks on the final exact head.

The target synchronization PR remains **Draft and unmerged**. Workflow repairs
follow separately authorized exact-head Auto-forward integration. A green
implementation PR or a goal's self-report is not evidence that the target
upgrade succeeded.

## Implementation layout

The native AppServer adapter owns one goal lifecycle and one fixed tool. The
shared gate implementation freezes candidate bytes, preserves complete JSONL
diagnostics, checks accepted-base behavior plus exact upstream contracts, and
uses the same gate commands in CI. The fixed Draft publisher reuses existing
source identity and exact-head acceptance checks; it performs ordinary
branch pushes only. No verifier PR, role switch, checkpoint or merge continuation
remains. Existing non-goal `exec` consumers retain their adapter.

The AppServer process and descendants run under the existing Linux bubblewrap
boundary with an empty root, fixed public runtime paths, private process/home/tmp
state, candidate-write and diagnostics-read-only mounts. The controller checkout,
GitHub authentication and host file sockets are not mounted. Only the trusted
parent performs the fixed publication operations. The supported proof target is
the temporary Ubuntu GitHub-hosted VM; other platforms are not claimed. CI Live
continues using its existing trust arrangement, without a new credential service.
