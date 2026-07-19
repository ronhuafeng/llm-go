---
name: legacy-prune
description: 仅在用户显式调用 $legacy-prune 时，审查并清理明确范围内缺少当前保留证据的遗留实现及其完整闭包。
---

# Legacy Prune

## 调用与范围

仅在用户显式调用 `$legacy-prune` 时执行。把用户点名的 capability、目录、模块、
文件或提交差异固定为本次范围；范围缺失或会产生实质歧义时先询问，不默认审计整个
仓库。调用关系或契约证据可以指向范围外消费者，但这只影响候选判定，不扩大修改授权。

根据用户请求选择一个动作，读取对应文档，然后严格按该文档执行：

- 检查、审计、识别或报告 legacy 候选，但不修改代码：读取
  `commands/review.md`。
- 修复或删除当前对话中已经确认的 findings：读取
  `commands/apply.md`。

示例请求：

- `使用 $legacy-prune review 审查 <范围> 中的 legacy 实现。`
- `使用 $legacy-prune apply 修复本对话中已确认的 LP-01 和 LP-02。`

用户要求修改但尚未确认 findings 时，先在同一固定范围执行只读 review。用户已经
明确要求应用已确认 findings 时，直接进入 apply，不重新发起全仓审查。

## 共享证据政策

让保留承担举证责任，让删除承担安全责任。对每个候选识别其完整 owned closure，
包括只为它存在的 implementation、validation、automation、generated material、
configuration、documentation、operational state 和 dependent artifacts。

只保留被下列至少一项正向要求的最小切片：

- 当前可达消费者；
- 权威且当前的人类政策或用户路径；
- 上游或外部契约；
- 公开兼容义务；
- 法律、安全或运维要求；
- 已记录且仍适用的不变量。

历史理由、假设消费者、镜像实现的测试，以及只相互验证或引用的闭环不构成保留
证据。描述历史的材料如果仍是当前 interface、policy、compatibility aid、legal
record 或 required decision record，则按当前 owner 和用途判定，而不是按年代判定。

删除前按风险证明 observable behavior、external contracts、owned facts、security
和 operational invariants、domain ownership 及剩余设计仍正确且内部一致。删除完整
的无支持闭包，同时保留具有独立证据的切片。若删除 abstraction 只会把复杂度推给
消费者、复制 owner 或丢失事实，则保留或深化它。

只让有证据的不确定性阻塞受影响的删除，并明确缺少的证据；假设性不确定性不支持
保留。

## 软件仓库 profile

- 仓内调用数不是公开 library interface 的唯一消费者证据；核对公开契约、API
  inventory、迁移与支持义务。
- 当前上游 schema、protocol 或 generator input 可以要求保留未被仓库自身调用的
  generated surface。
- 测试只有在预期来自当前公开契约、上游事实、安全或运维不变量、现实失败模式时才
  构成独立证据；只证明被测实现存在的测试随闭包删除。
- 验证通常覆盖受影响模块测试、静态分析、生成结果可复现性、仓库边界检查、集成
  测试、残留引用检索，以及相关 CI、发布或外部状态。

检索优先覆盖 Git tracked files 和固定范围内的工作树修改，包括相关未跟踪文件。
默认排除 Git ignored 缓存、构建产物、依赖目录和范围外的未跟踪目录；只有调用关系、
构建入口或契约证据指向这些位置时才扩展检索。

区分 proposed、local、committed、published、deployed 和 externally verified 完成层级。
保留并行用户工作；除非用户另行明确授权，不 commit、push、创建 PR、部署或修改远端、
生产环境状态。
