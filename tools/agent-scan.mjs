#!/usr/bin/env node
import { readdirSync, statSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname } from "node:path";

const repoRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const defaultsPath = join(repoRoot, "config", "tutti.defaults.json");

const EXTENSION_TRIGGERS = {
  codebuddy: { binaries: ["codebuddy"], apps: ["WorkBuddy", "CodeBuddy"] },
  gemini: { binaries: ["gemini"], apps: ["Gemini"] },
  copilot: { binaries: ["copilot"], apps: [] },
  kilo: { binaries: ["kilo"], apps: ["Kilo"] },
  qwen: { binaries: ["qwen", "qwen-code"], apps: ["Qwen"] },
  hermes: { binaries: ["hermes", "hermes-agent"], apps: ["Hermes"] },
  "kimi-code": { binaries: ["kimi"], apps: ["Kimi"] },
  grok: { binaries: ["grok"], apps: ["Grok"] }
};

const BUILTIN_PROVIDERS = {
  codex: { binaries: ["codex"], apps: ["ChatGPT"] },
  opencode: { binaries: ["opencode"], apps: ["OpenCode"] },
  "claude-code": { binaries: ["claude"], apps: ["Claude", "Claude Code"] },
  cursor: { binaries: ["cursor-agent", "cursor"], apps: ["Cursor"] }
};

function pathDirs() {
  return (process.env.PATH || "").split(":").filter(Boolean);
}

function which(name) {
  for (const dir of pathDirs()) {
    const p = join(dir, name);
    try {
      if (statSync(p).isFile() && accessOK(p)) return p;
    } catch {}
  }
  return null;
}

function accessOK(p) {
  try {
    execFileSync("test", ["-x", p]);
    return true;
  } catch {
    return false;
  }
}

function installedApps() {
  const out = [];
  for (const base of [
    "/Applications",
    join(process.env.HOME, "Applications")
  ]) {
    try {
      for (const name of readdirSync(base)) {
        if (name.endsWith(".app")) out.push(name.slice(0, -4));
      }
    } catch {}
  }
  return out;
}

function matchAny(targets, binaries, apps) {
  const found = { binaries: [], apps: [] };
  for (const b of targets.binaries || []) {
    const p = which(b);
    if (p) found.binaries.push(`${b} → ${p}`);
  }
  for (const a of targets.apps || []) {
    for (const installed of apps) {
      if (installed.toLowerCase() === a.toLowerCase())
        found.apps.push(installed);
    }
  }
  return found;
}

const apps = installedApps();
const report = { builtin: {}, extensions: {}, changes: [] };

for (const [provider, targets] of Object.entries(BUILTIN_PROVIDERS)) {
  const found = matchAny(targets, [], apps);
  const ok = found.binaries.length > 0 || found.apps.length > 0;
  report.builtin[provider] = ok ? [...found.binaries, ...found.apps] : null;
}

const defaults = JSON.parse(readFileSync(defaultsPath, "utf8"));
const sources = defaults.agentExtensions?.sources || [];

for (const [extKey, targets] of Object.entries(EXTENSION_TRIGGERS)) {
  const found = matchAny(targets, [], apps);
  const detected = found.binaries.length > 0 || found.apps.length > 0;
  const entry = sources.find((s) => s.key === extKey);
  if (detected && entry && !entry.enabled) {
    entry.enabled = true;
    report.changes.push(extKey);
    report.extensions[extKey] = {
      detected: [...found.binaries, ...found.apps],
      enabled: true
    };
  } else if (detected && entry) {
    report.extensions[extKey] = {
      detected: [...found.binaries, ...found.apps],
      enabled: true,
      already: true
    };
  } else if (entry) {
    report.extensions[extKey] = { detected: null, enabled: entry.enabled };
  }
}

if (report.changes.length > 0) {
  writeFileSync(defaultsPath, JSON.stringify(defaults, null, 2) + "\n");
}

console.log("== Tutti Agent 扫描报告 ==\n");
console.log("内置 Provider（守护进程自动识别，无需配置）:");
for (const [k, v] of Object.entries(report.builtin)) {
  console.log(
    `  ${v ? "✅" : "·"} ${k}${v ? `: ${v.join(", ")}` : " 未检测到"}`
  );
}
console.log("\n扩展 Agent:");
for (const [k, v] of Object.entries(report.extensions)) {
  if (v.detected) {
    console.log(
      `  ✅ ${k}: ${v.detected.join(", ")} [${v.already ? "已启用" : "本次已启用"}]`
    );
  } else {
    console.log(`  ·  ${k}: 未检测到 [enabled=${v.enabled}]`);
  }
}
if (report.changes.length > 0) {
  console.log(`\n已启用扩展: ${report.changes.join(", ")}`);
  console.log("下一步（使配置生效）:");
  console.log("  pnpm generate:defaults && pnpm build:go");
  console.log("  然后重启 Tutti 桌面端");
} else {
  console.log("\n本次无需变更。");
}
