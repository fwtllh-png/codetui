# A2UI 分阶段技术实现方案

> 状态：待实施设计，不代表功能已经交付。
> 编写日期：2026-09-11。
> 基线：编写时的当前工作区代码，包含尚未提交的变更；实施前需重新核对相关接口。
> 本文只规划 QCode 的接入，不要求同时开发新的 Skill、MCP Server 或独立应用。

## 阅读导航

- [决策与阶段总览](#1-决策摘要)
- [架构与所有权](#4-总体架构与所有权)
- [协议与数据模型](#5-协议与组件目录)
- [持久化与恢复](#7-持久化恢复与一致性)
- [交互与授权](#8-交互授权与后续任务)
- [逐阶段任务及验收](#12-分阶段实施任务)
- [测试与验证](#13-测试矩阵与验证命令)
- [开关与回退](#14-开关兼容与退出)
- [工作量估算](#15-工作量与推进方式)

## 1. 决策摘要

引入 A2UI 作为现有对话中的受控动态呈现协议，解决两类问题：

1. Agent 按当前任务组合表单和结果视图，减少用户将界面信息重新描述成自然语言。
2. 不同 Skill 和工具复用组件目录，在已有组件覆盖的范围内避免逐工具开发专用前端。

不替换 Runtime Operation/Event，不新建执行控制面，不把现有工作台整体改造成
模型生成界面。普通解释继续使用 Markdown；审批、执行事实、Diff 和凭证设置继续由
QCode 的确定性界面负责。

建议技术路线：

- 以 A2UI `v0.9.1` 为目标协议，使用明确版本的 QCode 自定义 Catalog。
- 优先验证官方 `@a2ui/react` 与 `@a2ui/web_core`，不因采用协议重写前端框架。
- 采用结构化 Tool 参数/结果承载 UI，不从 Markdown、代码围栏或任意字符串中猜测 UI。
- 先交付可恢复的只读 Surface，再交付结构化输入、结果交互和外部能力接入。
- 所有界面必须有文本或确定性表单降级；降级不扩大权限，不假装操作成功。
- 新能力默认关闭，按照阶段启用；新提交的交互契约必须先可恢复，再允许用户使用。

### 1.1 阶段总览

| 阶段 | 交付名称 | 用户可见成果 | 前置依赖 | 停止点 |
| --- | --- | --- | --- | --- |
| 0 | 协议与依赖验证 | 内部 Fixture 验证，不开放生产入口 | 无 | 不合适则停止引入依赖 |
| 1 | 可回放的只读界面 | Agent 在对话中展示原生列表和结构化结果 | 阶段 0 | 可独立保留为只读能力 |
| 2 | 可恢复的结构化输入 | 多字段、多选、数值等一次性提交 | 阶段 1 | 推荐的首个用户试点版本 |
| 3 | 证据驱动的结果交互 | 筛选诊断、选择条目、带上下文发起后续任务 | 阶段 2 | 推荐的首个完整产品闭环 |
| 4 | Skill 与 MCP 接入 | 不修改工作台即可接入新的受控界面组合 | 阶段 3 | 外部来源可单独关闭 |
| 5 | 发布与长期运行 | 完整故障、性能、供应链和降级验收 | 各阶段已有聚焦测试 | 决定是否默认启用 |

阶段号只用于本文排期，不进入包名、协议名、工具名或提交标题。
每阶段都必须含自己的安全、恢复和性能测试，阶段 5 不是延后这些工作的理由。

## 2. 目标、非目标与成功标准

### 2.1 首批用户场景

| 场景 | 用户操作 | Agent 获得的结果 | 不允许的推断 |
| --- | --- | --- | --- |
| 确认重构范围 | 选择模块、输入约束、选择兼容要求 | 带字段 ID 和类型的回答 | 选择文件不等于批准修改 |
| 比较依赖升级方案 | 比较版本和风险，选择候选项 | 被选条目 ID 和方案版本 | 选择目标版本不等于批准安装 |
| 查看诊断结果 | 按文件或状态筛选，展开证据 | 本地筛选无需反馈；提交后才有选择上下文 | 模型解释不等于真实验证通过 |
| 使用不同 Skill | 不同任务呈现不同组合，沿用统一组件 | 统一交互契约 | Skill 不得安装任意可执行 UI |

首批试点选择“多字段需求确认”和“诊断结果选择后继续处理”。
不为了演示新增独立诊断系统，消费已有 Tool Result、Receipt 和文件引用。

### 2.2 不在本方案范围内

- 替换聊天编辑器、会话导航、Settings、Git 操作、审批和 Subagent 生命周期。
- A2A Server、AG-UI Transport、MCP Server Host、多端应用或公网部署。
- MCP Apps 的 HTML/JavaScript iframe 执行环境。
- 自动加载远端组件代码、任意 CSS、JavaScript 表达式、浏览器网络或文件系统能力。
- UI 按钮直接执行命令、修改文件、提交 Git、部署服务或批准计划。
- 为 UI 启动专用模型、独立 Agent、后台任务队列或第二套工作流引擎。
- 照搬全部 A2UI Catalog，或为尚无消费者的版本构建兼容矩阵。

### 2.3 可衡量的成功标准

采用相同任务、模型、数据和设备，对比 Markdown/现有输入与新界面：

| 指标 | 定义 | 验收方法 |
| --- | --- | --- |
| 用户往返次数 | 从首次要求输入到取得完整合法输入的提交次数 | 首批固定任务集逐项对比 |
| 输入纠错率 | 因歧义、类型或遗漏而重新提交的任务比例 | 区分用户改主意与界面错误 |
| 首次可交互时间 | Input Request 被接受到表单可操作的时间 | 分离模型生成、传输、模块加载和渲染 |
| 额外模型开销 | Catalog、UI 提案和修复造成的 Token 与延迟 | 使用真实 Usage；不把传输节约当 Token 节约 |
| 接入成本 | 新场景是否需要修改通用前端或 Runtime 分支 | 用两个领域不同的 Fixture 验证复用 |
| 长会话性能 | 帧间隔、输入响应、内存、快照读取和写入量 | 与现有 streaming Fixture 在同环境对比 |
| 正确性 | 错归属、重复消费、无审批副作用、丢失已提交回答 | 故障矩阵全部通过，不接受统计性豁免 |

不预先承诺性能或 Token 收益百分比。阶段 0 固定基线、测试设备、测量方法与显式
验收预算，后续不得为了通过测试静默放宽。若同等输入效果只需几个固定表单、
动态组合没有额外收益，可停止在阶段 2，不继续扩张外部 UI 生态。

## 3. 现状与差距

以下是已核对的代码事实，不是目标设计。

| 现有能力 | 代码入口 | 接入意义或缺口 |
| --- | --- | --- |
| 用户输入工具 | `internal/adapter/tool/interact/interact.go` | 当前主要为 `prompt` 和字符串 `options` |
| 输入等待 | `internal/adapter/tool/interact/host.go` | 已有 Request ID、到期时间、恢复与重复回答处理 |
| 输入事件 | `internal/runtime/protocol/event.go` | `input.required` / `input.resolved`，尚无表单 Schema 与 Surface |
| 输入回答 | `internal/runtime/protocol/operation.go` | `InputReplyPayload.Values` 为 `map[string]string`，不能直接表达完整类型 |
| 操作路由 | `internal/runtime/app/operation_dispatch.go`、`session_control.go` | 已有 `input.reply`、Session/Thread 归属绑定与 Pending Work 路由 |
| Turn 状态 | `internal/runtime/agent/turnkernel` | `PhaseAwaitingInput`、输入 Effect 和 Coordinator 是恢复基础 |
| MCP 数据 | `internal/adapter/mcp/types.go`、`internal/adapter/tool/mcp/mcp.go` | 可接收 `StructuredContent`，目前主要归入结果 Metadata，不是 UI 契约 |
| MCP 版本 | `internal/adapter/mcp/types.go` | 当前协议常量为 `2024-11-05`；不能据此假设支持新版 UI 扩展 |
| 事件生成 | `event_traits.json`、`Makefile` 的协议生成目标 | 新事件必须补 Trait、Go/TS/Schema 和 Golden |
| Web 投影 | `web/src/projection/conversation.ts`、`web/src/runtime/client.ts` | 已有增量投影、会话快照、Workspace Cursor 和稳定引用 |
| Web 组件 | `web/src/ui/App.tsx`、`TranscriptCards.tsx` | 输入、审批、工具、Diff 等确定性界面可复用，不应整体替换 |
| 持久化快照 | `internal/persist/history/service.go` | 当前以事件窗口构造呈现快照；UI Patch 被裁剪后需要独立完整基线 |
| 依赖与样式 | `web/package.json`、`web/src/ui/theme` | React 18 / Vite；已有主题和图标体系 |
| JSON Schema | `go.mod` | 已有 `github.com/santhosh-tekuri/jsonschema/v6`，优先复用 |

相关现有测试包括：

- `internal/adapter/tool/interact/interact_test.go`、`host_test.go`。
- `internal/runtime/app/runtime_terminal_recovery_test.go`。
- `internal/runtime/agent/turnkernel/reducer_test.go`。
- `web/src/projection/conversation.test.ts`、`web/src/runtime/client.test.ts`。
- `web/src/ui/App.test.tsx`、`performance.test.ts`。
- `web/tests/e2e/streaming.spec.ts`、`visual.spec.ts`。

## 4. 总体架构与所有权

```text
模型 / Skill 指令 / MCP 工具结果
              |
              v
Tool Registry + Guard -> 结构化 UI 提案
              |
              v
Agent 内的 UI 校验/规范化 -> Coordinator 接受事实
              |                       |
              |                       +-> 既有 Domain Fact / CAS / Outbox
              v
Runtime Event / Snapshot -> Web Host Transport -> Web UI Projection
                                                   |
                                                   v
                                      A2UI Processor + QCode Catalog
                                                   |
                    本地筛选不出浏览器 <-----------+--> 用户提交
                                                          |
                                                          v
                                      input.reply / 普通 turn.start
                                                          |
                                                          v
                                  Runtime 校验 -> 原有 Turn 与受治理工具
```

### 4.1 模块职责

下表新增名称均为拟议名称；实际实现可在既有同职责文件内完成，不为了目录完整性建空包。

| Owner | 职责 | 禁止承担的职责 |
| --- | --- | --- |
| `internal/runtime/protocol` | UI 信封、引用、交互数据、错误详情与公开容量合同 | 依赖 Renderer、执行工具、远端下载 Schema |
| `internal/adapter/ui/a2ui`（拟新增） | 固定上游 Schema、解析、Catalog 校验、消息规范化 | 拥有 Session、Turn、授权或数据库生命周期 |
| `internal/adapter/tool/interact` | 发布 UI 提案、请求结构化输入的薄工具入口 | 在 Tool 回调中直接广播未接受的 UI |
| `internal/runtime/agent` | 校验来源、冻结交互合同、接受 Surface 变更、输入与完成语义 | 建立专用 UI Agent Loop |
| `internal/runtime/app` | Operation 校验、归属与幂等、Snapshot/Artifact 查询端口 | 从界面动作直接执行 Tool |
| `internal/runtime/app/wire` | 构造校验器、Catalog、窄端口，注入主/子 Engine | 实现表单或业务流程 |
| `internal/persist` | 通过既有持久化路径保存引用、快照、回答和恢复证据 | 新建 UI 事件日志或另一个数据库 |
| `internal/host` | 现有同源鉴权、序列化、Transport | 读取任意 CAS、执行 Provider/工具、解释授权 |
| `web/src/projection` | Surface 的增量投影和位置/生命周期索引 | 根据模型文案推断完成、审批或风险 |
| `web/src/ui/a2ui`（拟新增） | Renderer 适配、主题、输入草稿、错误降级 | 发起任意网络请求或执行模型代码 |

UI State 仍是 Projection。Runtime 接受“这份 UI 已发布”的事实，不代表它认可
模型生成文字中的业务声明。审批、验证、预算、权限的权威来源保持不变。

### 4.2 生成路径

提供两种生产路径，共用同一校验和持久化合同：

1. **Agent 组合**：通过 `present_ui`（拟新增）提交 A2UI 消息；用于只读结果和结果选择。
2. **结构化表单**：扩展 `request_user_input`，优先从受限字段 Schema 确定性生成
   A2UI；可选布局仍必须覆盖同一份字段合同，不能创造新授权。

阶段 4 才引入工具/MCP 提供的声明式模板。仅展示确定性结果时不强制再让模型重写一份
组件树。目录说明通过既有 Tool/Skill 按需发现，冻结到 Turn，不在每次采样重复插入
完整 Schema，也不因 UI 改变 Provider Route。

## 5. 协议与组件目录

### 5.1 版本与能力

- 目标协议：A2UI `v0.9.1`。不混入 v0.8 字段或 v1.0 的 `actionResponse`。
- QCode Transport Envelope 单独版本化，不能把 QCode 的 Revision、Owner 等字段
  伪装成 A2UI 标准字段。
- QCode 自定义 Catalog 拟使用稳定标识 `urn:qcode:a2ui:catalog:core:1`，
  同时记录内容 Digest。它是标识符，不是允许客户端联网获取代码的 URL。
- 声明为“支持 v0.9.1 与指定 QCode Catalog 的受限实现”，不宣称覆盖全部 Basic Catalog。
- Server 广告能力、客户端实际 Renderer 能力、启用配置取交集；客户端广告不能扩大权限。
- 在已有 bootstrap/握手结构中添加 `ui_capabilities`，包含协议、Catalog Digest、
  支持模式和限制。纯呈现能力在客户端降级；交互 Schema 与目录在 Request 创建后冻结。
- 多浏览器能力不同不改变已发布合同。无 Renderer 的客户端使用确定性降级，不让
  后连接的客户端覆盖整个 Session 的能力。

阶段 0 必须核对 npm 实际发布版本、React Peer Dependency、构建产物与官方文档的一致性。
通过后锁定确切包版本和 Schema Digest，不使用 `latest`，不默认升级 React。
若官方 React 包不适配 React 18，优先评估保留 `web_core` 的薄 React 适配层；
若维护成本超出收益，停止，不退回自研完整 A2UI 解释器。

### 5.2 消息支持

| A2UI 消息 | QCode 处理方式 |
| --- | --- |
| `createSurface` | 为本次受信调用作用域建立 Surface，验证 Catalog，不接受跨 Owner ID |
| `updateComponents` | 按稳定 ID 更新组件；以整个发布批次为原子校验单位 |
| `updateDataModel` | 使用标准 JSON Pointer 和已验证类型做更新，不自定义路径字符串语法 |
| `deleteSurface` | 记录删除事实，撤销交互绑定；不删除审计证据 |
| 客户端 `action` | 解析为允许的语义动作，映射到既有 Operation 或纯本地交互 |
| 客户端错误 | 转成结构化诊断，不转成授权，也不自动发起新 Turn |

Tool 发布的一个批次只操作一个 Surface。创建批次必须包含可用根组件与完整必要数据，
批次内部可先引用后定义，但提交时不能有悬空引用或环。
外部按消息流传入时先有界组批，不能先渲染未经验证的半份界面。
每个批次至少包含一条消息；创建只能发生一次，删除后不得用同一身份复活。

### 5.3 首批 Catalog

| 阶段 | 组件能力 | 约束 |
| --- | --- | --- |
| 1 | 文本、纵向/横向布局、分隔线、列表、只读键值/表格 | 无任意样式，文本转义，表格数据有界 |
| 2 | 文本输入、布尔选择、单选/多选、数值输入、提交 | 可访问标签、字段级错误、稳定选项 ID |
| 3 | 可选择的数据表、文件/证据引用、详情展开 | 来源由 Runtime 验证；选择与后续执行分离 |
| 4 | 复用上述能力的声明式模板 | 不随模板下载或注册组件代码 |

标准组件能满足时使用标准定义；QCode 专用组件必须在自定义 Catalog 中声明。
图标、布局密度、尺寸、颜色、折叠和可访问性由客户端实现，不由模型自由选择。
不提供可伪造的“审批通过”“测试已验证”通用状态徽章；这类控件只能读取 Runtime
证据。模型给出的风险判断可以展示，但明确属于模型分析。

首版不启用远端 Image、音视频、HTML、iframe、任意函数或表达式。
只允许本地实现并列入 Catalog 的确定性校验/格式化函数，不能访问网络、DOM、
存储或系统能力。`sendDataModel=true` 在本接入 Profile 中显式拒绝，
不静默改写；只按提交字段白名单回传数据。

### 5.4 校验顺序

1. 在解析前检查传输字节上限，使用严格 JSON 解析，拒绝重复键和非有限数字。
2. 校验 Envelope、协议版本与固定 Catalog Schema；未知字段/变体不得静默接纳。
3. 校验组件 ID 唯一性、根、无环引用和路径合法性，拒绝原型污染键。
4. 校验数据绑定类型、字段可写范围、动作名称和引用目标。
5. 校验 Owner、Source Binding、Revision、总状态和单次更新容量。
6. 校验布局能否覆盖必填字段、选项与约束，避免隐藏必填字段的不可完成表单。
7. 校验通过后才产生可接受的 SurfaceMutation；失败保持旧 Revision 不变。

Go 使用已有 JSON Schema 库；Browser 使用固定上游 Processor/Schema。
两端共享测试向量与目录生成来源，不能分别手写两份规则并仅靠文档保持一致。
所有 `$ref` 仅从构建时注册的本地 Schema Registry 解析，禁用远端 Schema Fetch。

## 6. 数据模型与 Runtime 合同

以下均是拟议合同，字段应通过仓库生成器同步，不直接手改生成文件。

### 6.1 Surface 身份与持久化内容

| 对象 | 主要字段 | 规则 |
| --- | --- | --- |
| `UISurfaceRef` | `surface_id`、`revision`、`digest`、`catalog_id`、`catalog_digest` | 引用不可变已接受版本 |
| `UISurfaceOwner` | Workspace、Session、Thread、Turn、创建 Call ID | Runtime 注入，模型/MCP 不能指定或覆写 |
| `UISurfaceManifest` | Owner、Ref、模式、来源、正文引用、fallback、数据/组件摘要 | 存 CAS；不包含执行 Lease 或凭证 |
| `SurfaceMutation` | 请求身份、`expected_revision`、消息批次、结果 Ref | 原子变更；重复回放返回同一结果 |
| `UIInteractionContract` | Request ID、Surface Ref、字段 Schema、Schema Digest、到期时间 | 输入等待创建时冻结，不跟随浏览器状态变化 |
| `UIActionBinding` | Surface Ref、组件/动作身份、动作种类、允许字段与证据引用 | Runtime 根据允许规则绑定，不接受任意 Tool 名或 URL |

外部 Surface ID 只在来源局部有效。Runtime 分配规范身份并一致改写消息中的
Surface 引用，组件 ID 保留在本 Surface 内。
派生身份使用带域分隔的结构化输入和现有 ID/Digest 规范，不拼接未转义字段。
身份绑定不构成访问凭证，知道 Digest 也不能跨会话读取。

### 6.2 事件与查询

拟新增保留事件：

| 事件 | 内容 | 投影行为 |
| --- | --- | --- |
| `ui.surface.updated` | Surface Ref、前一 Revision、模式、来源、fallback、创建 Call ID | 同一 Surface 更新同一节点；不逐 Patch 新增卡片 |
| `ui.surface.deleted` | Surface ID、最后 Revision、结构化原因 | 移除活跃呈现，保留审计位置，禁用动作 |

大正文不塞进每个 Event；Event 只携带有界描述和 CAS 引用。
浏览器首次显示或发现 Revision 跳跃时，通过 Runtime 的拟议 `ui/surface/get`
查询精确版本。高频小更新可携带已校验增量，但必须能由同一 Ref 获取完整基线。
不得只存 Patch 而依赖客户端一直保留最初创建消息。

查询请求携带 `session_id`、Surface Ref 和请求对应的 Workspace 路由。
服务端先验证 Session/Thread 可见性及 Manifest 归属，再读取 CAS；
响应必须等于请求版本，不能悄悄返回最新版本。
文件或证据引用走各自已有权限检查，Renderer 不获得任意 CAS 或路径读取 API。

`event_traits.json` 必须给两个事件定义 Class、Item Owner、Durability、
Correlation 和 Terminal Trait，并同步 Go Event View、Web Projection、
轨迹展示、协议 Schema 和恢复筛选。它们不是业务 Terminal，也不表示执行进展。

### 6.3 工具入口

`present_ui` 的拟议参数：

| 字段 | 用途 |
| --- | --- |
| `surface_key` | 当前 Turn 内的逻辑身份，更新时复用 |
| `expected_revision` | 创建为零；更新需匹配已接受 Revision |
| `mode` | `display` 或阶段 3 的 `result`；不能创建 Input Wait |
| `messages` | 一份完整可校验的 A2UI 消息批次 |
| `fallback_text` | 纯文本降级摘要 |

工具 Executor 只返回提案；模型收到的最终工具回执由 Engine 在接纳后补齐
Surface 身份、Revision 或结构化错误，不回填整棵 UI。
更新自己的呈现产物属于会话状态变更，不是 Workspace 文件写；需要显式定义
Resource/Effect 和 Trusted Binding，允许合法 Plan Mode 场景，但不能借此写文件。
即使是只读呈现，也经过 Registry 和 Guard，不提供旁路。

工具提交与结果接纳共用正常的 Durable Effect 路径。`tool.Result` 拟增加明确的
`presentation` 类型字段，不能继续把任意 Metadata 键当作 UI 授权。
Engine 在 `ToolResultReceived` 接纳边界处理提案，不在 Provider Delta、
Tool 尚未完成或普通日志回调中发布。
接纳前不得把预期成功写入模型 History；接纳后的回执与恢复 Payload 使用同一版本。
若有效性校验发生在 Executor 返回后，最终 `IsError` 和回执必须在逻辑 Tool Result
接受时一起定稿，不能先闭合成功工具再补写一条矛盾失败。

`request_user_input` 沿用原工具名，扩展 `form`：

```json
{
  "prompt": "确认重构范围",
  "form": {
    "schema": {
      "type": "object",
      "properties": {
        "modules": {
          "type": "array",
          "items": {"type": "string", "enum": ["parser", "runtime"]},
          "uniqueItems": true,
          "minItems": 1
        },
        "keep_api": {"type": "boolean"},
        "constraints": {"type": "string"}
      },
      "required": ["modules", "keep_api"],
      "additionalProperties": false
    },
    "initial_values": {"keep_api": true},
    "labels": {
      "modules": "修改模块",
      "keep_api": "保留公开接口",
      "constraints": "其他约束"
    }
  }
}
```

这是 QCode Tool 输入示例，不是 A2UI 原始消息。Runtime 根据字段合同生成 A2UI。
`options` 和 `form` 互斥；不带 `form` 的简单提问沿用现有界面，不做历史兼容迁移。
首版字段 Schema 只开放一层对象、字符串、布尔、有限数值和枚举字符串数组，
支持 required/enum/长度/数值范围等明确规则，不开放任意递归 Schema、正则和脚本。
后续扩展需新增端到端合同测试，不能把完整 JSON Schema 任意能力透传给 Renderer。

### 6.4 类型化回答

为 `InputReplyPayload` 拟新增：

- `form_values`：经过受限 Schema 校验的 JSON 对象，保留布尔、数值和数组类型。
- `schema_digest`：回答对应的冻结字段合同。
- `surface_ref`：用户实际看到并提交的版本。

`form_values` 与旧 `answer` / `values` 回答形态互斥；空字符串、缺失、`false`、
`0`、空数组和 `null` 按 Schema 分别处理，不能靠 truthy/falsy 判断是否填写。
涉及跨语言精度的标识符和整数按字符串合同表达，不将 JSON 数字无条件转为 float。

同步扩展 `interact.Request/Reply`、Control Payload、RecoveredInteraction、
Tool Result、`input.required/resolved` 和持久化回答引用。
`input.required` 携带足够的字段合同/有界引用以便无 Renderer 客户端工作；
`input.resolved` 记录回答摘要与引用，不把整个表单和敏感值复制到每条事件。

## 7. 持久化、恢复与一致性

### 7.1 接纳顺序

UI 创建、更新和删除复用既有 Coordinator、CAS、Domain Fact 和 Terminal Outbox：

1. Tool/输入入口完成 Schema、来源和容量校验。
2. 规范化正文按 Digest Stage 到现有 CAS。
3. Coordinator 在接受 Tool Result 或 Input Request 的同一转换内保存引用、
   Revision、来源和确定性发布身份。Input Contract 必须和 Pending Input 一起接受。
4. 已接受事实再投影为 Event，发布失败作为可恢复投影处理。
5. 重启和 Terminal Outbox 根据同一事实补发，同一 Event ID 去重。

不允许“UI 已显示，但对应 Request 尚未持久化”的窗口。
CAS Stage 失败则本次 UI 不接受；已接受后广播失败不重新执行原工具。
外部工具若已产生副作用，附带 UI 校验失败只记录呈现诊断，保留工具真实结算，
不能把它变成可自动重试的工具失败。

UI 非权威不等于可以丢弃交互状态。Input 的字段合同与已接受回答属于正确性数据，
持久化失败时继续停留在原等待/恢复语义，不能假装提交成功。
纯显示的 Renderer 故障不影响 Turn 成功；`present_ui` 自身输入不合法则返回正常
可解释的 Tool 错误。

### 7.2 快照与历史窗口

Kernel 状态持有当前 Turn 的 Surface Ref 和待发布变更，复用既有 Domain Fact
恢复。为跨 Turn 查询拟增加一个可重建的 `ui_surface_revisions` 关系投影，
归属 `internal/persist`，不保存另一份正文或独立事件流：

| 列组 | 内容与索引要求 |
| --- | --- |
| 身份 | Workspace、Session、Thread、Turn、Surface ID、Revision，联合唯一 |
| 发布事实 | Event ID、Event Sequence、Manifest Digest、删除标志，Event ID 唯一 |
| 查询 | 按 Session/Thread 与 Sequence 定位围栏内各 Surface 最新版本 |
| 清理 | Session 删除路径显式覆盖，外键可用时同时约束，不仅依赖隐式级联 |

该投影由已接受并发布的 UI 事件重建，与既有事件投影提交边界保持原子性。
不能只保存“当前最新一行”，否则无法生成历史水位的一致 Snapshot。
读取围栏不得高于实际已完成的投影水位；投影缺失时按已接受事实修复，不让浏览器
拼接不同水位的 Owner、正文和 Pending Input。

在现有 `SessionPresentationSnapshot` 增加有界 Surface 摘要或 Manifest 索引引用，
并明确其与 `through_sequence` 使用同一读取围栏。
Surface 超出单次快照预算时分页加载 Manifest；不能只截断 Patch 或无限扩大
当前呈现快照上限。Active Input Contract 必须通过 Pending Input 查询完整可达。

客户端 Hydration：

1. 获取 Session Snapshot 和水位。
2. 先安装水位对应的 Surface 基线，再按序应用高于水位的 Event。
3. 仅为可见 Surface 拉取正文；取得正文前可展示 fallback。
4. 旧版本正文晚到时不得覆盖更新版本；切换 Session/Workspace 取消旧请求。
5. Retention Gap 复用现有 Desync/重新 Hydrate，不猜测缺失 Patch。

当前读取 Snapshot 的事件裁剪逻辑不足以保证新 Surface 完整，需要明确增加这部分，
不是仅在客户端增加一个消息类型。

### 7.3 存储规模与生命周期

初版每个已接受 Revision 可保存一份有界完整规范化正文，先保证恢复正确；
只存引用到 Turn 状态，不能在每个后续 Domain Fact 中重复嵌入正文。
相同 Digest 的纯显示更新视为无变化，不重复写正文或刷新进展。
UI 更新与模型/工具结果边界对齐，不按字符、动画帧或浏览器输入事件写数据库。

阶段 0/1 必须测量完整快照方案的写放大。若在显式预算内不可接受，再将 Manifest
拆成共享组件/数据块；不要预先构建第二套通用 Patch 存储引擎。
Surface 总字节、数量和修订总量都有显式公开限额，不能只限制单个消息。

Session 删除/Discard 在既有事务中清理所属 Surface 关系投影及回答引用，
审计事件保留策略沿用当前产品约定。CAS 可达性计入尚保留的审计/Terminal 引用，
不得将“删除 Session”宣传为物理擦除所有历史内容。
若现有 CAS 回收尚不覆盖新引用，先补所有权与回收测试，不新增孤儿对象清理脚本绕过。

### 7.4 状态与恢复规则

| 场景 | 规定行为 |
| --- | --- |
| 重复发布/恢复相同 Effect | 复用已接受结果和 Event ID，不产生新卡片或新 Revision |
| 相同幂等身份携带不同内容 | `conflict`，不覆盖旧结果 |
| 更新 Revision 过期 | 保留原值，返回当前 Ref；不自动覆盖 |
| Display 所属 Turn 结束 | 冻结成历史呈现，不接收迟到更新 |
| Input 所属 Turn 等待 | 同一 Request/Schema/Surface 保持不变，不动态替换用户正在填写的合同 |
| 关闭浏览器/断线 | 不自行取消 Turn；Input 按既有到期和恢复规则处理 |
| 用户取消 Turn | Pending Input 与动作失效；晚到回答不能恢复已取消 Turn |
| Input 到期 | 服务端时间裁决；不能由浏览器延长 TTL |
| 终态 Result 继续处理 | 引用旧结果创建新 Turn，不复活旧 Turn |
| Fork/Checkpoint Restore | 可保留只读历史引用，不复制可执行动作或原 Request 的授权 |
| 删除/归档 Session | 拒绝提交新动作；延迟请求不能复活 Session |
| Child Thread 产物 | 留在 Child 执行块，原 Owner 不变；不创建独立 Session |

Child 当前没有 Input Host，本方案不改变这一点。Child 可以发布只读结果；
需要用户决策时通过已有结构化结果交给 Parent，由 Parent 创建自己的 Input Wait。
Parent 引用 Child 证据必须校验父子关系，不能改写产物 Owner。

## 8. 交互、授权与后续任务

### 8.1 动作分类

| 动作 | 执行位置 | Runtime 行为 |
| --- | --- | --- |
| 排序、筛选、展开、编辑草稿 | 浏览器 | 不发送 Operation，不调用模型 |
| 提交表单 | `input.reply` | 校验冻结合同，恰好一次消费当前 Request |
| 打开文件/证据 | 现有受约束查询或导航 | 校验归属与资源，不能拼任意 URL |
| 提议继续处理 | 打开现有 Composer 并展示选择摘要 | 不自动发送、不直接执行工具 |
| 用户确认继续处理 | 普通 `turn.start`，必要时走现有排队机制 | 创建新 Turn，重新检查当前状态、计划和 Guard |
| Approve/执行命令/任意 RPC | 不在 Catalog 中 | 结构化拒绝，不转成字符串命令 |

不新增万能 `ui.action.execute` API。
Web Action Router 按确定性动作类型匹配，不将动作 `name` 当作 Tool 名、URL 或
宿主函数名动态执行。

### 8.2 表单提交事务

1. Host 继续验证同源和 Capability Token，将请求交给 OperationService。
2. Runtime 绑定 Request 的真实 Workspace/Session/Thread/Turn，拒绝伪造归属。
3. 校验 Request 仍 pending、未到期，且 Surface Ref 与 Schema Digest 一致。
4. 严格校验字段、选项身份和白名单；客户端校验只用于体验，不是安全边界。
5. 使用 Operation 幂等键和 Request Ledger 保存规范化回答并消费等待。
6. 持久化成功后才 Resume 原 Input Effect，并将同一份类型化值返回模型。
7. 事件补发与恢复不能造成第二次提交或再次执行已闭合工具。

字段错误不消费 Request、不创建新 Request，只返回字段 JSON Pointer、
规则代码和可显示的消息，允许原位纠正。
两个浏览器提交由服务端决定唯一胜者；同幂等键同载荷返回原 Receipt，
另一份回答或另一幂等键消费已解决 Request 返回冲突。
网络响应丢失时用原幂等键查询/重试，不生成新的提交身份。

### 8.3 结果后续处理

阶段 3 为 `StartTurnPayload` 拟增 `ui_context`：

- 源 Surface Ref、稳定条目 ID、允许的用户选择和用户补充说明。
- 在创建 Turn 的 admission 边界校验归属、条目存在性、Schema 和来源；
  不信任客户端复制的完整表格或隐藏参数。
- 规范化选择随已接受请求持久化，用户消息展示相同摘要；不能只保存一句
  “继续处理”导致历史和恢复丢失范围。
- 排队时保存不可变选择，在实际开轮时再次检查 Source 可达性和 Workspace 状态。
- 源数据过期时明确显示失效或重新查询，不自动扩大所选集合。

Source Ref 是上下文，不是审批或事实升级。进入模型的外部文字带来源和非权威边界，
当前 Workspace 与证据新鲜度仍由普通 Turn 检查。
正在运行的 Turn 不接收任意 Result Action 作为暗中 Steer；需要中途决策时只能
创建 Input Wait。用户明确选择排队则复用普通队列合同。

### 8.4 错误合同

沿用 `protocol.Problem` 的顶层错误码，扩展有枚举、Schema 和测试的 UI 原因及详情。

| 顶层 Code | 拟议 Reason | 恢复动作 |
| --- | --- | --- |
| `invalid_argument` | `ui_schema_invalid`、`ui_binding_invalid`、`ui_action_not_allowed` | 按字段详情修正，不原样重试 |
| `conflict` | `ui_revision_stale`、`ui_owner_mismatch`、`ui_input_already_resolved` | 刷新权威状态或停止提交 |
| `unavailable` | `ui_catalog_unavailable`、`ui_renderer_unavailable` | 使用降级呈现，不能扩大目录 |
| `resource_exhausted` | `ui_capacity_exceeded` | 缩小结果或分页，不截断为貌似完整结果 |
| `deadline_exceeded` | `ui_input_expired` | 结束该等待，按既有 Turn 恢复策略处理 |

错误详情包含 `surface_id`、当前 Revision、字段 Pointer、限制字段和恢复动作，
不包含原始敏感值。错误文案可以本地化，业务分支只依据 Code/Reason。
UI 修复不拥有独立无限模型循环，复用现有 Tool 错误反馈和显式预算。

## 9. 安全模型

### 9.1 信任边界

| 输入来源 | 信任级别 | 接纳方式 |
| --- | --- | --- |
| 模型生成组件/文字 | 不可信 | Schema/容量/引用校验，禁止可执行代码 |
| Skill 指令和模板 | 有来源、仍非授权 | 既有 Lock/Digest 和发现机制，不能注册权限 |
| MCP 描述和结果 | 外部不可信 | 冻结 Source Binding，UI 接纳与工具权限分别判断 |
| 浏览器值和动作 | 不可信 | Owner/Revision/Request/Schema/幂等校验 |
| Runtime Receipt/验证事实 | 已验证来源 | 按受约束引用读取，不允许模型覆盖 |
| 本地打包组件实现 | 受信任代码 | 供应链、审查、测试和发布约束 |

### 9.2 必须覆盖的威胁

- 组件或字段注入脚本、危险 URL、原型污染、外部 `$ref` 或跨 Surface 数据路径。
- 巨型数组、深层结构、循环组件、重复 ID、消息洪泛导致浏览器或服务端耗尽。
- 伪造审批区、隐藏风险、覆盖宿主导航、全屏遮挡或用文本冒充 Runtime 证据。
- 操作旧 Revision、复用过期 Request、跨 Workspace/Session/Child 投递动作。
- 借诊断引用读取未授权文件、CAS 或其他会话内容。
- 借 `sendDataModel`、远端资源或全量日志泄露用户输入和工具数据。
- MCP 模板提供的工具调用绕过 Trusted Binding，或者供应方变更导致权限扩大。

组件容器由宿主管理，不能覆盖工作台的审批/输入/导航区域。危险动作始终位于
原有可信 UI 中。受限 JSON 只是缩小攻击面，不构成“无 XSS、无注入”的保证。

### 9.3 凭证与数据最小化

首版表单禁止收集 API Key、Token、密码等原始凭证；需要凭证时引导现有 Connection
设置与 Keyring 流程，UI 只携带受约束引用。
拒绝明确的 secret 字段类型不等于能识别用户在普通文本框粘贴的全部秘密，因此提交
前提示数据用途，并沿用现有脱敏和存储访问控制。

草稿默认只在当前浏览器内存保留，跨折叠/虚拟化保持，刷新或关闭后不承诺恢复；
不写 LocalStorage，不每次按键同步服务端。已提交回答进入既有受控持久化。
日志/指标只记录类型、大小、Digest、错误码和关联身份，不记录表单原文。
外部 MCP 的后续调用只接收本次允许的字段，不转发整个会话或其他 Surface 数据。

## 10. Web 渲染与性能

### 10.1 接入形式

- 在 Conversation Projection 增加稳定 `surface` 节点，关联创建 Call ID 与消息位置。
- 每个 Owner Scope 管理自己的 Processor；相同外部 ID 不共享状态。
- 在独立适配组件中接入 Renderer，不把第三方 Processor 状态扩散到整个 App。
- 仅第一次可见 Surface 才动态加载 A2UI 依赖；未启用/纯文本会话不付解析和加载成本。
- 通过已验证消息/引用更新 Processor，避免将整份 Event History 每帧重新输入。
- 已完成 Turn 的 Surface 按原执行过程折叠规则展示；有最终结论时保留可发现的结果入口，
  不把整个执行过程强制展开。
- 搜索/Trajectory 定位到 Surface 时展开对应执行块并定位，不改变原有导航语义。

### 10.2 更新与输入隔离

组件 ID、行 ID 和回调引用必须稳定。普通文本流不能使历史 Surface 失效，
一个数据字段变化只通知相关订阅者。
同一动画帧可以合并发布纯显示更新，但不能丢弃删除、Input 接受/解决和终态。
正在编辑的表单 Schema 固定；输入草稿和服务端 Data Model 分离，不能被迟到 Patch 覆盖。

异步加载以 Owner、Revision、Digest 为键去重，并在卸载/切换时取消或丢弃过期结果。
虚拟化卸载只卸载渲染，不丢当前待提交草稿；Session 删除和 Request 终结再清理。
Reader 位置、文本选择、按钮焦点优先于自动滚动，沿用现有交互锚定规则。

### 10.3 容量合同

新增统一的 `ui` 配置域与 `UILimits`（拟议），由 Runtime 广告有效值：

| 限制字段 | 覆盖内容 | 值的来源 |
| --- | --- | --- |
| `max_update_bytes` | 单批次规范化更新与解析前输入 | 显式配置，并受 Transport 编码后上限约束 |
| `max_surface_bytes` | 一个完整可恢复 Surface 正文 | 显式配置，与查询/存储可承载范围共同校验 |
| `max_components`、`max_depth` | 组件和 JSON 解析复杂度 | 阶段 0 的攻击测试与资源预算形成公开安全合同 |
| `max_surfaces_per_turn` | 单 Turn 产物数量 | 显式配置，区分存储数量与可见渲染数量 |
| `max_ui_bytes_per_turn`、`max_revisions_per_surface` | 总存储和写放大 | 显式配置，不能只限制单次消息 |
| `max_form_fields`、`max_action_bytes` | 字段与提交大小 | 显式配置及现有请求上限 |
| `max_resident_bytes` | Browser Processor 和正文缓存 | 客户端声明能力与产品配置取较小值 |

实验阶段配置缺失或非法时不得启用新能力，不随手填经验默认值。
阶段 0 输出带设备、负载、测量出处和边界测试的建议值；正式默认值在阶段 5
经文档与契约评审后登记。不能将 Go Host 的常量直接导入无依赖的 Protocol 包，
由构造期注入有效 Transport 限制。

请求、事件、完整查询响应均按真实序列化后的字节数检查，计入信封和 JSON 转义开销。
当前 Web 的 JSON Body、WebSocket Frame 与 History Snapshot 上限并不相同，
不得假设“请求能接收，事件就能发送”。超限返回结构化错误或明确分页，
不静默截断字段、表格或恢复状态。

模型上下文预算继续来自真实 Model Capability 与既有 Context Admission。
不引入 UI 专用模型档位、固定 Token 截断或隐藏百分比。

## 11. Skill 与 MCP 接入边界

### 11.1 Skill

先让既有 Skill 通过 `present_ui` / `request_user_input` 使用同一目录，
不要求所有 Skill 改 Manifest。
可复用模板作为声明式资源沿既有 `skills_read`、Lock、Digest 和冻结 Handle 路径读取，
不默认读取/执行 Skill 随附的 React 或 JavaScript。
目录说明按需要加载并可缓存，不为每个 Skill 重复注入整套 Catalog。

业务接入只依赖发布工具和版本化模板合同，不要求使用方导入
`internal/runtime/agent` 或持久化内部包。只有真实多处重复出现后才提供薄 Builder SDK，
不先创建通用 UI 工作流 SDK。

### 11.2 MCP

阶段 4 单独定义并锁定 A2UI-over-MCP Binding，对照上游该版本的示例与 Schema：

1. 审核并补齐 Metadata、资源 MIME、结构化数据和能力协商字段；必要的 MCP 版本升级
   单独验证，不因存在 `StructuredContent` 就宣称扩展兼容。
2. 只接纳绑定明确、MIME 为 `application/a2ui+json` 的声明式资源/消息；
   该 MIME 出现在哪个合法 MCP 字段由冻结 Binding 决定，不发明通用猜测规则。
3. 普通 `structuredContent` 不自动解释为 UI；文本中的相似 JSON 保持文本。
4. 模板获取复用受治理资源工具和既有连接，不由 Browser 直连 MCP Server。
5. Source 绑定 Server 身份、工具身份、模板 Digest 与 Catalog，缓存不能跨来源污染。
6. UI 只获得数据呈现资格，不能提升外部工具的 Trusted Binding。

上游示例中的“客户端动作直接作为 tools/call”不直接移植到 QCode。
首个外部版本只支持只读展示、本地选择以及用户确认后的普通新 Turn。
若未来必须直接调用指定 MCP 工具，需另行设计显式受治理操作合同和审批；
不能在此阶段添加一个通用 Action-to-Tool 转发器。

普通工具结果保留原 Content、IsError 和真实执行结算；UI 校验失败按来源保留结构化
诊断和原文本，不丢正常结果。外部断线、Schema 不兼容或能力被禁用只关闭该来源的 UI。

## 12. 分阶段实施任务

各阶段状态目前均为“未开始”。阶段交付后在本节记录测试证据并更新状态，
不要仅凭演示截图改成完成。

### 阶段 0：协议与依赖验证

**目标**：用最小实验确定协议、依赖和成本成立，不修改产品行为。

任务：

- [ ] 固定目标上游 Schema、许可证、Digest、React/Web Core 包版本和版本对应关系。
- [ ] 验证 React 18、Vite、CSP、懒加载、订阅清理和 ErrorBoundary 兼容。
- [ ] 定义 QCode Catalog、受限 Schema Profile 与共享 Go/TS 校验向量。
- [ ] 用现有 E2E Fixture 框架跑只读表格、表单和无 Renderer 降级。
- [ ] 建立 Token、加载大小、渲染、内存、写入和恢复基线，登记容量建议值来源。
- [ ] 形成准入结论；不通过时保留结论文档，移除本阶段引入的未使用实验依赖。

验收：协议样本两端一致、无新增宿主权限、版本可固定，且已有明确的下一阶段配置。
不通过则停止；不以强制升级 React 或自行重写全协议作为默认补救。
测试产物只放既有测试位置或 `.tmp`，不新增生产第二服务。

### 阶段 1：可回放的只读界面

**目标**：实现最小纵向链路，而不只做浏览器 Demo。

任务：

- [ ] 增加协议 Ref/Manifest/事件、`UILimits` 与显式开关。
- [ ] 实现 A2UI 校验 Adapter、只读 Catalog、`present_ui` 和 Typed Result 提案。
- [ ] 在 Coordinator 接纳、CAS、Domain Fact、Outbox 和恢复中持久化引用。
- [ ] 增加 Runtime 精确版本查询和有水位的 Session Snapshot 基线。
- [ ] 实现 Web Projection、懒加载 Renderer、fallback、历史位置和来源显示。
- [ ] 覆盖重复、断线、重启、删除 Session、跨 Workspace 与无效组件。
- [ ] 验证 UI 更新不会改变进展 Lease、执行清单、验证状态和 Turn 完成条件。

验收：真实普通 Turn 能发布列表，刷新/重启后仍是同一份列表；
坏 UI 不破坏聊天；主/子 Thread 归属正确；不具备任何执行型 Action。
每个显示界面必须有正文结论或文本降级，不以 `present_ui` 替代现有非空最终答案规则。

### 阶段 2：可恢复的结构化输入

**目标**：一次性获取完整类型化回答，并在恢复后继续同一 Turn。

任务：

- [ ] 扩展 `request_user_input` 的字段合同及确定性 A2UI 表单编译。
- [ ] 扩展 Input Request/Reply、Control、Kernel、恢复 Payload 和协议事件。
- [ ] 实现 Schema Digest/Surface Ref 校验、字段错误和提交幂等。
- [ ] 实现输入草稿、可访问性、移动端排版和无 Renderer 的原生表单降级。
- [ ] 覆盖等待重启、接受后响应丢失、双浏览器竞争、超时、取消与晚到回答。
- [ ] 用多模块重构确认 Fixture 比较往返次数、歧义和输入纠错。

验收：布尔、零值、多选和自由文本正确往返；输入只消费一次；
Renderer 被关闭也能完成已有等待；Child 仍不能直接请求用户输入。
达到此阶段即可开放首批内置场景，不必等待外部 MCP 接入。

### 阶段 3：证据驱动的结果交互

**目标**：用户可以选择结果并明确发起后续任务，而不增加隐式执行路径。

任务：

- [ ] 增加选择表格、文件/Receipt 引用和本地筛选，复用确定性证据组件。
- [ ] 为 Result 定义固定动作绑定和来源/新鲜度规则。
- [ ] 扩展 StartTurn/Queue 的 `ui_context`，接入 admission 和用户消息/恢复上下文。
- [ ] “继续处理”先进入 Composer，显示实际选择；用户发送后才创建普通 Turn。
- [ ] 覆盖源 Revision 不符、文件变化、归档、删除、排队期间变化和恶意隐藏字段。
- [ ] 完成诊断结果选择到普通受治理修复 Turn 的 E2E。

验收：勾选结果不执行任何副作用；点击准备动作也不执行；
用户确认的新 Turn 保留精确选择，仍通过原规划、审批、Journal 和 Sandbox。
模型描述不能获得与真实 Receipt 相同的可信呈现。

### 阶段 4：Skill 与 MCP 接入

**目标**：证明长尾能力可以复用通用界面，而不是增加新的硬编码分支。

任务：

- [ ] 编写接入合同和声明式模板示例，不要求业务方导入 Runtime 内部包。
- [ ] 验证两个不同领域的能力复用现有 Catalog，不改 App 的领域分支。
- [ ] 冻结 MCP UI Binding，补齐所需协议字段与协商测试。
- [ ] 接入受治理模板获取、结果 Typed Presentation、来源缓存和失效规则。
- [ ] 默认关闭外部 UI，支持按来源显式启用并独立撤销。
- [ ] 覆盖未知 Catalog、变更 Digest、恶意动作、断线和普通结果降级。

验收：增加声明式模板即可获得新组合；外部来源不能注册执行代码、改审批或越权；
不将本阶段能力标为完整 MCP Apps 或任意 A2UI-over-MCP 互操作。

### 阶段 5：发布与长期运行

**目标**：决定哪些能力可默认开启，形成可支持的产品合同。

任务：

- [ ] 完成端到端故障注入、长会话、多 Workspace 和双客户端测试。
- [ ] 执行前端加载、内存、输入响应、持久化写放大与恢复时间对比。
- [ ] 固定公共默认限制及出处；测试配置缺失、零值、负值、边界和超限。
- [ ] 完成依赖许可证、SBOM、漏洞检查和打包资源验证。
- [ ] 实测关闭渲染、关闭新发布、关闭外部来源后的降级与恢复。
- [ ] 更新中文产品指南、协议说明、可靠性矩阵和发布事实。
- [ ] 经真实试点数据判断是否默认开启；达不到收益则保持 opt-in 或停止扩张。

验收：没有未闭合的正确性/安全问题，性能符合已登记预算，已发布交互可恢复，
且用户能在不安装外部服务的前提下完成首批场景。

## 13. 测试矩阵与验证命令

### 13.1 最低覆盖

| 层次 | 必测内容 |
| --- | --- |
| Schema/Parser | 各消息形态、未知字段、重复键、无效 Pointer、数值精度、原型污染 |
| Catalog | 组件/函数白名单、悬空引用、循环、字段覆盖、相同 ID 不同 Owner |
| Kernel | 发布/输入接纳原子性、Revision、幂等、Cancel 竞争、UI 不改变业务进展 |
| Persistence | 每个 Stage/Commit/Publish 边界崩溃、Reopen、Outbox 去重、CAS 缺失/损坏 |
| Lifecycle | Session 删除/归档、Fork、Restore、Child 结果、来源禁用、不复活旧动作 |
| Transport | 鉴权、跨归属、编码后字节上限、版本协商、Snapshot 水位与迟到请求 |
| React | 独立订阅、纯文本不加载 Renderer、草稿不被覆盖、引用稳定、订阅释放 |
| 交互 | 空值/false/0/多选、双客户端、字段错误、响应丢失、网络重连和超时 |
| 安全 | 假审批、非法引用、外部代码/资源、未知动作、权限不扩张 |
| E2E | 正常完成、刷新、重启恢复、历史回放、移动端、键盘/读屏、主题与焦点 |
| 性能 | 多 Surface、长表格、最大合法输入、连续 Patch、历史裁剪、销毁后内存回落 |
| MCP | 协商、MIME/Metadata、结果失败与 UI 失败分离、模板来源和撤销 |

必须覆盖的故障时序：

1. CAS Stage 后、Domain Fact 前崩溃：没有可交互但未接受的 Surface。
2. Domain Fact 后、Event 前崩溃：恢复补发一次，不重新执行 Tool。
3. Input 接受前断线：相同提交身份可以重试，原等待不被误消费。
4. Input 接受后、响应或 Resume 前崩溃：恢复原回答，不重新向用户提问。
5. Cancel 与 Submit 并发：单一线性化结果，不同时出现完成与取消生效。
6. Snapshot 与 Update 并发：围栏前后状态明确，无 Patch 缺口或旧版本覆盖。
7. 外部副作用完成但 UI 无效：保留副作用结算，不自动重试原调用。

### 13.2 现有可用命令

按变更范围选择，不把全量发布门禁用作日常每一步的前置条件：

```bash
go test ./internal/runtime/protocol
go test ./internal/adapter/tool/interact
go test ./internal/runtime/agent/turnkernel
go test ./internal/runtime/agent/engine
go test ./internal/runtime/app/...
go test ./internal/persist/...
go test ./internal/adapter/mcp/... ./internal/adapter/tool/mcp/...
go test ./internal/host/runtimeapi/web ./internal/host/web
make protocol-schema
make web-protocol-check
npm --prefix web run check
npm --prefix web test
npm --prefix web run test:e2e -- streaming.spec.ts
make docs-check
make book-check
git diff --check
```

新增包/Fixture 建好后再添加其命令到 Makefile 与开发指南；不在文档中声称拟议命令
已经存在。集成版本按影响面运行 `make security-test`、`make reliability-gate`、
`make web-performance`、`make web-streaming-soak` 以及正式发布门禁。
环境不满足的测试必须明确报告，未执行不能标为通过。

## 14. 开关、兼容与退出

拟议开关分别控制“允许新发布”“允许新表单”“允许结果后续入口”“允许外部来源”。
Renderer 本地故障可降级显示，但不能修改 Runtime 的启用配置。

关闭能力时：

- 新 Turn 不再广告被关闭的工具能力，新输入回退原简单输入。
- 已存在 Surface 仍可读取 fallback；已接受表单继续按冻结 Schema 提交或显式取消。
- 不因关闭 UI 重复执行工具、改变终态或丢弃用户已提交回答。
- 禁用外部来源后不得再获取其模板或发出新请求；历史只读快照仍受原访问控制。
- 关闭新 UI 生产能力与删除 Renderer/Schema 代码不是同一步。

首次公开稳定发布前，不为历史开发数据库编写无需求的兼容迁移。
如果实现确需新增关系表/公开 Schema，则遵守仓库当前事务迁移约定，
初始化直接生成最新结构；在实际合入时分配版本，本文不预占下一个 Schema 号。

回退优先采用同一二进制关闭能力，不承诺旧二进制能读取未知 Event/Schema。
软件降级只有在真实前版本读取测试通过后才允许，否则按正式备份/恢复和发布策略处理，
不能删除新事件或手工改数据库来伪造兼容。
任何更新或重启前检查全部 Workspace 的活动 Turn/Operation，并取得用户明确同意。
本文及文档交付阶段不启动、重启或替换运行中的 QCode。

## 15. 工作量与推进方式

以下为排期估算，不是性能阈值或交付承诺。假设一名熟悉 Runtime 的 Go 工程师、
一名熟悉现有 Web 的工程师协作；不包含额外领域系统开发。

| 阶段 | 估算工程人日 | 主要不确定性 |
| --- | --- | --- |
| 协议与依赖验证 | 3-5 | 实际包版本、Peer Dependency、受限 Catalog 适配 |
| 可回放的只读界面 | 8-12 | Surface 持久化、快照围栏、补发与回收 |
| 可恢复的结构化输入 | 7-10 | 回答事务、恢复、双客户端和降级 |
| 证据驱动的结果交互 | 5-8 | 证据引用和新 Turn/Queue admission |
| Skill 与 MCP 接入 | 6-10 | MCP 版本、Metadata、来源治理和模板协商 |
| 发布与长期运行 | 5-8 | 故障覆盖、性能和供应链问题修复 |

合计约 34-53 工程人日。跨层联调不能按人数简单折半；阶段 0 后重新估算。
推荐首次承诺范围为阶段 0-2，约 18-27 工程人日，交付可恢复的动态界面和输入闭环；
阶段 3 完成后再决定外部生态投入。

每个实施变更按语义拆分且保持可构建：

1. 合同与共享校验 Fixture。
2. Runtime 接纳、持久化、恢复与查询。
3. Renderer、降级与现有对话集成。
4. 输入闭环。
5. 结果后续处理。
6. 外部来源接入和发布验收。

合同未实现时保持能力关闭，不暴露半成品交互。Review 重点是状态归属、故障窗口、
授权和恢复，不以新增组件数量衡量完成度。

## 16. 文档与实施完成定义

本方案仅在开发导航下以“待实施方案”链接，不写成现有产品特性。
随阶段交付将真实行为同步到：

- [架构设计](./architecture.md)：所有权、协议、Input 与 Surface 恢复。
- [配置说明](./configuration.md)：开关、容量、默认值来源和降级。
- [使用指南](./usage.md)：真实已交付场景、数据提交边界。
- [安全模型](./security.md)：Catalog、来源、输入、链接和执行隔离。
- [本地开发](./development.md)：生成命令、Fixture、测试与依赖验证。
- [后续规划](./roadmap.md)：只保留尚未交付的目标。

知识书籍受影响章节在实际实现时根据 `docs/book/governance.json` 识别，
按 `docs/book/catalog.json` 更新中文正文和状态，再运行 `make book-navigation`；
本次不提前修改章节状态、不创建空章节。

每阶段完成必须同时满足：

- [ ] 受支持 Web 入口可到达，或阶段 0 有明确实验结论。
- [ ] 合同、错误、Cancel/Retry/Recovery 行为与实现一致。
- [ ] 无新权限旁路，无第二套业务状态机。
- [ ] 相应正确性、安全、性能、降级测试有可检查证据。
- [ ] 文档、生成协议和已交付状态同步。
- [ ] 未覆盖场景和环境限制明确记录。

## 17. 外部依据

以下资料于 2026-09-11 核对；上游页面会演进，阶段 0 仍需固定实际使用版本。
官网的生产版本标注不等于 QCode 接入已达到生产要求。

1. [A2UI 定位](https://a2ui.org/introduction/what-is-a2ui/)。
2. [版本状态](https://a2ui.org/)。
3. [v0.9.1 协议](https://a2ui.org/specification/v0.9.1-a2ui/)。
4. [Renderer 与 Web Core](https://a2ui.org/guides/renderer-development/)。
5. [动作与数据同步](https://a2ui.org/concepts/actions/)。
6. [A2UI over MCP](https://a2ui.org/guides/a2ui_over_mcp/)。

采用理由是 QCode 的动态交互需求，不是跟随竞品。Codex/Cursor 具备结构化 UI
不能证明其原生采用 A2UI；MCP Apps 支持也不能等同于 A2UI 原生支持。
