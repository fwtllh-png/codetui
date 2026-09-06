# 任务推进性能与折返问题：全局分析与改进方案

| 项 | 内容 |
| --- | --- |
| 状态 | P0 已实施（见第 7 节）；P1-1、P1-2、P1-3 已实施（见第 7 节）；其余 P1/P2 未实施 |
| 日期 | 2026-09-06 |
| 基线 | 当前 `main`（bc7c94dc）+ 工作区未提交改动 |
| 范围 | Runtime 任务推进速度、采样轮次效率、上下文保持 |
| 定位 | 非产品手册；`docs/zh-CN` 仍只描述已交付行为 |

## 1. 问题陈述

用户体感归纳为三个可度量的现象：

1. **回合间延迟**：一个回合结束后，下一个工作迟迟不开始。
2. **无进展采样轮次**：执行过程中出现"被拒绝 → 重试 → 再被拒绝"、"写了又被丢弃"的循环，
   表现为折返、迂回前进。
3. **信息重做**：模型重读已读过的文件、重翻截断的结果、重写已写过的答案。

本文档给出根因全景（第 3 节）、按优先级排列的改进方案（第 4 节）与度量方法（第 5 节）。
所有代码行号均指当前工作区版本，实施时以符号名为准。

## 2. 分析方法

- 审阅 `README.md`、`docs/zh-CN/architecture.md`。
- 四个子系统并行代码调研：主循环与收敛机制、上下文组装与结果投影、工具执行链路、
  Subagent 编排与采样限流。
- 关键结论在源码二次验证（第 3 节中标注"已验证"的条目）。

## 3. 根因全景

四类问题相互叠加。单 Agent 交互场景以 A/B/C 类为主；多 Agent 场景 D 类占主导。

| 类别 | 一句话概括 | 主要体感 |
| --- | --- | --- |
| A 回合间固定税 | 每个回合之间有一笔与任务无关的固定延迟 | 慢、卡 |
| B 闸门制造迂回 | 防止原地打转的硬闸门本身消耗采样轮次 | 折返、碰壁 |
| C 信息丢失重做 | 压缩与预算截断导致模型重新获取信息 | 迂回、重做 |
| D 并行度放大器 | 采样全局串行 + 长命令碎片化轮询 | 多 Agent 时显著变慢 |

### 3.1 A 类：回合间固定税（结构性延迟）

**A1 收尾叙事同步阻塞下一个回合（已验证）。**
每个 turn 完成后、排队的下一个 turn 开始前，Runtime 同步调用一次 summary 模型生成
narrative：`internal/runtime/app/context_maintenance.go:32` 在
`internal/runtime/app/turn_service.go:128-133` 的 `TurnQueueService.Drain` 之前执行，
使用 `context.Background()`，超时默认 30 秒
（`internal/config/defaults.go:57-58`）。触发条件：`SemanticNarrative == "post_turn"` 且
`OmittedHistory` 非空（`internal/runtime/agent/engine/narrative.go:176-207`），即会话
历史超过 `recent_tail_turns`（默认 2）后**每个回合都触发**。provider 慢或挂起时，
每个回合固定追加最多约 30 秒的空转。

**A2 沙箱 Prepare 每次进程启动都全仓遍历（已验证）。**
Seatbelt 后端每次 `Prepare` 都执行 `validateWorkspaceLinks` —— 对整个 workspace 做
`filepath.WalkDir` 硬链接校验（`internal/security/sandbox/backend.go:256-329`、
`:1159-1248`），并重新读取解析最多 1MB 的 `/System/Library/Sandbox/Profiles/system.sb`
（`:838-868`）。每次 `exec_command`、`write_stdin`、gopls 诊断都付一遍。大仓库下
这是每次进程启动的最大单项开销。

**A3 修改型工具的重复校验。**
一次 `file_edit` 对目标文件约 8–10 次全文读 + hash（prepareMutation、ExactEditProofs、
validateFileWrites、broker 四次快照、Journal Before/After、finishBrokerFileWrites），
2 次 ledger fsync；审批路径下同一编辑计划最多计算 3 遍
（`internal/adapter/tool/guard/pipeline_authorize.go:259-283`）。
另外**每次工具调用都重新编译 JSON Schema**，无任何缓存
（`internal/adapter/tool/tool.go:940-956`）。

