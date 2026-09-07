# Turn 中断恢复与重复探索：问题分析及改进方案

本文记录 Turn 中断后重复采样、搜索和读取的原因，以及建议的修复路径。

- 分析日期：2026-09-07。
- 代码基线：`41c619dc`。
- 状态：三个阶段均已实施。第一阶段：工具阶段接受取消时统一经过上下文
  结算（已完成工具结果随 SessionDelta 进入终态提交），失败 Turn 保留闭合
  对话片段，Continue 历史投影保留来源 Turn 交换、仅去重恢复信封（Retry
  维持整删语义）。第二阶段：Turn 内续接快照在采样接纳与工具结果接纳等
  语义边界持久化——内容先写入既有 Context CAS，Kernel 以
  `continuation_recorded` 事实原子提交游标；进程重启后按“恢复执行事实 →
  恢复对话与证据 → 校验环境（工作区/Profile/模型漂移即拒绝并说明）→
  处理未完成 Effect → 采样”的固定顺序重建执行中的对话。第三阶段：读取
  记录升级为路径、读取范围（起止行）、内容版本（工作区读取 SHA256）、
  结果引用与调用身份；内容未变且窗口覆盖时准入直接重放原结果，文件变
  化、窗口不足或引用失效时放行必要读取并把失效原因写入结果
  metadata；跨 Turn 复用仅接纳带工作区读取摘要事实的 file_read；
  `partialOutput` 接入恢复信封（capsule v3）；`turn_history` 在终态
  Delta 缺失时回退到续接快照，覆盖崩溃中断 Turn 的来源链。
- 证据范围：源码、已有聚焦测试及两个临时确定性复现实验。未读取具体用户会话，
  因而不能把某一次实际重复探索直接归因于某个分支。

## 结论

QCode 当前存在恢复设计上的缺口，也有明确的实现问题：执行生命周期和工具状态的
恢复，没有始终伴随模型可见上下文与任务进度的恢复。

模型可能知道“这个文件读过、这个工具执行成功”，但不知道“读到了什么、排除了
什么、下一步应该处理什么”。模型因此重新生成搜索、读取或分析调用，即使 Runtime
没有重新派发原来的已完成 Effect，用户仍然会感到工作被重复执行。

恢复正确性需要同时满足两个条件：

1. 执行侧知道哪些工作已经完成，避免重复派发同一个已完成 Effect。
2. 模型侧拿到已完成工作的结果和证据，避免重新生成等价的探索调用。

现有实现对第一项已有较多保障，第二项尚未形成完整的持久化与恢复契约。

## 三种不同的恢复场景

| 场景 | 当前行为 | 分析重点 |
| --- | --- | --- |
| 等待审批或用户输入后继续 | 解除当前 Turn 的等待 | 保留当前执行链路 |
| 用户中断后点击 Continue | 创建新 Turn，携带来源 Turn 的恢复信息 | 来源知识能否完整传递 |
| 进程重启后恢复未完成 Turn | 恢复原 Turn 的 Kernel 和 Pending Effect，再进入 Engine | 正在执行的对话能否重建 |

Continue 使用新 Turn ID 本身不是缺陷。旧 Turn 已经终结，新 Turn 可以引用旧 Turn
的续接状态；关键是引用的状态足以支撑后续工作。普通新消息也不能直接等同于带
`TurnRecoveryContext` 的 Continue，二者的历史投影路径不同。

相关入口见 [恢复操作准备](../../internal/persist/artifact/service.go)、
[启动时恢复 Pending Turn](../../internal/runtime/app/turn_recovery.go) 和
[Kernel 构造与恢复](../../internal/runtime/agent/turnkernel/runtime_kernel.go)。

## 已确认的问题

### 1. Continue 删除来源 Turn 的完整对话

`RecoveryBaseHistory()` 收到 Recovery 后，按来源 Turn 身份移除其全部消息。
该函数没有区分 Continue 与 Retry，也没有区分重复恢复指令和有效工具证据。

`TestRecoveryHistoryReplacesSourceTurnByIdentity` 明确要求移除来源 Turn 内完整的
`file_read → result`。这是当前设计行为，不是偶发的序列化遗漏。

来源 Turn 被替换为 `recovery_evidence`。它主要包含工具名、调用 ID、路径、参数
哈希、输出哈希、状态和输出首行的短片段，没有完整工具结果或完整分析结论。
`PrepareTurnRecovery()` 收集了 `partialOutput`，但当前 Continue Prompt 构造没有
使用它。`RenderRecoveryEvidence()` 超过现有预算时，还会从数组尾部删减工具和
Outcome；最新结果可能先被裁掉。

