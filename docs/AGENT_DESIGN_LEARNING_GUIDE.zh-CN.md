# Reasonix Agent 设计学习指南

这不是一份“功能说明书”，而是一份面向维护者的学习文档。
它要解决的问题是：当你开始深入 `Reasonix` 代码时，如何不被大量目录和实现细节淹没，而是先建立正确的心智模型，再回到具体代码里验证它。

这份文档只聚焦 `agent` 主线，阅读范围主要是：

- `cmd/reasonix/main.go`
- `internal/cli/cli.go`
- `internal/boot/boot.go`
- `internal/control/*`
- `internal/agent/*`
- `internal/tool/*`
- `internal/provider/*`
- `internal/permission/*`
- `internal/memory/*`
- `internal/skill/*`

不展开 desktop、bot、site、workers 的产品层实现。

## 先用一句话理解这个项目

如果你把 `Reasonix` 看成“一个聊天界面”，你会很快迷路。

更准确的理解是：

`Reasonix` 是一个 **执行型 coding agent 系统**。它把一个用户任务拆成下面几层：

- **入口层**：接收命令行、桌面端或 HTTP 的输入
- **装配层**：根据配置把 model、tool、permission、memory、skill 装配成可运行系统
- **控制层**：负责会话生命周期、审批、plan mode、输入分流、checkpoint、goal
- **执行层**：真正驱动“一轮任务”的核心循环，也就是 `agent.Agent`
- **能力层**：provider、tool、memory、skill、history 等被执行层调用的能力面

所以你在读代码时，不要先问“这个页面怎么渲染”，而要先问：

- 输入是怎么变成一次任务的？
- 谁负责装配运行环境？
- 谁负责真正和模型对话？
- 谁负责把工具暴露给模型？
- 谁负责保持会话可恢复、可压缩、可审批？

只要这五个问题清楚，后面的细节都会落位。

## 先建立正确心智模型

### 1. `boot` 不是执行器，它是装配器

先看 `internal/boot/boot.go:Build`。

这个函数的职责不是“跑一轮 agent”，而是把配置解析成一个完整可运行的 `Controller`：

- 解析 model/provider
- 组装 system prompt
- 装配 memory 和 skills
- 构建 `tool.Registry`
- 注入 built-in tools、MCP tools、task/skill tools
- 创建 `agent.Agent`
- 如有需要，再包一层 `agent.Coordinator`

也就是说：

- **`boot` 负责把系统搭起来**
- **`agent` 负责把一轮任务跑完**

这两个职责是分离的，这是整个项目最重要的边界之一。

### 2. `Controller` 不是模型循环，它是会话 orchestration

看 `internal/control/controller.go:Controller`。

`Controller` 不是单纯的 UI 层，也不是模型 API 封装。它是一个 **transport-agnostic session driver**：

- 接受 `Submit` / `Send` / `RunTurn` 等命令
- 维护当前 session 的生命周期
- 处理中断、审批、plan mode、slash command、shell shortcut、goal、checkpoint
- 把事件通过 `event.Sink` 发给前端

所以如果你问：“用户输入到底先到哪里？”
答案通常不是 `agent.Agent.Run`，而是先经过 `Controller`。

### 3. `Agent` 只关心三件事：`Provider`、`Registry`、`Session`

看 `internal/agent/agent.go:Agent` 和 `internal/agent/session.go:Session`。

`Agent` 的核心抽象极简：

- `provider.Provider`：模型后端
- `tool.Registry`：模型当前可调用的能力表面
- `Session`：当前对话日志

这就是这个系统设计上很漂亮的一点：
`Agent` 不需要知道你是 CLI、desktop 还是 HTTP，也不需要知道 MCP 配置文件怎么读；那些都在 `boot` 和 `control` 解决了。

### 4. `Session` 不是“缓存对象”，而是事实来源

看：

- `internal/agent/session.go`
- `internal/agent/save.go`
- `internal/provider/provider.go:NormalizeMessages`
- `internal/agent/compact.go`

`Session.Messages` 不是临时中间态，而是执行层真正持有的会话事实。

这意味着：

- provider 请求直接从它读取
- compaction 直接重写它
- save/load 直接持久化它
- prune / normalize 都是在保护这份日志的可用性

理解这一点以后，你就会明白为什么很多设计都围绕“如何安全地改写消息历史”展开。

## 文本知识图谱

这一节不用图，而用“节点 + 关系”的方式写知识图谱。每个节点都绑定到真实代码。

