# Codex 省额度模式验收记录（2026-08-03）

> 目标：用户开启输入框旁的单一开关后，主线程继续使用用户选择的 Codex 主模型，适合拆分的独立子任务默认交给 `gpt-5.6-luna`（`max` 推理），并跨新会话与应用重启记住开关状态。

## 结论

**核心功能链路通过，桌面 UI 截图待补。** 真实 Tutti Desktop、tuttid、Codex provider 和模型调用链已启动并完成开启态、持久化、真实子线程模型、关闭态对照验证。macOS 当前未授予 Tutti Accessibility 与 Screen Recording 权限，无法合规采集桌面窗口截图；本文中的三张图是由真实 API、会话文件和 Codex 状态库结果整理的**验收证据图**，不是 UI 截图。

## 执行信息

| 项目         | 内容                                                                       |
| ------------ | -------------------------------------------------------------------------- |
| 分支         | `feat/codex-saver-mode`                                                    |
| 桌面环境     | Tutti Desktop dev build，`make dev-gui`                                    |
| 本地入口     | `http://127.0.0.1:5173/`                                                   |
| Workspace    | `ac379c4a-5c34-4751-bd41-a29b1fa51446`                                     |
| Agent target | `local:codex`（可访问 model-plan）                                         |
| 主模型/推理  | `gpt-5.6-sol` / `low`                                                      |
| Luna 子线程  | `gpt-5.6-luna` / `max`                                                     |
| 证据优先级   | Codex `state_5.sqlite` > Session/Preferences API > 会话生成文件 > 模型回复 |

## 场景清单

| ID  | 场景               | 操作与通过标准                                                                                                                | 结果                                                                                                                                                                 |
| --- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| S01 | 开发者入口控制     | `lab.codexSaverMode` 关闭时不展示输入框开关；开启后仅支持该能力的 Codex target 展示。                                         | ✅ 自动化测试与真实 Preferences/Composer API 通过；UI 人工截图待补。                                                                                                 |
| S02 | 单一输入框开关     | 输入框旁只出现一个“Codex 省额度模式”开关，不增加模型/role/线程数选择。                                                        | ✅ 组件与文案测试通过；UI 人工截图待补。                                                                                                                             |
| S03 | 目标级记忆         | 对 `local:codex` 开启后，新会话与应用/daemon 重启后仍为开启。                                                                 | ✅ 完整重启后 Preferences 与 Composer Options 均为 `true`。                                                                                                          |
| S04 | 主模型不变         | 开启省额度模式后，新建会话仍使用用户选择的 `gpt-5.6-sol` / `low`。                                                            | ✅ Session API 与 Codex 状态库一致。                                                                                                                                 |
| S05 | Luna 配置注入      | 开启后，会话级 `CODEX_HOME/agents/luna_worker.toml` 为 Luna/max，并向会话 `AGENTS.md` 注入轻量路由规则。                      | ✅ 文件真实存在；未修改用户全局 `$CODEX_HOME`。                                                                                                                      |
| S06 | 真实子线程切模     | 给主线程一个边界明确、可独立执行的任务；主线程调用 `spawn_agent`，子线程实际为 Luna/max。                                     | ✅ 最终代码会话 `ce09a5f5-17a3-4aaf-aee7-1ffe30eecf02` 完成，1 个子线程；状态库为 Luna/max。前一诊断会话确认模型按版本无关提示选择当前 schema 的 `fork_turns=none`。 |
| S07 | 关闭态兼容         | 显式关闭后执行同类子线程任务，不生成 Luna role，不注入 Saver 规则，子线程继承主模型。                                         | ✅ 会话 `48d15900-9423-4d95-9923-a14853b8ca9e` 完成；主/子均 Sol/low，role 文件不存在。                                                                              |
| S08 | 活跃会话保护       | 已启动会话中不允许热切换该设置，避免运行时配置与 UI 状态不一致。                                                              | ✅ service 与 AgentGUI 定向测试通过。                                                                                                                                |
| S09 | 提示词轻量且跨版本 | 只说明何时委派、子线程不继承主会话历史、上下文边界与结果复核；不硬编码 V1/V2 参数，不规定固定线程数、强制并行或复杂验收协议。 | ✅ runtimeprep 测试确认无 `fork_turns`/`fork_context`、无 `max_concurrent_threads`、无强制 `parallel`。                                                              |

## 关键链路证据

### 1. 主模型保留，子线程实际使用 Luna/max

![主线程与 Luna 子线程权威状态库证据](assets/codex-saver-mode-runtime-evidence.png)

权威查询来自会话级 `CODEX_HOME/state_5.sqlite`：

```text
thread_source  model         reasoning_effort
-------------  ------------  ----------------
(main)         gpt-5.6-sol   low
subagent       gpt-5.6-luna  max
```

这里不采用模型回复中的“我正在使用 Luna”作为通过依据。

