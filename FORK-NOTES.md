# Fork 定制说明（GGBond115/tutti）

基于 tutti-os/tutti 的个人定制版。本文档记录所有偏离上游的改动。

## 定制内容

### 1. 新增 Provider：豆包（doubao）

豆包桌面版是纯 GUI 应用（无 CLI、无 ACP），通过**自研 ACP 桥接器**接入（剪贴板人工接力）：

- 桥接器：`tools/agent-bridges/doubao-bridge.mjs`（ACP v1 协议，Node 零依赖）
- 安装位置：`~/.local/bin/tutti-doubao-bridge`
- 工作方式：
  1. 在 Tutti 会话中发任务 → 桥接器自动复制到剪贴板并唤起豆包 App
  2. 在豆包中粘贴发送，拿到回复后复制全文
  3. 回到 Tutti 会话随便发一条消息 → 桥接器读取剪贴板，把豆包回复记录进会话
- 显示名"豆包"，图标取自 Doubao.app（`doubao-rounded.png` 等 6 个变体）
- 已知限制：豆包额度无公开 API（App 内加密会话），暂不显示额度

### 2. 新增 Provider：WorkBuddy（codebuddy）

WorkBuddy 桌面 App（腾讯 CodeBuddy 引擎）通过**headless CLI 桥接**接入：

- 桥接器：`tools/agent-bridges/workbuddy-bridge.mjs` → `~/.local/bin/tutti-workbuddy-bridge`
- 原理：ACP stdio ↔ `codebuddy -p --output-format stream-json --permission-mode bypassPermissions [--resume <id>]`
  - 每回合起一个 headless CLI 进程；`session/new` 分配会话 id，后续回合用 `--resume` 延续上下文
  - 流式 text delta 以 `agent_message_chunk` 推给 Tutti
  - 权限模式 bypassPermissions：headless 下无授权弹窗通道，acceptEdits 会导致 Bash/Glob 等工具全被拒
  - 模型自选：session/new 返回 model configOptions（列表从 `--help` 动态解析），
    `session/set_config_option` 切换后每回合带 `--model <id>`
  - 全局记忆：每会话首回合自动注入 `~/.workbuddy/memory/<uid>_memory.md`（WorkBuddy App 的
    User Memory Profile），后续回合靠 --resume 继承
  - 每回合起一个 headless CLI 进程；`session/new` 分配会话 id，后续回合用 `--resume` 延续上下文
  - 流式 text delta 以 `agent_message_chunk` 推给 Tutti
- 为什么不走 CLI 自带的 `--acp` 模式：该模式依赖本地 daemon（`/internal/agent`），
  standalone 场景下 session/new 会无超时挂起，不可用
- CLI 来源：npm 官方包 `@tencent-ai/codebuddy-code@2.121.2`，装在
  `~/.local/share/tutti/codebuddy-standalone/`（Tutti 官方扩展同款源；
  官方扩展本身因 minTuttiVersion 校验对本地构建的 0.0.0-dirty 版本号不生效）
- 登录态复用：WorkBuddy App 登录后会把 CLI 凭据写在
  `~/Library/Application Support/CodeBuddyExtension/Data/Public/auth/workbuddy-desktop.info`，
  桥接器每次回合自动把它同步为 CLI 读取的 `Tencent-Cloud.coding-copilot.info`（mtime 判断，不覆盖更新的）
- 登录检测诚实化：`tutti-workbuddy-bridge --check` 校验凭据文件存在且未过期，
  输出 `workbuddy-bridge (not )?authenticated`（OpenCode 解析器识别）
- 积分（credits）额度：
  - `DesktopUsageProbeKindCodeBuddy`（"codebuddy"）+ 桌面端 handler
    `apps/desktop/src/main/codebuddyProviderUsageProbe.ts`
  - 数据源 `POST https://copilot.tencent.com/billing/meter/get-user-resource`（Bearer token 取自上述凭据文件）
  - 请求体携带 App 同款过滤参数：ProductCode="p_tcaca"、Status=[0,3]、分页、时间范围
  - 用 `CycleCapacityRemainPrecise`/`CycleCapacitySizePrecise`（精确小数）聚合所有有效套餐
  - activePlan 优先级：旗舰 > 高级 > 青年 > Pro年付/月付 > 试用/体验
  - PackageCode → 中文套餐名映射（免费版/Pro月付版/成长计划积分/青年版/高级版/旗舰版等）
  - 输出 quotaType=credits + percentRemaining + amountRemaining/amountLimit + resetsAtUnixMs（activePlan.CycleEndTime，UTC+8 解析）
  - 已验证：Tutti 显示 7,611.72 credits 与 WorkBuddy App 完全一致