**A4 journaled 写后同步跑 gopls。**
每个受管文件写完成后同步在沙箱内运行 `gopls check`
（`internal/adapter/tool/guard/guard.go:637-679`、
`internal/observability/diagnostics/diagnostics.go:66-77`），冷启动一次 gopls 进程
（并叠加 A2 的全仓 walk）。

**A5 热路径调试残留（已验证，应立即删除）。**
`internal/adapter/tool/tool.go` 的 `validateBindingLocked` 中有一段标注
`#region debug-point C:stale-binding` 的代码，在 `file_read` binding 过期时同步
`http.Post("http://127.0.0.1:7777/event", ...)`，**无超时控制**。若 7777 端口存在
半开连接会无限期阻塞该调用路径。

### 3.2 B 类：防迂回闸门本身制造迂回（折返感的核心）

QCode 为省 token 和防"原地打转"设置了大量硬闸门。每次被闸门拒绝都消耗一整轮模型采样
（延迟 + token），这就是"折返"体感的机制化来源。

**B1 正文随时作废重写。**
模型的普通文本是 ProvisionalOutput，一旦提出新工具调用即被丢弃
（`internal/runtime/agent/turnkernel/reducer_tool.go:27-36`）；每次 repair 也执行
`DiscardOutput`（`internal/runtime/agent/engine/turn_handler.go:792-837`）。最终答案
必须在 `turn_complete` 的 summary 里重写一遍——"写了丢、丢了再写"。

**B2 强制结构化收尾与 Declaration Repair。**
默认 `RequireCompletionDeclaration = execution.Tools`（即 true，
`internal/runtime/app/wire/modules_runtime.go:167`）。只读直答回合可由 provider
`end_turn` 完成（架构文档已述），但**执行过工具的回合必须 `turn_complete` 收尾**；
纯文本停止触发 Declaration Repair（预算默认 1，
`internal/runtime/agent/turnkernel/reducer_common.go:109-117`）再采一轮。

**B3 "想结束被强制继续"链。**
plan 存在未完成步骤且发生过 mutation 时，`turn_complete(complete)` 被拒
（`plan_progress_incomplete`，
`internal/runtime/agent/turnkernel/reducer_verification.go:132-138`），随后走
"拒绝 → Declaration Repair → 再拒 → Convergence → 最终可能 blocked 失败"链
（`turn_handler.go:818-871`、`:692-763`），最多消耗 3+ 轮采样。合法出口只有两个：
把剩余步骤做完/标 done，或声明 `status=incomplete`。

**B4 观察闸门开局碰壁。**
Continue 恢复时 `git_status`/`git_diff` 直接被拒；已读路径的整文件重读被拒；finish-only
阶段无窗口 `file_read` 被拒（`internal/runtime/agent/engine/observation_gate.go:17-94`）。
模型"先看看现状再动手"的惯性开局会连续撞墙数轮，每次撞墙 = 一轮完整采样。

**B5 无进展检测的长尾。**
实现阶段（Work Item 有 Known/Open 后）改用
`execution.implement_no_progress_samples`（默认 6）：第 3 个无进展采样开始提示收敛，
第 6 个进入 finish-only，但强制 Finalization 要到
`max(lease+1, MaxSteps + repair 预算)` = 69 个无进展采样之后
（`internal/runtime/agent/turnkernel/reducer_sampling.go:449-455`、
`internal/runtime/agent/engine/turncontext.go:160-170`）。中间区间很长，且 `MaxSteps`
（默认 64）**不是硬步数上限**——主循环 `for step := 0; ; step++` 没有 step 检查
（`turn_handler.go:891`），小进展（如把 plan 步骤标 done）可持续续租。

**B6 失败缓存回放。**
模型原样重发失败调用时直接拿回同一份缓存失败
（`internal/adapter/tool/result_cache.go:161-183`，`replayed_from_call_id`），
不产生新事实。模型不解析提示时就表现为"再试立刻又败"。要拿新事实必须改参数，
或先发生一次 mutation 使缓存失效。

### 3.3 C 类：信息丢失导致的重做（迂回的结构性来源）