### 2. 开关跨应用重启记忆

![省额度模式目标级记忆证据](assets/codex-saver-mode-memory-evidence.png)

应用和 daemon 完整重启后，真实 API 返回：

```text
preferences.featureFlags.lab.codexSaverMode = true
preferences.agentComposerDefaultsByAgentTarget.local:codex.codexSaverMode = true
composer.codexSaverModeSupported = true
composer.effectiveSettings.codexSaverMode = true
composer.effectiveSettings.model = gpt-5.6-sol
```

### 3. 关闭态恢复原行为

![省额度模式关闭态对照证据](assets/codex-saver-mode-disabled-evidence.png)

关闭态会话仍可正常调用子线程，但不会生成 `agents/luna_worker.toml`，主线程与子线程均保持 Sol/low。

### 4. 同任务开启/关闭 A/B（效果、耗时与成本）

2026-08-03 使用相同主模型 `gpt-5.6-sol / low`、相同提示词和相同验收答案，分别创建开启与关闭会话。任务固定只创建 1 个无历史子线程，计算并复核 `1² + 2² + … + 100² = 338350`。模型与 token 均取自会话级 Codex `state_5.sqlite` 和 rollout 原始事件，不采用模型自报。

| 指标                          | 开启省额度模式                                  | 关闭省额度模式                                 |
| ----------------------------- | ----------------------------------------------- | ---------------------------------------------- |
| 主线程                        | Sol / low                                       | Sol / low                                      |
| 子线程                        | Luna / max                                      | Sol / low                                      |
| 正确性                        | 主/子答案均正确                                 | 主/子答案均正确                                |
| 子线程 token                  | 16,424（输入 16,224，其中缓存 5,888；输出 200） | 16,157（输入 16,104，其中缓存 9,984；输出 53） |
| 主线程 + 子线程 token         | 66,252                                          | 65,833                                         |
| 子线程耗时 / TTFT             | 8.320s / 8.019s                                 | 7.124s / 6.911s                                |
| 整个主 Turn 耗时              | 20.622s                                         | 18.293s                                        |
| API 等价估算：子线程          | 约 $0.0024                                      | 约 $0.0372                                     |
| API 等价估算：主线程 + 子线程 | 约 $0.0672                                      | 约 $0.0929                                     |

本次单样本中，Luna/max 子线程 token 略多且约慢 16.8%，但按 OpenAI 2026-07-30 公布的 API 单价（[Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol)：输入/缓存/输出分别为 $5/$0.50/$30 每百万 token；[Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna)：$0.20/$0.02/$1.20）折算，子线程约便宜 93.5%，整条工作流约便宜 27.7%。这说明该模式的主要收益来自 Luna 的更低计价，而不是保证减少 token 或降低延迟。

上述美元数是 **API 等价估算，不是本次 Pro/Codex 订阅的实际扣费**。估算只计算 rollout 可区分的未缓存输入、缓存读取和输出，未计无法从该记录单独识别的 cache-write surcharge。OpenAI 明确说明 Codex 订阅价格和 quota budget 不变，Luna 会消耗更少 credits；本次运行前后 UI 只提供整数百分比额度，均显示 90% 剩余，粒度不足以测出单任务实际 credit 差值。单次简单算术任务也不能代表复杂代码任务，应通过多任务、多次重复的质量/成本评测再决定默认开启范围。

公开资料：

