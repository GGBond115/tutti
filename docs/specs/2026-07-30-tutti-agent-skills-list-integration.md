# Tutti Agent `skills/list` 接入方案

Status: implemented; pending cross-platform artifact validation

## 1. 背景与目标

Tutti Agent 与 Codex 使用相同的 app-server 协议。当前 Tutti Agent 已声明
`skills` capability，但没有接入 Composer capability catalog，因此
AgentGUI 收不到 `skills/list` 的结果。

已核实：

- Tutti Agent `0.0.10` 支持
  `initialize -> initialized -> skills/list`；本机调用返回 49 个启用的
  skills。
- tuttid 现有 Codex capability lister 已经具备 app-server 进程管理、
  `skills/list` 请求、响应解析、30 秒缓存和错误降级。
- `skills/list` 结果已能映射成
  `ComposerCapabilityOption{Kind:"skill", Invocation:"promptItem"}`。
- OpenAPI capability DTO、Activity Adapter、AgentGUI 和 runtime 已完整支持
  上述 capability，无需增加 skill DTO 字段。
- AgentGUI 当前只允许 `$` 查询 skills，符合本次确认的产品要求。

目标是复用 Codex 的现有链路，为 Tutti Agent 增加“只请求
`skills/list`”的 catalog 模式，使 Tutti skills 出现在 `$` 面板并按
`promptItem` 提交。

## 2. 当前架构和核心问题

### 2.1 当前 Composer 链路

```text
AgentGUI
  -> AgentActivityRuntime.getComposerOptions
  -> POST /v1/agent-providers/{provider}/composer-options
  -> tuttid Service.GetComposerOptions
  -> listComposerCapabilityOptions
  -> ComposerOptions.capabilityCatalog
  -> OpenAPI capability DTO
  -> activity-tuttid-adapter
  -> providerSkillsFromComposerOptions
  -> AgentGUI "$" palette
  -> structured promptItem
  -> app-server turn/start
```

这条链路对 Codex 已经存在，但 Tutti Agent descriptor 没有声明
`CapabilityCatalog`：

```text
Tutti Agent CapabilityCatalog.Kind = ""
  -> composerCapabilityCatalogLister 返回 unsupported=false
  -> capabilityCatalog=[]
  -> AgentGUI availableSkills=[]
```

### 2.2 Codex 现有实现

Codex descriptor 使用 `CapabilityCatalogKindCodexAppServer`。当前 lister
启动 `codex app-server` 后一次请求：

```text
skills/list
app/list
plugin/list
mcpServerStatus/list
```

解析完成后，Codex 的
`NativePluginCatalogAuthoritative=true` 会只向 Composer 投影
sites/browser/computer-use 等 native plugins。

Tutti Agent 没有 app/plugin/MCP，因此不能直接照搬“四个方法全请求”，但
可以复用同一个 lister，仅把请求集缩小为 `skills/list`。

### 2.3 已经可直接复用的后半段

现有 `parseCodexSkillCapabilities` 已生成：

```text
kind=skill
status=available | disabled
trigger=$<name>
path=<SKILL.md>
invocation=promptItem
```

当 provider 没有设置 `NativePluginCatalogAuthoritative` 时，
`providerSkillsFromComposerOptions` 会把 available、具有 path 的
`promptItem` skill capability 转为 AgentGUI skill。

AgentGUI 当前明确只放行 `$`：

```ts
const skillQueryMatch =
  triggerQueryMatch?.prefix === "$" ? triggerQueryMatch : null;
```

提交时现有代码会生成：

```json
{
  "type": "skill",
  "name": "review",
  "path": "/path/to/review/SKILL.md"
}
```

因此问题只在 app-server catalog 的“入口和请求范围”，不在 HTTP、前端或
runtime。

## 3. 目标架构和完整链路

```text
Tutti Agent ProviderDescriptor
  CapabilityCatalog.Kind = app_server_skills
             |
             v
composerCapabilityCatalogLister
  command 来自 RuntimeDescriptor:
  ["tutti-agent", "app-server"]
  requestSet = skillsOnly
             |
             v
现有 app-server lister
  initialize
  initialized
  skills/list(cwds=[cwd], forceReload=false)
             |
             v
现有 parseCodexSkillCapabilities
  -> ComposerCapabilityOption(kind=skill, trigger=$name,
                              path, invocation=promptItem)
             |
             v
ComposerOptions.capabilityCatalog
  -> 现有 OpenAPI capability DTO
  -> 现有 Activity Adapter
  -> 现有 providerSkillsFromComposerOptions
  -> 现有 "$" skill palette
             |
             v
选择 "$review"
  -> 现有 promptItemBlocksForProviderSkills
  -> {type:"skill", name:"review", path:"..."}
  -> 现有 appServerUserInput
  -> tutti-agent turn/start
```

Codex 和 Tutti Agent 的差异只保留在 descriptor：

| Provider    | Catalog kind        | RPC 请求              | GUI 投影            |
| ----------- | ------------------- | --------------------- | ------------------- |
| Codex       | `codex_app_server`  | skills/app/plugin/MCP | 现有 native plugins |
| Tutti Agent | `app_server_skills` | 仅 skills             | 普通 `$` skills     |