因此，“知道曾经读过”不能替代“拥有读到的知识”。哈希有助于校验身份，但不能
让模型从中恢复源码或搜索结果。

源码与测试：

- [历史投影](../../internal/runtime/agent/context/history.go)：`RecoveryBaseHistory`。
- [历史恢复测试](../../internal/runtime/agent/engine/recovery_history_test.go)。
- [恢复摘要构造](../../internal/persist/artifact/service.go)：
  `PrepareTurnRecovery`、`RenderRecoveryEvidence`、`recoveryOutcomeReason`。

### 2. 工具阶段接受取消时，可能跳过整个上下文收尾

`Scope.Run()` 中的 `finishAcceptedCancellation()` 直接设置
`contextFinalized = true`，然后完成 Kernel 终态并发送 Canceled，没有调用
`finalizeTerminalContext()`。这同时阻止了后续 Deferred Context Finalization。

主循环在把工具结果追加到 `transaction` 之前检查该取消分支。因此，工具结果即使
已经完成并向 UI 发布，也可能没有进入可供下一次使用的会话历史。

普通模型调用取消路径有保留闭合历史的逻辑；工具阶段的这条路径却跳过了它。
这解释了为什么恢复体验可能随中断时机变化。

一般失败路径也存在信息损失：`finalizeTerminalContext()` 刻意排除失败 Turn 的
Transaction，仅向旧历史追加失败说明。一个 Turn 探索多轮后才失败，前面已经成功
的工具结果也可能退出默认模型上下文。需要清理未闭合的工具消息，不意味着必须
丢弃此前全部闭合对话。

源码：

- [Turn 主循环](../../internal/runtime/agent/engine/turn_handler.go)：
  `finishAcceptedCancellation` 及工具结果回填顺序。
- [终态上下文处理](../../internal/runtime/agent/engine/terminal_handler.go)：
  `finalizeTerminalContext`。
- [SessionDelta 应用](../../internal/runtime/agent/engine/session_delta.go)。

### 3. 进程恢复重建了 Kernel，却没有完整重建执行中的对话

`transaction` 是 `Scope.Run()` 内的局部变量。恢复时，Engine 从已有会话历史
重新开始，追加用户输入，再补入 Pending 工具调用。此前已经关闭的工具调用不会
从该路径重新加入。

`PendingToolCalls()` 只读取待执行 Effect 和 `OpenCalls`。
`ModelSampleRequested` 只有 `SampleID`，没有当次实际模型请求的消息快照。
因此，Kernel 中存在已完成的 Sample/Tool 事实，并不意味着恢复后的 Provider
Request 具备此前的对话。

现有 `ResponseAssembly` 能保留当前采样的已确认片段，并支持不完整输出续接。
但单次采样的输出装配不能替代整个 Turn 此前的工具对话和上下文。

源码：

- [Turn 执行入口](../../internal/runtime/agent/engine/turn_handler.go)。
- [Pending 工具恢复](../../internal/runtime/agent/turnkernel/runtime_kernel.go)。
- [采样命令](../../internal/runtime/agent/turnkernel/command.go)：`ModelSampleRequested`。
- [模型请求重建](../../internal/runtime/agent/engine/model_handler.go)。
- [流式恢复测试](../../internal/runtime/agent/engine/provider_stream_recovery_test.go)。

### 4. 防重复探索机制不能补回丢失的信息

QCode 已有 Resume Hint、Working Set、WorkItem、工具准入限制和结果缓存，但这些
能力尚未形成可靠的知识续接链路：

- Continue 种入的已读信息主要是路径，读取窗口统一设为 `full`，没有恢复精确
  范围和内容版本。
- `workItemWindowIsNew()` 对 `full` 接受任意正数 `start_line`，不能准确判断
  该窗口是否已经读取过。
- 已知路径重读可能被拒绝，要求改用 `turn_history` 或 `result_get`；但
  `turn_history` 的归档读取依赖终态中的 SessionDelta，缺失的 Delta 无法通过
  该路径补回。不能假设提示中的历史入口总能取回所需内容。
- 工具结果缓存在每次 `Scope.Run()` 重新创建，已有 `RepeatReplaySameTurn`
  契约不能自然覆盖跨 Turn 的 Continue。