**C1 并行批次预算均摊。**
每次工具结果的 token 预算 = 容量 ÷ 本批并行调用数
（`internal/runtime/agent/engine/tool_handler.go:52-68`）。模型一次发 6–8 个读，
单个结果预算被压到 1/6–1/8，大文件/大搜索几乎必然截断 spill → 模型调 `result_get`
翻页 → 每页又是一轮采样。一次信息获取变成 2–3 个工具回合。

**C2 `turn_history` 与提示语不符（已验证）。**
系统提示告诉模型"旧回合用 `turn_history` 找回"，但该工具**只读内存 history**
（`internal/runtime/agent/engine/turn_checkpoint.go:16-29`，读 `e.history`）。
History Replacement 发生后，被摘要掉的回合从 `e.history` 消失，`turn_history` 报
"turn N is not in durable history"——模型被指路后撞墙，只好重读文件。

**C3 压缩丢原文。**
压缩后被移除消息的原始 Tool Result 文本、reasoning 全部消失，幸存的只有每消息
≤512 字节的 digest 行（`internal/runtime/agent/context/compaction_candidate.go:139-244`）；
Truth capsule 的 Summary 分节按"最贵先渲染、整节丢弃"排序，facts/digest 排在最后
最先被扔（`internal/runtime/agent/context/compact_compact.go:122-177`）。模型需要旧
内容时重读文件是常态。

**C4 回合中段无摘要压缩。**
mid-turn 阶段只允许一次 Visible Tail Fold；仍超硬限制则直接 ResourceExhausted 终止
整回合（`internal/runtime/agent/engine/history_recovery.go:37-138`）——最贵的折返：
整回合白做。

**C5 token 估算系统性偏低。**
`runes/4` 估算（`internal/runtime/agent/context/budget.go:16-35`）低估代码类真实
token，真实值靠 provider 观测回填；首段窗口内预算判断偏松，反复触发 prune/fold，
模型体感为"上下文反复变脸、信息来回找"。

### 3.4 D 类：多 Agent 与长命令的放大器

**D1 采样全局单飞。**
Session 级限流器是容量为 1 的队列
（`internal/runtime/agent/engine/shared_rate_limit.go:13-63`），主 Agent 与所有子
Agent 的模型调用**完全串行**——一次流式采样期间整个 session 其他引擎都在排队
（`model_handler.go:385` 持有 token 贯穿整个采样）。任一子 Agent 触发 429，
冷却期冻结整个 session 并阻止新 spawn
（`internal/orchestration/subagent/supervisor.go:153-166`）。子 Agent 的"并行"只在
工具执行层。默认 posture 下这是多 Agent 场景延迟的最大放大器。

**D2 子 Agent 准冷启动。**
默认 context mode 是 `task_capsule`：任务说明 + 至多 16 个 RelevantFiles（**只有
路径，没有内容**）+ 16 条 Evidence 摘要，显式排除父转录
（`internal/orchestration/subagent/context_fork.go:290-332`）。子 Agent 必须重新
探索父 Agent 已读过的文件，每次探索又撞 D1 队列，双重放大。

**D3 长命令碎片化轮询。**
`exec_command` 首次采样只等 `yield_time_ms`（默认 10s），进程未结束即返回
`session_id`，之后**完全靠模型手动 `write_stdin`（每次默认再等 5s）续接**
（`internal/adapter/tool/shell/protocol.go:20-26`）。Runtime 不自动续接输出。一次
3 分钟的构建/测试需要 6–36 次往返，每次都是完整的采样轮次。

**D4 子 Agent 生命周期摩擦。**
serialized child 共享父 turn 的 workspace gate，要等父 turn 整个结束才能开跑
（`internal/runtime/agent/engine/start_handler.go:8-34`、
`internal/runtime/app/wire/childruntime.go:498-508`）；子 Agent 结果只回传 400 字符
截断的摘要 + 最多 64 个变更路径
（`internal/orchestration/subagent/lifecycle.go:149-166`），信息不足引发追加
`followup_task`。

## 4. 改进方案

按优先级分档。每项给出：动因 → 改法 → 验收 → 风险。

### P0：立即修（低风险、高确定性）

**P0-1 删除调试钩子。**
删除 `internal/adapter/tool/tool.go` `validateBindingLocked` 中的
`#region debug-point C` HTTP 上报块。
验收：`grep -rn "127.0.0.1:7777" internal/` 无结果；`go test ./internal/adapter/tool/...` 通过。
风险：无。

