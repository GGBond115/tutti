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

## 风险与待补

- **UI 截图待补**：当前 Tutti 缺少 macOS Accessibility 与 Screen Recording 权限。授权后应补两张真实桌面截图：开发者设置中的入口开关、Composer 输入框的开启态。
- “不继承主会话历史”依赖主模型遵循会话指令；若模型忽略并使用完整历史 fork，子线程会按 Codex 上游规则继承主模型。真实验收任务已正确选择当前工具的 no-history 选项并切到 Luna/max。
- 该模式只改变适合独立委派的子线程，不保证每个任务都会拆分；轻量、强耦合任务继续由主线程处理属于预期行为。
- 开启态只写会话级 Codex home；关闭或新建关闭态会话不会遗留 Luna 配置，不需要数据迁移。

## 最终人工复验步骤

1. 在开发者设置打开“Codex 省额度模式入口”。
2. 新建 Codex 会话，确认输入框旁出现且只有一个“Codex 省额度模式”开关。
3. 选择任意 Codex 主模型并开启开关，发送一个可独立拆分的中等任务。
4. 确认主会话仍显示用户选择的模型，且能看到一个子线程完成后回到主线程汇总。
5. 新建会话并重启应用，确认开关仍为开启。
6. 关闭开关再新建会话，确认行为恢复且无 Luna 配置注入。