### 节点 1：CLI 入口

- **关键文件**：`cmd/reasonix/main.go`、`internal/cli/cli.go`
- **关键符号**：`main`、`cli.Run`、`runAgent`、`setup`
- **它解决什么问题**：把不同启动方式（`run` / `chat` / `serve` / `config` 等）路由到不同执行路径
- **它依赖谁**：`boot.Build`、`control.Controller`
- **谁依赖它**：终端用户入口
- **它故意不负责什么**：不负责真正的模型循环，也不负责 tool 注入细节

你可以把它理解成“入口分发器”。
真正的核心价值在于：它尽量早地把“子命令分流”和“交互模式分流”做掉，然后把系统搭建交给 `boot`。

### 节点 2：Boot 装配层

- **关键文件**：`internal/boot/boot.go`
- **关键符号**：`Build`、`addBuiltins`
- **它解决什么问题**：把配置解析成一个完整可运行的 `Controller`
- **它依赖谁**：`config`、`provider`、`tool`、`plugin`、`memory`、`skill`、`permission`
- **谁依赖它**：CLI、desktop、serve 等前端入口
- **它故意不负责什么**：不负责会话期内的输入分流和单轮执行

你应该重点看这些代码落点：

- `internal/boot/boot.go:Build`：总装配入口
- `internal/boot/boot.go` 中的 `skill.ApplyIndex(...)`：skill 索引进 prompt
- `internal/boot/boot.go` 中的 `tool.NewRegistry()`：tool registry 初始化
- `internal/boot/boot.go` 中的 `addBuiltins(...)`：built-in tools 注入
- `internal/boot/boot.go` 中的 `agent.New(...)`：真正生成 executor
- `internal/boot/boot.go` 中的 `agent.NewCoordinator(...)`：双模型模式下加 planner

**设计理念**：系统是 config-driven assembly，而不是把工具、模型、权限写死在入口里。

### 节点 3：Control 控制层

- **关键文件**：`internal/control/controller.go`、`internal/control/input.go`、`internal/control/auto_plan.go`
- **关键符号**：`Controller`、`New`、`Submit`、`submit`、`submitCommandOrTurn`、`Compose`、`SendWithRaw`
- **它解决什么问题**：把用户输入转成正确的一轮执行，并维护会话控制语义
- **它依赖谁**：`agent.Runner`、`permission`、`checkpoint`、`hook`、`memory`、`jobs`
- **谁依赖它**：所有前端
- **它故意不负责什么**：不直接管理模型协议，不直接定义工具 schema

这里最值得学习的不是“它做了多少事情”，而是“它把哪些事情拦在 `Agent` 外面”。

例如：

- `Submit` / `submit` 先识别 memory quick add、goal command、`!` shell shortcut、slash commands
- `Compose` 把 plan mode 和其他控制信息拼成“发给模型的最终文本”
- `auto_plan.go` 负责启发式意图识别和可选 classifier

这说明一个重要设计选择：
**输入解释、会话策略、审批与 UI 语义放在 `control`，不要塞进 `agent` 主循环。**

### 节点 4：Agent 执行层

- **关键文件**：`internal/agent/agent.go`
- **关键符号**：`Agent`、`New`、`Run`、`stream`、`executeBatch`、`executeOne`
- **它解决什么问题**：驱动“一轮任务”从 prompt 到工具调用再到最终回答
- **它依赖谁**：`provider.Provider`、`tool.Registry`、`Session`
- **谁依赖它**：`Controller` 或 `Coordinator`
- **它故意不负责什么**：不负责 UI，不负责配置发现，不负责前端输入语法

如果你只打算精读一个文件，那就是 `internal/agent/agent.go`。

你读这个文件时，不要一上来就陷在细节里。先抓主骨架：

1. `Run` 是主循环入口
2. `stream` 负责发起 provider 请求并收集文本 / reasoning / tool calls
3. `executeBatch` / `executeOne` 负责实际执行工具
4. 每轮末尾会处理 usage、compaction、readiness 等收尾逻辑

这背后的设计思想是：

- `Agent` 不做“业务功能”，它做的是 **LLM orchestration**
- 它的工作不是理解某个具体工具的业务语义，而是把 provider、tool、session 协调起来

### 节点 5：Session 与持久化