这些条件可能形成额外循环：缺少内容 → 重读被拒绝 → 尝试回读历史 → 历史不完整
→ 再次采样寻找其他办法。这是由机制推导出的风险，尚未用真实会话测量其发生比例。

源码：

- [WorkItem 恢复](../../internal/runtime/agent/engine/work_item.go)。
- [探索工具准入](../../internal/runtime/agent/engine/observation_gate.go)。
- [恢复提示](../../internal/runtime/agent/context/resume.go)。
- [历史归档](../../internal/runtime/app/turn_archive.go)。
- [工具结果缓存](../../internal/adapter/tool/result_cache.go)。

## 验证记录及局限

以下已有聚焦测试于分析时通过。`artifact` 包在该筛选下没有匹配的测试，不能
视为该包全部行为已被验证。

```bash
go test ./internal/runtime/agent/engine \
  ./internal/runtime/agent/context \
  ./internal/runtime/agent/turnkernel \
  ./internal/runtime/app \
  ./internal/persist/artifact \
  -run 'Test.*(Recovery|RecoverTurn|Cancel|Resume|Checkpoint|R3|ContinueWorkItem)' \
  -count=1
```

分析另使用仓库已有 Fixture 和 Test Helper 运行了两个临时测试。测试以期望的
上下文保留行为作断言，均揭示了缺口；临时测试没有作为正式回归测试保留在代码树中。
下面保留重建实验所需的场景和观测，不依赖本机临时文件。

| 实验 | 构造方式 | 观测 |
| --- | --- | --- |
| 工具结果发布时取消 | Scripted Provider 调用 Echo 工具，在成功结果发布回调中请求 `user_interrupted` | 工具执行 1 次，终态 Canceled；History、Checkpoint 数量及 Session Revision 均为 0，没有 Staged SessionDelta |
| 恢复第二次采样 | 用 MemoryTerminalEnvelopeStore/StoreCoordinatorRuntime 接纳第一轮采样和工具完成事实，启动第二轮采样后释放 Coordinator，再创建新 Engine 恢复同一 Turn | 恢复后的首次 Provider Request 缺少此前的工具调用及结果对话 |

第二项是持久化 Kernel 状态重建实验，不是操作系统级杀进程实验。其 Fixture
后续还触发了 Repair Budget 阻塞终态；这里的判断仅依据首次恢复请求中缺少此前
工具对话，不将其视为一次完整成功的端到端恢复。

已有测试主要覆盖来源身份、终态、Effect 重排和部分流式续接。它们通过不能证明
“恢复后模型实际看到了全部必要的已完成工作”。

## 改进目标与不变量

目标是从最近一个持久化安全边界继续，并让模型拿到该边界前已获得的必要知识和
未完成工作。尚未完成的采样可能需要重新发起；已完成探索因上下文丢失而重做，
则应作为恢复缺陷处理。

改进必须遵循现有所有权与安全边界：

1. Turn 续接业务属于 `internal/runtime/agent`；Host 只提交操作和展示投影。
2. 对话快照引用与执行事实保持一致，不引入独立于 Kernel 的第二套执行状态机。
3. SessionDelta 仍在 Durable Commit 后应用，不为恢复体验绕过原子提交。
4. 取消不再触发模型总结。上下文结算通过确定性记录和既有持久化边界完成。
5. 恢复快照不授予工具执行权限。副作用继续经过 Guard、Policy、Approval、
   Constitution、Journal 和 Sandbox。
6. 不确定的外部副作用先 Reconcile，不能因恢复记录存在就自动重放。
7. 模型可见上下文保持有界；容量来自模型能力、现有公开配置和实际剩余预算，
   不新增隐藏固定阈值。完整 Transcript 与模型投影分开存储和管理。

## 分阶段实施方案

### 第一阶段：修复取消收尾和 Continue 历史投影

取消与失败统一经过上下文结算：先收集已完成的工具结果，构造合法闭合的对话
片段，再生成 SessionDelta，随 Terminal Envelope 提交。

并行批次应按调用分别保留完成结果，未完成调用单独记录状态。既不能向模型提供
悬空 Tool Pair，也不能因为批次尚未全部结束而丢弃已经完成的证据。

Continue 保留来源 Turn 的有效探索证据和已确认结论，仅去重恢复指令包装。
Retry 使用单独定义的重试语义。避免恢复指令无限嵌套，不需要删除整个来源 Turn。