**P0-2 `turn_history` 接持久层。**
被 History Replacement 裁掉的回合应仍可从持久层（Turn Checkpoint / Event Log /
CAS 中的 ResultStore 原文）有界回读；短期退路是修正提示语，让提示只指向
确实可用的 `result_get` handle。
验收：触发一次真实压缩后，`turn_history` 对被裁回合仍返回有界内容而非报错。
风险：中——需要确定旧 turn 原文的权威恢复源；不得绕过 CAS/Event Log 的既有边界。

**P0-3 缓存 JSON Schema 编译。**
在 Registry 生命周期内按 descriptor revision 缓存编译产物，descriptor 变更时失效
（Registry 已有 revision/generation 机制可复用，`tool.go:962-982`）。
验收：基准测试中每调用固定开销显著下降；`NormalizeArguments` 行为不变
（现有 tool_test 覆盖）。
风险：低——缓存键必须包含完整 descriptor，否则会校验错 schema。

**P0-4 缓存沙箱 Prepare 的全仓校验结果。**
`validateWorkspaceLinks` 与 `auditSeatbeltSystemProfile` 的结果按 workspace root +
关键目录 stat/mtime 缓存，文件树变更时失效；`system.sb` 按内容版本缓存。
验收：大仓库下第二次及以后的进程启动不再触发全仓 WalkDir（trace 可见 Prepare 耗时
下降）；硬链接防护行为不变（构造符号链接/硬链接用例回归）。
风险：中——缓存失效判断漏掉变更会削弱安全校验，失效条件需要保守
（宁可多 walk 不可漏）。

**P0-5 收尾叙事异步化。**
业务 terminal 发布后立即 `Drain` 队列，narrative 改为后台执行；下一个 turn 首次
采样前 join——若届时未完成则按既有语义回退 Truth + Tail（narrative 本就是非权威
层，失败回退路径已存在，`narrative.go:257-267`）。
验收：长会话中 turn 完成到下一 turn 开始的间隔不再包含 narrative 耗时；narrative
成功时下一 turn 仍能看到摘要；失败时回退语义不变。
风险：中——需要保证 join 点在 world 投影之前，且取消/恢复路径不丢 usage 上报。

### P1：机制调整（中等工作量）

**P1-1 工具结果预算从均摊改为按需 + 总量约束。**
现状 `capacity / len(calls)` 惩罚并行度。改为：按调用声明的需求（读窗口大小、
搜索 max_results）分配，总量不超过本批预算；读类工具优先保完整投影，超总量时
按工具类型截断（现状的 spill 机制保留为兜底）。
验收：典型 6–8 并行读场景 spill 率显著下降；`result_get` 调用次数下降（trace 可测）。
风险：低——投影层改动，Durable 原文与 ResultStore 语义不变。

**P1-2 长命令自动续接。**
进程未结束时由 Runtime 后台持续收集输出进 ResultStore；模型下次 `write_stdin`
（或回合推进）直接拿自上次以来的全部增量。保持 `yield_time_ms` 的首等待语义与
取消/回收语义不变。
验收：一次长构建的 `write_stdin` 往返次数从"每 5s 一次"降为"模型主动查看的次数"；
输出无丢失（与现行 session 语义一致）。
风险：中——后台收集的生命周期要与 Turn 取消、进程组回收、session 关闭对齐。

**P1-3 采样并发从权威能力派生。**
现状容量恒为 1。改为：已知 provider 并发/速率限制时按声明值派生并发度，未知时
保持单飞（遵守仓库硬规则：不发明绝对经验阈值）。至少将 summary/narrative 路由
与主对话路由的采样分离，避免收尾叙事与业务采样互相排队。
验收：多 Agent 场景下子 Agent 采样不再被父采样完全阻塞；429 冷却不再跨路由
全局冻结（仍需遵守 provider 声明的限制）。
风险：高——并发化会放大 provider 侧限流与配额压力，必须以显式配置或声明能力为
上限，并保留共享冷却语义。