- **关键文件**：`internal/agent/session.go`、`internal/agent/save.go`
- **关键符号**：`Session`、`Add`、`Replace`、`Snapshot`、`Save`、`LoadSession`
- **它解决什么问题**：提供可修改、可保存、可恢复的对话日志
- **它依赖谁**：`provider.Message`
- **谁依赖它**：`Agent`、`Controller`、resume/load 流程
- **它故意不负责什么**：不负责生成摘要，不负责 provider 协议

最重要的设计点有两个：

1. `Session` 是带锁的可重写日志，不是 append-only event stream
2. `Save` 每次全量重写 JSONL，而不是增量 append

为什么这样设计？

因为 compaction 会改写中间历史；如果 persistence 仍然假设“只追加不改写”，系统复杂度会显著上升。

### 节点 6：Compaction 与上下文经济学

- **关键文件**：`internal/agent/compact.go`、`internal/agent/prune.go`
- **关键符号**：`maybeCompact`、`compact`、`PruneStaleToolResults`
- **它解决什么问题**：在不打断任务连续性的前提下，控制上下文窗口和缓存命中
- **它依赖谁**：`Session`、`provider.Usage`
- **谁依赖它**：`Agent.Run`
- **它故意不负责什么**：不负责 provider token 计费规则本身

这个模块体现了 `Reasonix` 的一个核心理念：
**prompt cache shape 是一等设计约束，不是事后优化。**

你可以从代码里看到这种理念如何落地：

- `maybeCompact` 先看 soft / hard threshold
- `compact` 不是简单“全历史总结”，而是重写成“前缀 + summary + 最近 tail”
- `PruneStaleToolResults` 尝试在 summarization 之前先回收无价值旧输出

维护者应该把它看成“上下文预算管理器”，而不是“聊天摘要功能”。

### 节点 7：Provider 抽象

- **关键文件**：`internal/provider/provider.go`
- **关键符号**：`Provider`、`Request`、`ToolSchema`、`Message`、`NormalizeMessages`
- **它解决什么问题**：把不同模型后端统一到同一请求/响应抽象下
- **它依赖谁**：具体 provider 子包，如 `provider/openai`、`provider/anthropic`
- **谁依赖它**：`Agent`
- **它故意不负责什么**：不负责工具执行，不负责 session 生命周期

这里的关键理解是：

- `Agent` 不直接面向 OpenAI 或 Anthropic SDK
- `Agent` 只面向 `provider.Provider`
- 真正给模型看的工具表面，是 `provider.Request.Tools []ToolSchema`

特别要看：

- `internal/provider/provider.go:Request`
- `internal/provider/provider.go:ToolSchema`
- `internal/provider/provider.go:NormalizeMessages`

`NormalizeMessages` 很重要，因为它体现了一个务实设计：
**session 可以保存“真实历史”，provider send path 再负责把历史修正成 wire-safe 形式。**

### 节点 8：Tool Registry

- **关键文件**：`internal/tool/tool.go`
- **关键符号**：`Tool`、`Registry`、`Add`、`Schemas`
- **它解决什么问题**：把系统当前可用能力整理成显式、可导出的工具表面
- **它依赖谁**：具体 tool 实现、`provider.ToolSchema`
- **谁依赖它**：`Agent`、`boot`、subagent/task/skill 体系
- **它故意不负责什么**：不做执行循环，不做权限判定

这是第二个非常核心的设计点：
**能力表面是显式 registry，不是隐式全局函数集。**

看这几个点：

- `tool.Tool` 是统一能力接口
- `Registry.Add` 负责注册并缓存 schema
- `Registry.Schemas` 把能力表面导出给 provider

这意味着系统里“模型能看到什么能力”是明确可追踪的，而不是靠 prompt 约定。

### 节点 9：Permission 与 Plan Mode

- **关键文件**：`internal/permission/permission.go`、`internal/control/auto_plan.go`、`internal/agent/agent.go`
- **关键符号**：`permission.Policy`、`Policy.Decide`、`Controller.SetPlanMode`
- **它解决什么问题**：限制工具执行的副作用，并支持 plan-first 工作流
- **它依赖谁**：tool 元数据、配置规则、controller 状态
- **谁依赖它**：`Controller`、`Agent`
- **它故意不负责什么**：不决定用户任务本身的业务意义

这里最值得你学习的是：
**plan mode 主要是运行期 gate，而不是换一套 prompt。**

原因是：

- 如果每次切 plan mode 都改工具列表和 prompt 结构，cache shape 会更不稳定
- 现在的做法是：prompt 尽量稳定，真正的限制在执行时生效