## 4. 各仓库、服务和模块改造

### 4.1 `tutti`：provider registry

涉及：

- `packages/agent/daemon/providerregistry/types.go`
- `packages/agent/daemon/providerregistry/registry.go`
- `packages/agent/daemon/providerregistry/providers.go`

新增一个 capability catalog kind：

```go
const CapabilityCatalogKindAppServerSkills CapabilityCatalogKind =
    "app_server_skills"
```

registry validation 接受该值。

Tutti Agent descriptor 增加：

```go
CapabilityCatalog: CapabilityCatalogDescriptor{
    Kind: CapabilityCatalogKindAppServerSkills,
},
```

不新增 `SkillKind`、`SkillDiscoveryKind`、`TriggerPrefix` 或 provider-specific
GUI behavior。

### 4.2 `tuttid`：现有 app-server capability lister

涉及：

- `services/tuttid/service/agent/codex_capability_catalog.go`
- `services/tuttid/service/agent/codex_capability_catalog_test.go`

给现有 lister 增加一个小型请求范围：

```go
type appServerCatalogRequestSet string

const (
    appServerCatalogRequestSetCodex     = "codex"
    appServerCatalogRequestSetSkillsOnly = "skills_only"
)
```

`composerCapabilityCatalogLister` 根据 descriptor kind 构造：

```text
codex_app_server
  -> RequestSet=codex

app_server_skills
  -> RequestSet=skills_only
```

把当前固定的 `requests` 和 `pending` 改为由 request set 生成：

```text
codex:
  2 -> skills/list
  3 -> app/list
  4 -> plugin/list
  5 -> mcpServerStatus/list

skills_only:
  2 -> skills/list
```

握手、进程、scanner 限额、超时、解析函数和错误处理继续使用现有代码。

### 4.3 `tuttid`：Composer capability 投影

仍在 `codex_capability_catalog.go` 内完成，不新增 service 层级。

现有成功分支是：

```go
if kind == codex_app_server {
    return codexNativeComposerPluginOptions(result.Options)
}
return mergeComposerCapabilityOptions(fallback, result.Options)
```

新 `app_server_skills` 不进入 Codex native plugin 过滤，因此
`skills/list` 产生的 skill options 会原样进入 `capabilityCatalog`。

失败时沿用现有行为：

- Composer Options 请求仍成功。
- capability catalog 为空。
- 错误写入现有 `capabilityCatalogErrors` 诊断上下文。
- 成功结果继续使用现有 30 秒缓存。

可以补一条非敏感结构化日志，记录
`provider/requestSet/returnedCount/errorCount/durationMs`；不记录 skill
内容、路径或认证信息。

### 4.4 HTTP、Activity、AgentGUI、runtime

这些模块不改生产代码：

| 模块                                     | 原因                                                    |
| ---------------------------------------- | ------------------------------------------------------- |
| `services/tuttid/api/openapi`            | capability DTO 已有 kind/status/trigger/path/invocation |
| `services/tuttid/api`                    | capability mapper 已传递全部所需字段                    |
| `packages/agent/activity-tuttid-adapter` | 已解析 capability invocation                            |
| `packages/agent/activity-core`           | capability 类型已完整                                   |
| `packages/agent/gui`                     | 已把 skill capability 投影成 `$` skill；继续只允许 `$`  |
| `packages/agent/daemon/runtime`          | 已支持 structured skill block                           |

只增加回归测试，不增加新接口或状态。

### 4.5 `tutti-agent` 仓库

当前 Tutti Agent `0.0.10` 已提供所需 `skills/list` 协议，本次未改该仓库生产代码。

可补一条 contract test，确认以下请求保持兼容：

```json
{
  "method": "skills/list",
  "params": {
    "cwds": ["/workspace"],
    "forceReload": false
  }
}
```

**待验证：**其他平台发布的 `0.0.10` artifact 是否与本机验证版本一致。

## 5. 文件拆分与 Adapter 边界

本次不需要新建大型 adapter 框架，也不需要新增 package。

建议只在现有文件内增加：

```text
providerregistry/types.go
  -> 新 catalog kind

providerregistry/providers.go
  -> Tutti descriptor 选择 skills-only catalog

codex_capability_catalog.go
  -> request set
  -> 动态 requests/pending

对应测试文件
  -> Tutti 只请求 skills/list
  -> Codex 请求集不变
```

边界保持为：

```text
Provider descriptor
  决定使用哪一种 catalog request set

tuttid app-server lister
  负责进程、JSON-RPC 和 wire parsing

Composer service
  负责缓存、错误降级和 capability projection

AgentGUI
  只消费统一 capability contract，不判断 provider
```

## 6. 明确不做