**P1-4 收窄无进展长尾 + repair 保留正文。**
让 implement lease（`execution.implement_no_progress_samples`）成为三个阶段
（提示收敛 / finish-only / 强制 Finalization）的一致权威来源，消除
"finish-only 在第 6 轮、强制收敛在第 69 轮"的长尾；同时评估 repair 时保留已捕获
正文（复用 `preserve_provisional` 先例），减少"答案重写"。
验收：连续无进展的 turn 在 lease 耗尽后进入 Finalization 的轮次可由配置直接解释；
repair 后正文不再需要整体重写。
风险：中——收敛节奏加快可能截断确实在缓慢推进的工作；需要以 trace 中
"finish-only 后恢复进展"的频率为依据调校。

**P1-5 拒绝反馈结构化（进行中，与当前未提交改动同方向）。**
当前工作区改动（`file.go` 的 `start_line` schema 说明、`result/recovery.go` 的
编辑失败恢复指引、`observation_gate.go` 拒绝时指向 `turn_history`/`result_get`、
`tool.go` ModelResult 保留编辑恢复事实）正是本项的一部分：把每次"拒绝"变成
"可直接执行的纠正指令"，直接减少 B4/B6 的折返轮次。
验收：既有新增测试（`result_test.go`、`tool_test.go`）通过；trace 中同类拒绝的
重复发生率下降。
风险：低。注意 P0-2 落地前，`observation_gate.go` 的拒绝提示指向的
`turn_history` 存在 C2 的不可用路径，两者应一起交付。

### P2：长期方向

**P2-1 子 Agent capsule 携带关键内容摘要。**
在 `task_capsule` 的 16 个 RelevantFiles 基础上，为每个路径附带父会话已读窗口的
有界摘要或 ResultStore handle，缓解 D2 的重复探索。需维持"Never copy parent
transcripts"的委托契约——只共享文件事实，不共享对话。

**P2-2 受控的 mid-turn 压缩。**
长回合中段超压时，在"一次 view fold"与"ResourceExhausted 终止"之间增加受控的
分层降级（如仅压缩已闭合工具回合的原文、保留 handle），避免整回合白做（C4）。

**P2-3 token 估算校准前置。**
用 provider 观测到的真实 token 比率（已有 `Observe` 回填通道）在会话早期就校正
`runes/4` 估算，减少首段窗口的反复 prune/fold（C5）。

## 5. 度量与验证

先用现有可观测面建立基线，改一项测一项，避免凭体感归因：

| 指标 | 来源 | 对应根因 |
| --- | --- | --- |
| turn 完成 → 下一 turn 开始的间隔 | Trace / Turn Queue | A1、A5 |
| 每回合采样数、无进展采样数 | Trace（Phase Latency） | B2、B3、B5 |
| 工具被拒次数与同类拒绝重复率 | Execution Receipt | B4、B6 |
| `result_get` 调用次数 / spill 率 | Execution Receipt | C1 |
| 压缩后 `turn_history` 失败次数 | 工具结果 | C2 |
| 沙箱 Prepare 耗时分布 | Trace | A2 |
| 多 Agent 采样排队等待 | Trace / Usage | D1 |
| 长命令 `write_stdin` 往返次数 | Execution Receipt | D3 |

验收表述采用相对改进（如"P0-5 后 turn 间隔分布的 p95 不再包含 narrative 等待"），
不引入新的绝对阈值（遵守仓库硬规则）。

## 6. 实施顺序建议

1. **第一批（可独立合入）**：P0-1、P0-3（纯性能，无语义变化）。
2. **第二批**：P0-5（narrative 异步化）+ P0-2（turn_history 持久化）+
   P1-5（当前未提交改动，二者互相依赖提示语的正确性）。
3. **第三批**：P0-4（沙箱缓存，需保守失效条件）、P1-1、P1-2。
4. **第四批**：P1-3、P1-4（涉及收敛与并发语义，需以 trace 数据支撑调校）。
5. **后续**：P2 项结合 roadmap 排期。

每批合入后重跑第 5 节基线，确认对应指标改善且无回归。

## 7. P0 实施记录（2026-09-06）