这是一个很工程化的取舍，而不是“语义最优雅”的做法。

### 节点 10：Memory / History / Skill

- **关键文件**：`internal/memory/store.go`、`internal/history/tool.go`、`internal/skill/skill.go`
- **关键符号**：`memory.Store`、`history.NewTool`、`skill.Store`
- **它解决什么问题**：给 agent 提供长期记忆、历史检索和可调用 playbook
- **它依赖谁**：plain files、registry、boot 装配
- **谁依赖它**：`boot.Build`、`Agent`
- **它故意不负责什么**：不接管主循环，不接管 controller

这几个模块都体现同一种偏好：
**把 agent 的长期外部状态尽量落成普通文件，而不是黑盒数据库。**

例如：

- `memory.Store` 是文件系统里的 memory 目录和 `MEMORY.md`
- `history` 从本地 session 里检索
- `skill` 从 markdown 文件发现和装载 playbook

### 节点 11：Coordinator 与 Subagent

- **关键文件**：`internal/agent/coordinator.go`、`internal/agent/task.go`
- **关键符号**：`Coordinator`、`NewCoordinator`、`TaskTool`、`SubagentToolRegistry`
- **它解决什么问题**：把复杂任务拆给 planner 或子 agent，同时保持上下文和能力边界可控
- **它依赖谁**：`Agent`、`Provider`、`Registry`
- **谁依赖它**：`boot.Build`、task/skill/subagent 路径
- **它故意不负责什么**：不让子 agent 无限制继承所有宿主能力

这个设计里有两个非常重要的理念：

1. **planner 与 executor 分 session**，以保持各自 prefix 稳定
2. **subagent 继承父能力，但会裁掉递归代理、后台 job 等边界敏感工具**

这就是为什么 `SubagentToolRegistry` 和 `ReadOnlySubagentToolRegistry` 这么重要：
它们不是小工具函数，而是在定义系统的 delegation boundary。

## 关键设计理念与代码落点

下面这部分不是“模块介绍”，而是“你应该从代码里读出的设计判断”。

### 理念 1：装配与执行分离

**代码落点**：

- `internal/boot/boot.go:Build`
- `internal/agent/agent.go:New`
- `internal/agent/agent.go:Run`

**你应该读出的结论**：

- `Build` 负责 system assembly
- `New` 负责把抽象对象拼成 agent 实例
- `Run` 才是单轮执行

这避免了一个常见问题：入口代码越写越像主循环，主循环越写越像配置中心。

### 理念 2：Controller 是系统边界，不是 UI 辅助类

**代码落点**：

- `internal/control/controller.go:Controller`
- `internal/control/controller.go:Submit`
- `internal/control/controller.go:RunTurn`
- `internal/control/input.go:Compose`

**你应该读出的结论**：

- 前端不直接驱动 `Agent`
- 前端驱动 `Controller`
- `Controller` 决定哪些输入是 slash/shell/goal/memory note，哪些才进入模型

所以维护时，如果你遇到“为什么这个输入没进模型”，先查 `control`，别先查 `agent`。

### 理念 3：工具是显式能力表面，不是 prompt 魔法

**代码落点**：

- `internal/tool/tool.go:Tool`
- `internal/tool/tool.go:Registry`
- `internal/tool/tool.go:Schemas`
- `internal/provider/provider.go:ToolSchema`
- `internal/agent/agent.go:stream`

**你应该读出的结论**：

模型看到哪些工具，是 registry 决定的；模型怎么拿到 schema，是 provider request 决定的。

这比“在 system prompt 里说你可以做 X”更加可维护，也更易审计。

### 理念 4：上下文窗口是系统设计约束，不是性能微调

**代码落点**：

- `internal/agent/compact.go`
- `internal/agent/cache_shape.go`
- `internal/agent/coordinator.go`
- `internal/skill/index.go`

**你应该读出的结论**：

这个项目从很多地方都在照顾 prefix cache shape：

- planner/executor 分会话
- skill 只把索引塞进 prompt，不把 body 全塞进去
- plan mode 尽量不改 prompt 结构，而在执行时 gate
- compaction 有软阈值、硬阈值和 tail 预算

如果你修改这些地方，应该先问：“这会不会把缓存稳定性打碎？”

### 理念 5：长期状态尽量落地为普通文件

**代码落点**：

- `internal/agent/save.go`
- `internal/memory/store.go`
- `internal/skill/skill.go`
- `internal/history/tool.go`

**你应该读出的结论**：