- 不开放 `/` 查询 skills。
- 不修改 AgentGUI skill palette 生产代码。
- 不给 Tutti Agent 请求 app/plugin/MCP。
- 不给 Tutti Agent设置 `NativePluginCatalogAuthoritative`。
- 不新增 `AgentProviderSkillOption.invocation`。
- 不新增 `SkillDiscoveryKind`、`TriggerPrefix` 等 descriptor 字段。
- 不切换 Codex filesystem skills 的来源。
- 不改变 Codex native plugin 展示策略。
- 不引入长驻 app-server 或 `skills/changed` watcher。
- 不新增数据库表、HTTP endpoint、Activity state 或 Host API。
- 不修改 Agent 排序和模型选择器。

## 7. 数据迁移和兼容策略

- 无数据库或用户配置迁移。
- HTTP schema 不变。
- Desktop/daemon 不存在新增字段的滚动升级问题。
- 旧 daemon 没有 Tutti catalog kind，仍返回空 skills，与当前行为一致。
- 新 daemon 使用现有 capability DTO；旧 Desktop 已能解析并展示。
- Claude Code、Cursor、OpenCode 和 Codex 的既有行为不变。

## 8. 风险与回滚

| 风险                                           | 控制                                                                               |
| ---------------------------------------------- | ---------------------------------------------------------------------------------- |
| 修改固定 pending 集合导致 Codex 回归           | 用现有四 RPC fixture 做完整回归                                                    |
| skills-only 仍误发其他 RPC                     | fake app-server 断言只收到 initialize/initialized/skills/list                      |
| Tutti app-server 超时                          | 沿用现有 8 秒超时、非致命降级和 30 秒缓存                                          |
| disabled skill 被展示                          | 沿用 parser 的 disabled status 和 GUI 的 available filter                          |
| app-server home/environment 与真实运行环境不同 | 本机 `make dev-gui` 已验证；其他平台仍待验证，只有出现差异证据后再增加环境准备逻辑 |

回滚只需从 Tutti descriptor 移除
`CapabilityCatalogKindAppServerSkills`，即可恢复当前空 catalog；没有数据需要
回滚。

## 9. 测试与验收标准

### 9.1 自动化测试

1. Provider registry 接受 `app_server_skills`，且 Tutti descriptor 使用该值。
2. skills-only fake app-server 只收到：
   - `initialize`
   - `initialized`
   - `skills/list`
3. Codex 仍收到原有四个 catalog RPC。
4. Tutti `skills/list` fixture 映射出：
   - `kind=skill`
   - `trigger=$name`
   - `path`
   - `invocation=promptItem`
5. `enabled=false` 不进入 AgentGUI available skills。
6. app-server 失败时 Composer Options 仍成功且 catalog 为空。
7. 现有 AgentGUI 测试证明 `$` skill capability 可见并生成 structured skill
   block。

执行：

```text
相关 providerregistry Go tests
services/tuttid/service/agent capability catalog tests
services/tuttid/api composer-options tests
AgentGUI capability/skill submission 回归测试
pnpm check:agent-provider-strategy-boundaries
pnpm check:changed
```

### 9.2 人工验收

2026-07-30 已使用本 worktree 的 `make dev-gui` 完成本机验收：

- Composer 当前 Provider 为 Tutti Agent。
- 输入 `$` 展示 49 个 skills。
- 输入 `$skill-cre` 只剩 `skill-creator`，选择后生成的编辑器 token 保留
  `trigger="$skill-creator"` 和 `data-agent-mention-kind="skill"`。
- 输入 `/skill-cre` 不展示 `skill-creator`。
- 验证未发送消息，结束后已清空 Composer 草稿。

- 选择 `local:tutti-agent` 后 Composer 状态为 ready。
- Composer Options 的 `capabilityCatalog` 中存在 available skill，字段包含
  `$trigger`、path、`invocation=promptItem`。
- `skills` 数组可以继续为空；AgentGUI 从 capability catalog 投影 skills。
- 输入 `$` 能检索 Tutti skills。
- 输入 `/` 不展示 skills。
- 选择并发送后，`turn/start.input` 包含结构化 skill block。
- 日志或 fake server 证明没有调用 app/plugin/MCP。
- Codex native plugins 的展示与调用保持不变。

## 10. 分阶段实施顺序

1. 增加 `CapabilityCatalogKindAppServerSkills` 和 Tutti descriptor 配置。
2. 给现有 lister 增加 skills-only request set，并补 Codex/Tutti RPC 测试。
3. 验证 Composer capability projection、缓存和错误降级。
4. 跑 API、AgentGUI、runtime 既有链路回归测试。
5. 使用发布版 Tutti Agent `0.0.10` 做端到端验收；本机已完成，其他平台
   artifact 仍待验证。只有发现真实环境差异时再补最小环境适配。

## 11. 复杂度结论

本方案只新增一个 descriptor 枚举和一个 lister 请求范围，复用现有
Codex 的其余链路。

不需要前一版提出的：

- 新 skill discovery framework；
- skill DTO 扩展；
- Activity Adapter 修改；
- AgentGUI `/` 改造；
- Codex skill 来源迁移；
- 新 cache；
- 新 endpoint。

因此生产代码改动预计集中在 provider registry 和
`codex_capability_catalog.go` 两个区域，属于对现有链路的窄扩展，不是新增
一套 Tutti 专用系统。
