#!/usr/bin/env node
import { execFileSync, spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { existsSync } from "node:fs";

const APP_BUNDLE_CANDIDATES = [
  "/Applications/Doubao.app",
  "/Applications/豆包.app",
  "/Applications/DoubaoWork.app",
  "/Applications/豆包办公.app"
];
const OPEN_TARGETS = ["Doubao", "豆包", "DoubaoWork", "豆包办公"];

const state = { lastCopied: null };
let nextSessionId = 0;

function log(message) {
  process.stderr.write(`[tutti-doubao-bridge] ${message}\n`);
}

function findAppBundle() {
  return (
    APP_BUNDLE_CANDIDATES.find((candidate) => existsSync(candidate)) ?? null
  );
}

function pbpaste() {
  try {
    return execFileSync("pbpaste", {
      encoding: "utf8",
      maxBuffer: 64 * 1024 * 1024
    });
  } catch {
    return "";
  }
}

function pbcopy(text) {
  execFileSync("pbcopy", { input: text, maxBuffer: 64 * 1024 * 1024 });
}

function openDoubao() {
  const bundle = findAppBundle();
  const args = bundle ? [bundle] : ["-a", OPEN_TARGETS[0]];
  try {
    spawn("open", args, { detached: true, stdio: "ignore" }).unref();
    log(`已唤起豆包 (${bundle ?? OPEN_TARGETS[0]})`);
  } catch (error) {
    log(`唤起豆包失败: ${error.message}`);
  }
}

function textOfPrompt(prompt) {
  if (!Array.isArray(prompt)) {
    return typeof prompt === "string" ? prompt : "";
  }
  return prompt
    .filter(
      (block) =>
        block && block.type === "text" && typeof block.text === "string"
    )
    .map((block) => block.text)
    .join("\n")
    .trim();
}

function send(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function reply(id, result) {
  send({ jsonrpc: "2.0", id, result });
}

function fail(id, code, message) {
  send({ jsonrpc: "2.0", id, error: { code, message } });
}

function emitReply(sessionId, text) {
  send({
    jsonrpc: "2.0",
    method: "session/update",
    params: {
      sessionId,
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text }
      }
    }
  });
}

async function handlePrompt(params) {
  const sessionId =
    typeof params.sessionId === "string" ? params.sessionId : "";
  const clipboard = pbpaste();
  const trimmedClipboard = clipboard.trim();
  const lastCopied = state.lastCopied;
  if (
    lastCopied !== null &&
    trimmedClipboard !== "" &&
    trimmedClipboard !== lastCopied.trim()
  ) {
    emitReply(sessionId, clipboard.replace(/\s+$/, ""));
    log(`读回剪贴板回复 ${trimmedClipboard.length} 字符`);
  }
  const task = textOfPrompt(params.prompt);
  if (task !== "") {
    pbcopy(task);
    state.lastCopied = task;
    if (process.env.TUTTI_DOUBAO_BRIDGE_NO_OPEN !== "1") {
      openDoubao();
    }
    log(`任务已复制到剪贴板 ${task.length} 字符，等待用户粘贴到豆包`);
  } else {
    log("空任务，跳过剪贴板写入");
  }
  return { stopReason: "end_turn" };
}

async function dispatch(message) {
  const { id, method, params } = message;
  switch (method) {
    case "initialize":
      return reply(id, {
        protocolVersion: 1,
        agentCapabilities: {
          promptCapabilities: { image: false, embeddedContext: false }
        }
      });
    case "session/new":
      return reply(id, {
        sessionId: `doubao-${Date.now()}-${++nextSessionId}`
      });
    case "session/prompt":
      return reply(id, await handlePrompt(params ?? {}));
    default:
      if (id === undefined || id === null) {
        return;
      }
      return fail(id, -32601, `method not supported: ${method}`);
  }
}

function runCheck() {
  const bundle = findAppBundle();
  if (!bundle) {
    log(`未找到豆包 App（候选路径: ${APP_BUNDLE_CANDIDATES.join(", ")}）`);
  } else {
    log(`检测到豆包 App: ${bundle}`);
  }
  process.stdout.write("doubao-bridge authenticated\n");
  process.exit(0);
}

function main() {
  if (process.argv.includes("--check")) {
    runCheck();
    return;
  }
  if (process.argv.includes("--login")) {
    log("豆包桥接器无需登录：请确认已安装豆包桌面版并保持登录状态。");
    process.exit(0);
  }
  const bundle = findAppBundle();
  log(
    bundle
      ? `启动 ACP v1 桥接（豆包: ${bundle}）`
      : "启动 ACP v1 桥接（未找到豆包 App）"
  );
  const reader = createInterface({ input: process.stdin, terminal: false });
  reader.on("line", (line) => {
    const trimmed = line.trim();
    if (trimmed === "") {
      return;
    }
    let message;
    try {
      message = JSON.parse(trimmed);
    } catch (error) {
      log(`无法解析的帧: ${error.message}`);
      return;
    }
    Promise.resolve(dispatch(message)).catch((error) => {
      const { id } = message;
      if (id === undefined || id === null) {
        log(`通知处理失败: ${error.message}`);
        return;
      }
      log(`请求处理失败: ${error.message}`);
      fail(id, -32000, error.message);
    });
  });
  reader.on("close", () => {
    log("stdin 关闭，退出");
    process.exit(0);
  });
}

main();