Reasonix 偏爱 plain files：

- session 是 JSONL
- memory 是 markdown + index
- skill 是 markdown
- history 从本地 session 检索

这让系统更容易恢复、调试、迁移和手工修复。

### 理念 6：subagent 不是“再开一个 Agent 就完了”

**代码落点**：

- `internal/agent/task.go:SubagentToolRegistry`
- `internal/agent/task.go:TaskTool`
- `internal/agent/coordinator.go:Coordinator`
- `internal/skill/tools.go`（如果你继续往下追 skill 调用）

**你应该读出的结论**：

这个系统不是简单递归调用自身，而是非常在意：

- 工具边界
- transcript ownership
- read-only / write-capable 差异
- 是否允许 background job

所以任何“让 subagent 更强”的改动，都必须先想清楚边界，而不是只想功能。

## 维护者学习路径

下面的顺序是“为了理解设计”，不是“为了最快定位 bug”。

### 第一遍：先看系统怎么装起来

**先读**：

- `cmd/reasonix/main.go`
- `internal/cli/cli.go`
- `internal/boot/boot.go`
- `internal/control/controller.go`（只看头部定义和入口函数）

**这一遍只回答 4 个问题**：

1. 用户输入从哪里进入系统？
2. `boot.Build` 到底产出了什么？
3. 为什么最终是 `Controller` 而不是 `Agent` 暴露给前端？
4. tool/provider 是在启动时还是运行时装配？

**这一遍先不要深挖**：

- compaction 细节
- provider wire protocol
- subagent transcript 持久化

你这时的目标只是建立“层次结构图”。

### 第二遍：看单轮执行到底怎么跑

**先读**：

- `internal/agent/agent.go`
- `internal/agent/session.go`
- `internal/provider/provider.go`
- `internal/tool/tool.go`

**这一遍只回答 5 个问题**：

1. `Agent.Run` 每一轮是怎样推进的？
2. provider 请求从哪里发出？
3. tool call 从哪里收集，再从哪里执行？
4. 执行结果怎么回写进 session？
5. 为什么 `Agent` 只依赖 provider、registry、session？

**建议读法**：

不要逐行啃 `agent.go`。先盯住这些符号：

- `New`
- `Run`
- `stream`
- `executeBatch`
- `executeOne`

看清它们之间的骨架后，再回去看 guard、retry、readiness、usage 等细节。

### 第三遍：看上下文为什么不会无限膨胀

**先读**：

- `internal/agent/compact.go`
- `internal/agent/prune.go`
- `internal/agent/save.go`
- `internal/provider/provider.go:NormalizeMessages`

**这一遍只回答 4 个问题**：

1. prompt 太长时，系统为什么不是简单截断？
2. compaction 为什么要改写 session？
3. stale tool result 为什么可以先 prune？
4. provider send path 为什么还要再做一次 Normalize？

这一遍读完，你会真正理解项目为什么这么重视“可恢复的对话历史”。

### 第四遍：看能力边界与系统策略

**先读**：

- `internal/permission/permission.go`
- `internal/control/auto_plan.go`
- `internal/agent/coordinator.go`
- `internal/agent/task.go`
- `internal/skill/skill.go`
- `internal/memory/store.go`

**这一遍只回答 6 个问题**：

1. 权限判定为什么做成纯 `Policy` + 带 I/O 的 `Gate`？
2. auto-plan 为什么先走启发式，再决定要不要 classifier？
3. planner 和 executor 为什么必须拆 session？
4. subagent 为什么要裁掉递归 meta-tools？
5. skill 为什么只把 index 放进 prompt，而不是把 body 全放进去？
6. memory 为什么落成文件而不是数据库？

这一遍结束，你对“系统理念”就不是猜的，而是能从代码里验证出来的。

## 改动入口索引

如果你以后要改系统，先按下面的入口找，不要盲搜全仓库。

### 想改 tool 注入或 tool 可见性

先看：

- `internal/boot/boot.go`
- `internal/tool/tool.go`
- `internal/tool/builtin/*`

重点符号：

- `Build`
- `addBuiltins`
- `Registry.Add`
- `Registry.Schemas`

### 想改用户输入如何分流

先看：

- `internal/control/controller.go`
- `internal/control/input.go`
- `internal/control/auto_plan.go`

重点符号：

- `Submit`
- `submit`
- `submitCommandOrTurn`
- `Compose`
- `shouldAutoPlan`

### 想改主 agent 循环

先看：