“保留”首先指 Durable Transcript 不丢失；模型请求在公开预算内保留最近必要
因果链，其余内容通过可解析引用检索，不要求无限扩大可见历史。

### 第二阶段：持久化 Turn 内的续接快照

在既有 Context Manifest/CAS、Domain Facts 上扩展续接记录。建议的快照内容包括：

| 内容 | 用途 |
| --- | --- |
| 安全边界、事实序号、消息游标 | 确定恢复位置与一致性 |
| 已完成 Assistant/Tool Exchange 的内容引用 | 恢复必要因果链 |
| 当前采样的请求上下文引用、摘要及 ResponseAssembly | 对齐请求输入与已确认输出 |
| Pending、Completed、Outcome Unknown 工具状态的事实引用 | 选择恢复、复用或 Reconcile |
| Plan、已确认发现、待解决问题和证据来源 | 恢复任务进度，避免从散文猜测待办 |
| Workspace、Profile/Model、工具目录身份 | 检查环境与授权绑定是否变化 |

在完整采样接纳、工具结果接纳等语义边界持久化，不只依赖 Turn 最后的收尾。
结果内容先可靠存储，再原子提交引用它的事实和游标；UI 从已接纳事实派生。
已经存储但尚未被引用的 Blob 可按既有存储治理规则处理，不能出现事实先声称
完成、恢复所需内容却不可读取的窗口。

恢复顺序应固定为：恢复执行事实 → 恢复对应对话与证据 → 校验环境变化 → 处理
未完成 Effect → 发起下一次采样。

Continue 可以保留新 Turn ID，但应以结构化字段引用来源快照和累计工作进度，
不要仅依赖解析 Prompt 中的恢复文本。旧 Turn 终态保持不可变。涉及公开协议或
持久化契约的修改，使用仓库生成命令并补充一致性测试；未发布阶段不预设无明确
需求的兼容 Migration。

### 第三阶段：完善证据复用与检索

读取记录应包含路径、读取范围、内容版本、结果引用和调用身份。内容未变化且
范围覆盖时提供原结果；文件变化、范围不足或引用失效时允许必要读取，并明确
记录失效原因。

恢复摘要承担导航职责：描述已有结论、证据位置和下一步，不能成为完整历史的
唯一替代品。`turn_history` 应能读取独立的执行 Transcript，并覆盖取消、失败和
多次 Continue 的来源链，不能完全依赖终态压缩后的 SessionDelta。

跨 Turn 缓存只适用于具备明确复用契约、能够校验依赖版本的工具，不能把任意 Shell
或外部调用当作可安全缓存的读取。缓存减少执行成本，完整的上下文续接才减少
模型重新决定探索的次数。

## 验收矩阵

| 场景 | 应断言的行为 |
| --- | --- |
| 搜索和读取完成后停止，再 Continue | 首次模型请求含关键结果，或含有效且足够明确的检索引用 |
| 工具结果已发布、尚未回填时取消 | 已完成结果进入 Durable Transcript 和对应续接状态 |
| 并行工具部分完成时取消 | 已完成结果保留；未完成与结果未知的调用有独立状态 |
| 第 N 次采样期间重启 | 前 N−1 次必要闭合对话仍可见，已完成 Effect 不重新派发 |
| 连续多次 Continue | 早先结论与证据不逐次消失，恢复指令不重复累积 |
| 历史压缩后恢复 | 被裁掉的证据引用仍可解析，必要因果链满足预算内保留要求 |
| 工作区、模型或工具身份变化 | 仅在可证明范围内复用，失效记录可解释，授权重新按契约校验 |
| 工具结果存储或事实提交失败 | 不声称已完成且可恢复；重试不会重复已确定的副作用 |

先用确定性 Provider Fixture 验证恢复后首次请求的内容、调用次数和引用可达性，
再补充真实进程重启、持久化故障和并发取消测试。实际模型实验可测量用户体验，
但不能替代确定性的持久化与副作用不变量测试。

建议记录恢复来源及原因、上下文快照摘要、已完成结果的复用情况、检索失效原因，
以及恢复后发生的等价探索。区分“重新交付结果”和“重新执行工具”；判断等价
探索时结合参数、读取范围与依赖版本，避免把必要的重新验证误算成退化。

## 推荐推进顺序

先修复取消路径和 Continue 的全量历史删除，再补齐执行中对话的持久化恢复，
随后完善证据版本与检索。验收中心应是恢复后的首次模型请求和副作用执行次数，
而不是仅观察 Turn 最终能否重新进入 Completed。