- [OpenAI：GPT-5.6 price-performance 更新](https://openai.com/index/advancing-the-price-performance-frontier-with-gpt-5-6/) 给出的推荐编码链路正是 Sol 处理不确定性和规划，Luna 执行定义清晰的实现、测试和评估；同时说明 Luna API 降价 80%，Codex 中会消耗更少 credits。
- [OpenAI 在 X 的价格公告](https://x.com/OpenAI/status/2082878156483219672) 说明 Luna 降价 80%，并将更低价格反映到 Codex/ChatGPT Work 的 usage 计算。
- [Viv 的 X 讨论](https://x.com/Vtrivedy10/status/2083197691429863687) 指出 Luna 并非 Codex Multi-Agent V2 原生推荐的协作子代理，建议将其作为独立 Thread 运行；其讨论中也有人报告“Luna Max 主线程 + Sol 顾问 Thread”用量更低，但属于个人单次经验。
- [Eric Provencher 的 X 提醒](https://x.com/pvncher/status/2083300990350954981) 不建议修改 model catalog 强行开放 Luna，并认为需要主动代理间通信的任务仍应使用 Sol/Terra。本实现不修改 catalog，且提示词只把边界清楚、可独立验收的任务交给 Luna。
- [社区价格/基准对比](https://x.com/_codemeow/status/2084095080705741153) 称 Luna/max 与 GPT-5.4/xhigh 在 Artificial Analysis 上同为 51 分、价格低 92%；这是社区转述，不作为本功能验收的权威质量证据。

目前未找到针对“Sol 主线程 + Luna/max 独立子线程 + 本开关实现”的公开、可复现受控评测，因此公开讨论只能作为路由策略参考，不能替代本地 A/B 和后续业务任务评测。

## 数据链路验收

```text
开发者开关 lab.codexSaverMode
        │ 控制入口是否展示
        ▼
输入框 Codex 省额度模式开关
        │ target 级默认值持久化
        ▼
Preferences → Composer Options → Create Session
        │ codexSaverMode=true，主模型字段原样保留
        ▼
Host / daemon Session settings
        │ normalize、stream payload、resume 均保留设置
        ▼
runtimeprep（仅会话级 CODEX_HOME）
        ├─ agents/luna_worker.toml：默认 subagent = Luna/max
        └─ AGENTS.md：边界任务 + spawn_agent 无历史 fork + 主线程复核
        ▼
Codex 主线程（用户模型） → 独立子线程（Luna/max） → 主线程汇总
```

## 验证命令与结果

```bash
go test ./packages/agent/runtimeprep ./packages/agent/daemon/runtime ./packages/agent/daemon/hostadapter
```

结果：通过。

```bash
pnpm check:changed
```

结果：最终 34 个 lane 全部通过（失败 lane 修复后复跑：6 个实际执行、28 个复用已通过结果）。独立 reviewer 已复查 settings round-trip、Codex V1/V2 no-history 兼容、默认 role 覆盖、TOML 合并边界和提示词重量；其提出的问题均已修复并补回归，最终复审 PASS、无阻断项。

## 已发现并修复的问题

1. Session settings 在 daemon runtime normalize 与 stream payload 往返时曾丢失 `codexSaverMode`，表现为创建请求为 `true`，随后读取变成 `false`。已补齐字段透传与回归测试。
2. 直接暴露 `agent_type` 会改变 Codex 保留工具 schema，当前 Sol 接口会以 400 拒绝，故未采用。
3. Codex V1 与 V2 的无历史参数不同（V1 为 `fork_context`，V2 为 `fork_turns`），且完整继承主线程时上游不会应用不同 role/model。最终方案把 Luna 配成省额度会话的默认子代理，并用版本无关的轻量 `AGENTS.md` 指示子任务不要继承主会话历史，由模型按当前工具 schema 选择对应选项。最终真实会话选择了 `fork_turns=none`。
4. 用户原配置若已定义 `agents.default`，会与自动发现的 Luna default 冲突。已改为只在省额度会话的隔离 `config.toml` 中显式声明 `agents.default → ./agents/luna_worker.toml`，不修改用户全局配置；标准表、quoted 表、`[agents]` 内联表、root dotted keys 及多行 description 均有冲突回归测试。
5. 应用重启后，旧 runtime 观测值曾覆盖创建时的不可变 Session 快照，使数据库仍为 `true` 的会话被 API/UI 错误显示为 `false`。已改为从 runtime snapshot 恢复 `codexSaverMode`；开启、关闭和缺少该字段的旧会话均有回归测试。重启最终开发版后，截图中的会话 `b8560ee5-0c85-42fa-9acd-f9e91193e3c3` 已由 Session API 正确返回 `true`。

## 风险与待补

- **UI 截图待补**：当前 Tutti 缺少 macOS Accessibility 与 Screen Recording 权限。授权后应补两张真实桌面截图：开发者设置中的入口开关、Composer 输入框的开启态。
- “不继承主会话历史”依赖主模型遵循会话指令；若模型忽略并使用完整历史 fork，子线程会按 Codex 上游规则继承主模型。真实验收任务已正确选择当前工具的 no-history 选项并切到 Luna/max。
- 该模式只改变适合独立委派的子线程，不保证每个任务都会拆分；轻量、强耦合任务继续由主线程处理属于预期行为。
- Luna 当前不是公开讨论中 Multi-Agent V2 原生推荐的主动协作模型。应继续限制为上下文自包含、结果可由主线程复核的独立任务；复杂跨代理协作、频繁互发消息和高风险决策仍留给 Sol/Terra。
- 当前成本结论只有一次微型任务样本；API 等价价格能证明单价差，但不能证明所有真实任务的总成本、质量或时延都更优。
- 开启态只写会话级 Codex home；关闭或新建关闭态会话不会遗留 Luna 配置，不需要数据迁移。

## 最终人工复验步骤

1. 在开发者设置打开“Codex 省额度模式入口”。
2. 新建 Codex 会话，确认输入框旁出现且只有一个“Codex 省额度模式”开关。
3. 选择任意 Codex 主模型并开启开关，发送一个可独立拆分的中等任务。
4. 确认主会话仍显示用户选择的模型，且能看到一个子线程完成后回到主线程汇总。
5. 新建会话并重启应用，确认开关仍为开启。
6. 关闭开关再新建会话，确认行为恢复且无 Luna 配置注入。