- `internal/agent/agent.go`

重点符号：

- `Run`
- `stream`
- `executeBatch`
- `executeOne`

### 想改 session 持久化或 resume 行为

先看：

- `internal/agent/session.go`
- `internal/agent/save.go`
- `internal/provider/provider.go`

重点符号：

- `Session`
- `Save`
- `LoadSession`
- `NormalizeMessages`

### 想改 compaction / context 管理

先看：

- `internal/agent/compact.go`
- `internal/agent/prune.go`
- `internal/agent/cache_shape.go`

重点问题：

- 这次改动会不会破坏 cache-stable prefix？
- 会不会让 compaction 更频繁？
- 会不会让 summary 丢掉用户约束？

### 想改 planner / subagent / skill 边界

先看：

- `internal/agent/coordinator.go`
- `internal/agent/task.go`
- `internal/skill/skill.go`
- `internal/skill/tools.go`
- `internal/boot/boot.go`

重点问题：

- 这是 planner 还是 executor 的职责？
- 子 agent 是否应该继承这项能力？
- 这个能力会不会打破 read-only / write boundary？

### 想改 provider 请求结构或模型兼容性

先看：

- `internal/provider/provider.go`
- `internal/provider/openai/*`
- `internal/provider/anthropic/*`

重点问题：

- 这是抽象层变化，还是某个 vendor 兼容层变化？
- 改动是否会影响 session wire safety？

## 如何从代码里读出理念，而不是只读到实现

这里给你一个很实用的阅读方法。

### 1. 先看 package comment 和 type comment

Reasonix 这套代码的包注释和类型注释质量很高。
像下面这些文件，开头几屏基本就是作者在告诉你“这个模块存在的理由”：

- `internal/boot/boot.go`
- `internal/control/controller.go`
- `internal/tool/tool.go`
- `internal/provider/provider.go`
- `internal/skill/skill.go`
- `internal/permission/permission.go`
- `internal/memory/store.go`

不要跳过这些注释直接看实现，否则你会把系统读成一堆工具函数。

### 2. 每次只追一条“责任链”

不要同时追：

- 输入分流
- tool 注入
- compaction
- subagent
- provider wire format

这会把你的大脑打爆。

正确方法是一次只追一条链，比如：

- 从 `runAgent` 追到 `boot.Build`
- 从 `Controller.Submit` 追到 `Agent.Run`
- 从 `Registry.Schemas` 追到 `provider.Request.Tools`
- 从 `maybeCompact` 追到 `Session.Replace`

### 3. 反复问“这个职责为什么放在这里”

这是最关键的问题。

例如：

- 为什么 auto-plan 在 `control`，不在 `agent`？
- 为什么 tool schema 在 `Registry`，不在 provider？
- 为什么 compaction 改 session，而不是单独维护一个摘要缓存？
- 为什么 planner 要拆 session，而不是和 executor 共用？

只要你持续问这个问题，你学到的就不是“代码技巧”，而是“架构判断”。

### 4. 区分“事实数据”和“派生数据”

在这个项目里，很多困惑都来自没分清两者。

例如：

- `Session.Messages` 是事实
- `provider.Request` 是派生
- `Registry` 是事实能力集合
- `ToolSchema` 是 provider-facing 派生表面
- memory 文件是事实
- system prompt 中的 memory block 是派生

你一旦按这个方式看代码，很多“为什么这里还要 normalize/compose/export 一次”的问题就通了。

## 配套阅读

读这份文档时，建议同时配合以下材料：

- `docs/SPEC.md`：全局工程契约，适合建立大局观
- `docs/GUIDE.zh-CN.md`：偏使用和系统行为
- `REASONIX.md`：项目内的长期记忆、约束和约定
- `docs/SESSION_MEMORY_RETRIEVAL.md`：如果你想继续深挖 memory/history 路径

## 最后给你的建议

如果你是为了“深入代码理解理念”，最有效的方法不是一上来读完整仓库，而是按下面的节奏：

1. 先读 `cli -> boot -> control -> agent` 主链，建立系统骨架
2. 再读 `session / compact / provider / tool`，建立执行语义
3. 最后读 `permission / planner / task / skill / memory`，理解系统策略

你会发现：

- `Reasonix` 的核心不是某个技巧函数
- 它真正有价值的是 **边界划分**、**上下文管理**、**能力显式化** 和 **可恢复的执行系统设计**

当你把这四点看懂，这个项目的理念就真正进入你的脑子里了。