- `~/.local/bin/codebuddy` 是个薄包装器（`login`/`--login` 引导进交互式 REPL 输入 `/login`，其余透传）

改动文件：

- `packages/agent/store-sqlite/canonical/provider.go`（DoubaoProviderID + CodeBuddyProviderID + 身份）
- `packages/agent/daemon/providerregistry/providers.go`（doubaoDescriptor + codeBuddyDescriptor）
- `packages/agent/daemon/providerregistry/types.go`（DesktopUsageProbeCodeBuddy）
- `packages/agent/daemon/providerregistry/registry.go`（注册 + usage probe kind 校验放行）
- `packages/agent/daemon/providerregistry/registry_test.go`（两个测试的期望集合同步）
- `services/tuttid/api/openapi/tuttid.v1.yaml`（枚举/属性）
- `packages/events/protocol/schemas/topics/preferences/desktop-preferences.schema.json`（defaultAgentProvider 枚举 + doubao/codebuddy）
- `apps/desktop/src/shared/preferences/core.ts`（desktopAgentProviders / desktopDefaultAgentProviders）
- `apps/desktop/src/main/codebuddyProviderUsageProbe.ts`（积分探测，新增）
- `apps/desktop/src/main/agentProviderUsageProbe.ts`（注册 codebuddy handler）
- `packages/agent/gui/managedAgentIconAssets.ts` + `providerIconAssets.ts`（doubao/workbuddy 图标条目）
- `packages/agent/gui/app/renderer/i18n/locales/{en,zh-CN}.agentGuiProviderIdentity.ts`（conversationFilter 词条）
- 生成物：`pnpm generate:api && pnpm generate:agent-gui-provider-catalog && pnpm generate:event-protocol`

### 3. Agent 列表只显示就绪的 Agent

- `apps/desktop/src/renderer/.../desktopAgentsService.ts` 的
  `mapAgentTargetPresentationsToAgents` 增加 `availability.status === "ready"` 过滤
- 效果：未安装（cursor）、未登录（claude-code/opencode）、不支持的 provider 不再出现在
  Agent 列表；登录/安装完成后自动出现
- 管理入口（设置 → Agents）不受影响，仍可看到全量 provider 并执行登录/安装

### 4. Agent 自动扫描器

- `tools/agent-scan.mjs`（`pnpm scan:agents`）
- 扫描 PATH + /Applications，识别已装的 Agent
- 内置 Provider（codex/opencode/claude-code/cursor）：守护进程自动识别，无需配置
- 扩展类（gemini/codebuddy/qwen/kimi-code/hermes/copilot/kilo/grok）：检测到宿主 App/CLI 后自动把 `enabled` 置为 true
- 新装了 AI Agent 后跑一次 `pnpm scan:agents`，按提示重建即可

### 5. 其他

- `apps/desktop/src/main/bootstrap.ts`：`supportsUpdates: false`（防止官方更新覆盖定制）
- `config/tutti.defaults.json`：codebuddy/hermes 扩展 enabled（扩展下载因版本校验失败，不影响上述桥接方案）
- `pnpm-workspace.yaml`：onlyBuiltDependencies 放行 esbuild、electron-winstaller

## 环境要求（本机已就绪）

- Node ≥ 24（brew node 26）
- pnpm 10.11.0（npm -g）
- Go 1.24+（brew go 1.27）
- esbuild/electron 构建脚本已在 pnpm-workspace.yaml onlyBuiltDependencies 中放行

## 日常开发

```bash
pnpm dev:desktop        # 启动桌面端（开发模式）
pnpm scan:agents        # 扫描本机 Agent
pnpm build              # 全量构建
bash tools/scripts/build-desktop-package.sh mac-unsigned   # 打包（装完删 apps/desktop/dist 省 1.8GB）
xattr -cr /Applications/Tutti.app   # 未签名应用首次打开
```

守护进程状态：生产 `~/.tutti/`，dev 模式 `~/.tutti-dev/`；日志 `logs/tuttid.log`

## 待办 / 已知事项

- OpenCode CLI 已安装（1.18.23），需运行 `opencode auth login` 完成一次登录
- 豆包额度：App 内加密会话 + 接口不公开，暂无法显示；如需可抓包分析后再加
- WorkBuddy 积分依赖 WorkBuddy App 的登录凭据文件；App 内退出登录后 Tutti 会同步变为 auth_required
- WorkBuddy 回合权限模式为 bypassPermissions（与 WorkBuddy App 行为一致，工具全放行；桥接器无授权弹窗通道）
- 豆包桥接器依赖 macOS `pbcopy/pbpaste/open`，仅限 macOS
- 上游同步：`git fetch upstream` 后 rebase，注意 providerregistry / openapi / preferences schema 定制点可能冲突