| 项 | 状态 | 说明 |
| --- | --- | --- |
| P0-1 调试钩子 | 已完成 | 删除 `validateBindingLocked` 的 7777 上报块及 `net/http` 导入 |
| P0-3 Schema 编译缓存 | 已完成 | `compileArgumentsSchema` 按 schema 内容 sha256 缓存编译产物；同 schema 复用、不同 schema 不串（测试锁定） |
| P0-5 收尾叙事异步化 | 已完成 | `Engine.PreparePostTurnNarrative` 同步捕获快照，Runtime sink 在 terminal 发布后立即 Drain、narrative 后台结算；`Engine.Execute`、`CompactForcedDurable`、`ReplaceHistory` 入口在取 `e.mu` 之前 join 挂起的 narrative（无条件等待结算完成；结算本身受已配置的 `NarrativeTimeout` 界定，等待上界与同步路径一致，且保证结算与下一回合严格有序、不与引擎锁产生数据竞争）。narrative 不经过共享采样单飞，异步化不会与业务采样争锁 |
| P0-2 turn_history 持久回读 | 已完成 | 引擎新增 `TurnTranscriptArchive` 端口：内存未命中时按 `turnIDs` 反查持久 turnID，经 `app.TurnTranscriptArchive` 从 terminal envelope 的 Context Manifest（CAS Base/Tail blob）重建完整 History 后提取目标回合；仅持久运行时装配 |
| P0-4 沙箱 Prepare 缓存 | 部分完成 | `auditSeatbeltSystemProfile` 改为按 system.sb 的 size+mtime 缓存成功审计（失败不缓存，stat 变化即失效，测试锁定）。`validateWorkspaceLinks` 的全仓 walk **有意保留每次执行**：外部进程可在校验后新建指向工作区内的硬链接，该变更不改变工作区任何目录的 mtime，不存在廉价且完备的失效信号；缓存该检查会削弱沙箱边界。若后续要降低该项成本，方向是并行化 walk（语义不变），而非缓存 |

验证：`go build ./...`、`go vet`（触及包）、相关包 `go test -race`
（adapter/tool、adapter/tool/turnhistory、runtime/agent/engine、runtime/app、
runtime/app/wire、security/sandbox、persist/artifact）、`scripts/check-docs.sh`
全部通过。

### P1-1 实施记录（2026-09-06）

工具结果预算从"每结果均摊"改为"按需分配 + 批次总量约束"：

- **每项上限**（`ResultTokenBudget`，供生产端预裁与单项兜底）：
  `min(autoCompactLimit, surfaceItemTokens, ResultStore 容量)`——去掉
  `/len(calls)` 均摊；`surfaceItemBytes` 按其命名本义用作每项上限
  （`ToolSurfaceBudget` 与 `dynamicToolResultSurfaceBytes` 的既有语义一致）。
- **批次总量**（新增 `ResultBatchBudget` ctx 值）：
  `min(autoCompactLimit, surfaceMaxTokens)`——与旧均摊的聚合上限
  `N × (总量/N)` 完全相等，模型可见表面预算的总量契约不变。
- **分配算法**（`ResultStore.AdmitBatchWithin`，max-min 水位线）：批次执行
  完成后按真实大小升序分配——小于当前水位线的结果全额内联并归还余量，
  超过的在水位线处 spill（截断通知 + `result_get` handle 的既有机制）。
  全大结果批次退化到与旧均摊完全相同的每份份额；混合批次中小结果不再
  浪费配额、大结果获得回收份额。
- **接入点**：`turnkernel/tool_effect.go` 的批次后准入循环改为一次
  `Registry.AdmitBatchWithin`；`engine/tool_handler.go` 设置双预算；
  单结果兜底路径（批次中止等）仍走 `AdmitResultWithin`。
- **幂等安全**：历史重放准入（`admitToolResultHistory`）以全量
  `autoCompactLimit` 校验既有 receipt，池化分配的配额恒 ≤ 该值，
  不会被下一轮采样回裁。
- **行为保持**：N=1 批次、既有经济表面预算测试
  （`TestRunToolsEnforcesRecordedEconomicSurfaceBudget`）语义不变；
  生产端（search 族）的 ctx 预算从均摊值提高到每项上限，模型声明的
  `max_results/max_file_bytes` 被更忠实地执行，超量部分在准入层以
  spill+handle（可找回）替代列表截断（不可找回）。
- 新增测试：池化分配单元测试四例（混合批次回收、全大批次与均摊等价、
  零总量回退、每项上限约束）与引擎级混合批次端到端测试
  （`TestRunToolsPoolAdmitsMixedBatchByDemand`）。

### P1-2 实施记录（2026-09-06）

长命令自动续接（`write_stdin` 安静窗口语义）：

- **调研修正**：计划中"Runtime 后台持续收集输出"这半在现有架构中已经
  成立——session 的输出由后台泵持续写入 archive，`WaitNext` 以交付游标
  返回自上次以来的全部增量。实际缺口在等待窗口语义：默认 5 秒窗口把
  静默长构建变成"每 5 秒一次模型轮询"，且数据到达即返回时进程往往
  尚未回收（`running=true` + 内容已到），又制造一轮确认轮询；持续
  输出型构建则是每块输出一轮采样。
- **实现**（`internal/adapter/tool/shell/protocol.go`）：
  `write_stdin` 在**未声明 `yield_time_ms`** 时启用 `waitSessionOutput`
  安静窗口语义——持有会话直到三者之一：进程退出；输出到达后再安静
  满一个窗口（默认 5000ms）；到达公共上限 30000ms（`maxProcessYield`，
  既有公开常量，未新增阈值）。窗口内持续到达的输出经 head+tail 累积器
  合并为单条结果（受 `output_tokens` 截断约束并上报
  `omitted_bytes`）。
- **声明路径不变**：模型显式声明 `yield_time_ms` 时保持单窗口精确等待
  （原契约逐字尊重）；`exec_command` 的首等待语义按计划要求未改动。
- **提示补齐**：安静超时返回附带 `process_still_running` /
  `required_action=write_stdin` / `retry_original=false` 元数据，与
  exec_command 的仍运行提示一致。
- **生命周期对齐**：ctx 取消即时传播（`Wait` select ctx.Done）、
  `timeout_ms` 杀进程组后 `Wait` 返回退出态、退出自动 `Close` 会话，
  均未改变；同一会话的并发 `write_stdin` 仍由 `deliveryMu` 串行
  （等待上限从 5s 提高到 30s）。
- **量化预期**：3 分钟静默构建的轮询从约 36 次降到约 6 次；"输出后
  即退出"的命令不再需要额外一轮确认轮询。
- 新增测试三例：5.3 秒后退出的静默进程被扩展等待直接捕获退出、
  声明 100ms 窗口精确生效（含提示元数据断言）、0.25 秒到达的输出
  快速返回且合并退出态。

### P1-3 实施记录（2026-09-06）

采样并发从权威能力派生：

- **调研修正**：`execution.max_concurrent`（默认 8，TOML、
  `QCODE_MAX_CONCURRENT` 与启动覆盖三通道，校验为正，provenance 完整）
  **已经是运维声明的 Provider 并发合同**，此前只接到 Provider HTTP 客户端
  一层；会话级 `SharedRateLimit` 硬编码容量 1 压在其上，使声明的并发从未
  生效。原分析的"容量恒为 1"实为声明与实现不一致，而非保守默认——因此
  无需新增任何配置字段。
- **实现**：`NewSharedRateLimit(concurrency)` 容量从
  `execution.max_concurrent` 派生（`<1` 回退单飞）；采样门与 HTTP 客户端
  在两个层面执行同一声明值。
- **共享冷却语义原样保留**：任一采样 429 后 `Record` 设置全局
  `cooldownUntil`，全部并发槽的后续 `Acquire` 等待冷却结束
  （`TestSharedRateLimitCooldownFreezesAllSlots` 锁定）；`Hot()` 驱动的
  Subagent spawn 准入、`BeginUserTurn` 保留剩余冷却等行为不变。
- **声明为 1 时与改造前完全一致**：原 serialization 测试保留为
  `limit=1` 用例；并发测试以 3 引擎/容量 2 验证第 3 个采样等待
  （`TestSharedRateLimitAdmitsDeclaredConcurrency`）。
- **计划第二项经核实已是现状**：summary/narrative 采样直接走 Provider、
  不经过采样门（P0-5 实施时已验证），并有独立的
  `summaryRouteCooldown`——429 冷却不跨路由全局冻结，无需改动。
- **风险控制**：并发上限即显式配置本身（未引入任何新阈值）；route 级
  `rate_limit` RPS 平面与 TPM 准入平面不变，仍按 Provider 反馈动态限流。
- 配置文档已补充 `execution.max_concurrent` 的双层语义说明。
