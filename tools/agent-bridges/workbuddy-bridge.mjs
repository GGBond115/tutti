#!/usr/bin/env node
import { spawn, execFileSync } from "node:child_process";
import { createInterface } from "node:readline";
import {
  existsSync,
  statSync,
  copyFileSync,
  mkdirSync,
  readFileSync
} from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { randomUUID } from "node:crypto";

const EXTENSION_AUTH_DIR = join(
  homedir(),
  "Library",
  "Application Support",
  "CodeBuddyExtension",
  "Data",
  "Public",
  "auth"
);
const APP_AUTH_FILE = join(EXTENSION_AUTH_DIR, "workbuddy-desktop.info");
const CLI_AUTH_FILE = join(
  EXTENSION_AUTH_DIR,
  "Tencent-Cloud.coding-copilot.info"
);
const CLI_BIN = join(
  homedir(),
  ".local",
  "share",
  "tutti",
  "codebuddy-standalone",
  "node_modules",
  ".bin",
  "codebuddy"
);
const TURN_TIMEOUT_MS = 30 * 60 * 1000;

const FALLBACK_MODELS = [
  "hy3",
  "hy3-x",
  "glm-5.3",
  "glm-5.2",
  "glm-5.1",
  "glm-5v-turbo",
  "minimax-m3",
  "minimax-m2.7",
  "kimi-k3-1",
  "kimi-k2.7",
  "kimi-k2.6",
  "deepseek-v4-pro",
  "deepseek-v4-flash"
];

let cachedModelIds = null;

function modelDisplayName(modelId) {
  if (modelId === "auto") return "自动（默认）";
  return modelId
    .split("-")
    .map((part) =>
      /^\d/.test(part) ? part : part.charAt(0).toUpperCase() + part.slice(1)
    )
    .join(" ");
}

function supportedModelIds() {
  if (cachedModelIds) return cachedModelIds;
  cachedModelIds = FALLBACK_MODELS;
  try {
    const help = execFileSync(CLI_BIN, ["--help"], {
      encoding: "utf8",
      timeout: 20000,
      maxBuffer: 4 * 1024 * 1024
    });
    const match =
      /--model <model>[^\n]*Currently supported:\s*\(([^)]*)\)/.exec(help);
    if (match) {
      const parsed = match[1]
        .split(",")
        .map((entry) => entry.trim())
        .filter(Boolean);
      if (parsed.length > 0) {
        cachedModelIds = parsed;
      }
    }
  } catch (error) {
    log(`读取模型列表失败，使用内置列表: ${error.message}`);
  }
  return cachedModelIds;
}

function modelConfigOptions(currentModel) {
  const options = [{ value: "auto", name: modelDisplayName("auto") }];
  for (const modelId of supportedModelIds()) {
    if (modelId === "auto") continue;
    options.push({ value: modelId, name: modelDisplayName(modelId) });
  }
  return [
    {
      id: "model",
      name: "Model",
      category: "model",
      type: "select",
      currentValue: currentModel,
      options
    }
  ];
}

const sessions = new Map();
const children = new Set();
const pendingAcpResponses = new Map();
let acpRequestIdCounter = 100;

function sendAcpRequest(method, params, timeoutMs = 10 * 60 * 1000) {
  return new Promise((resolve) => {
    const id = ++acpRequestIdCounter;
    pendingAcpResponses.set(id, resolve);
    send({ jsonrpc: "2.0", id, method, params });
    setTimeout(() => {
      if (pendingAcpResponses.has(id)) {
        pendingAcpResponses.delete(id);
        resolve(null);
      }
    }, timeoutMs);
  });
}

function summarizeToolInput(toolName, input) {
  const keys = ["command", "file_path", "path", "pattern", "url", "query"];
  for (const key of keys) {
    const value = input?.[key];
    if (typeof value === "string" && value.trim() !== "") {
      const trimmed = value.length > 120 ? `${value.slice(0, 120)}…` : value;
      return `${toolName}: ${trimmed}`;
    }
  }
  return toolName;
}

async function relayPermissionToTutti(session, child, event) {
  const request = event.request ?? {};
  const toolName = request.tool_name ?? "tool";
  const input = request.input ?? {};
  try {
    const result = await sendAcpRequest("session/request_permission", {
      sessionId: session.bridgeSessionId,
      toolCall: {
        toolCallId: String(request.tool_use_id || event.request_id),
        title: summarizeToolInput(toolName, input),
        name: toolName,
        rawInput: input
      },
      options: [
        { optionId: "allow_once", kind: "allow_once", name: "允许" },
        { optionId: "reject_once", kind: "reject_once", name: "拒绝" }
      ]
    });
    const optionId = String(result?.outcome?.optionId ?? "");
    const allowed = optionId.startsWith("allow");
    log(`权限请求 ${toolName} → ${allowed ? "允许" : "拒绝"}`);
    child.stdin.write(
      `${JSON.stringify({
        type: "control_response",
        response: {
          subtype: "success",
          request_id: event.request_id,
          response: { allowed }
        }
      })}\n`
    );
  } catch (error) {
    log(`权限转发失败，默认拒绝: ${error.message}`);
    try {
      child.stdin.write(
        `${JSON.stringify({
          type: "control_response",
          response: {
            subtype: "success",
            request_id: event.request_id,
            response: { allowed: false }
          }
        })}\n`
      );
    } catch {}
  }
}

function log(message) {
  process.stderr.write(`[tutti-workbuddy-bridge] ${message}\n`);
}

function send(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function reply(id, result) {
  send({ jsonrpc: "2.0", id, result });
}

function fail(id, code, message, data) {
  send({
    jsonrpc: "2.0",
    id,
    error: { code, message, ...(data ? { data } : {}) }
  });
}

function emitChunk(sessionId, text) {
  if (!text) return;
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

function authFileState() {
  if (!existsSync(APP_AUTH_FILE)) return null;
  try {
    const parsed = JSON.parse(readFileSync(APP_AUTH_FILE, "utf8"));
    const expiresAt = Number(parsed?.auth?.expiresAt) || 0;
    const account = parsed?.account ?? {};
    if (!parsed?.auth?.accessToken) return null;
    return {
      expiresAt,
      account: account.nickname ?? "",
      uid: account.uid ?? "",
      accessToken: parsed.auth.accessToken
    };
  } catch {
    return null;
  }
}

function appMemoryPath() {
  const auth = authFileState();
  if (!auth?.uid) return null;
  return join(homedir(), ".workbuddy", "memory", `${auth.uid}_memory.md`);
}

function loadAppMemory() {
  const memoryPath = appMemoryPath();
  if (!memoryPath || !existsSync(memoryPath)) return "";
  try {
    const content = readFileSync(memoryPath, "utf8").trim();
    if (!content) return "";
    log(`已注入 WorkBuddy 全局记忆 ${content.length} 字符`);
    return `<workbuddy_memory source="WorkBuddy App 全局记忆（自动注入）">\n${content}\n</workbuddy_memory>\n\n`;
  } catch (error) {
    log(`读取全局记忆失败: ${error.message}`);
    return "";
  }
}

function syncAuthFile() {
  if (!existsSync(APP_AUTH_FILE)) return;
  try {
    const sourceMtime = statSync(APP_AUTH_FILE).mtimeMs;
    const targetMtime = existsSync(CLI_AUTH_FILE)
      ? statSync(CLI_AUTH_FILE).mtimeMs
      : 0;
    if (sourceMtime > targetMtime) {
      mkdirSync(EXTENSION_AUTH_DIR, { recursive: true });
      copyFileSync(APP_AUTH_FILE, CLI_AUTH_FILE);
      log("已同步 WorkBuddy 登录凭据给 CodeBuddy CLI");
    }
  } catch (error) {
    log(`同步登录凭据失败: ${error.message}`);
  }
}

function runCheck() {
  const auth = authFileState();
  if (!auth) {
    process.stdout.write("workbuddy-bridge not authenticated\n");
    process.exit(0);
  }
  if (auth.expiresAt <= Date.now()) {
    process.stdout.write("workbuddy-bridge not authenticated\n");
    process.exit(0);
  }
  process.stdout.write("workbuddy-bridge authenticated\n");
  process.exit(0);
}

function runLogin() {
  const auth = authFileState();
  if (auth && auth.expiresAt > Date.now()) {
    log(
      `已使用 WorkBuddy 登录态（账号: ${auth.account}）。如需切换账号，请在 WorkBuddy App 内退出登录后重新登录。`
    );
    process.exit(0);
  }
  log(
    "未检测到 WorkBuddy 登录态。请打开 WorkBuddy App 并登录；登录完成后回来点击刷新即可。"
  );
  try {
    spawn("open", ["/Applications/WorkBuddy.app"], {
      detached: true,
      stdio: "ignore"
    }).unref();
  } catch {}
  process.exit(0);
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

function runTurn(session) {
  return new Promise((resolve) => {
    syncAuthFile();
    const args = [
      "-p",
      "--output-format",
      "stream-json",
      "--input-format",
      "stream-json"
    ];
    if (session.model && session.model !== "auto") {
      args.push("--model", session.model);
    }
    if (session.cliSessionId) {
      args.push("--resume", session.cliSessionId);
    }
    log(
      `启动 CodeBuddy 回合 (resume=${Boolean(session.cliSessionId)}, 模型=${session.model ?? "auto"})`
    );
    const child = spawn(CLI_BIN, args, {
      cwd: session.cwd && existsSync(session.cwd) ? session.cwd : homedir(),
      env: process.env,
      stdio: ["pipe", "pipe", "pipe"]
    });
    children.add(child);
    session.cancelled = false;
    const timer = setTimeout(() => {
      log("回合超时，终止 CLI 进程");
      try {
        child.kill("SIGKILL");
      } catch {}
    }, TURN_TIMEOUT_MS);
    session.cancelTurn = () => {
      session.cancelled = true;
      try {
        child.kill("SIGTERM");
      } catch {}
    };
    let stdoutBuffer = "";
    let stderrTail = "";
    let settled = false;
    let emittedText = "";

    const finish = (stopReason, errorMessage) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      children.delete(child);
      resolve({ stopReason, errorMessage });
    };

    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdoutBuffer += chunk;
      let index;
      while ((index = stdoutBuffer.indexOf("\n")) >= 0) {
        const line = stdoutBuffer.slice(0, index).trim();
        stdoutBuffer = stdoutBuffer.slice(index + 1);
        if (!line) continue;
        let event;
        try {
          event = JSON.parse(line);
        } catch {
          continue;
        }
        if (
          event.type === "control_request" &&
          event.request?.subtype === "can_use_tool"
        ) {
          void relayPermissionToTutti(session, child, event);
          continue;
        }
        if (event.type === "result") {
          try {
            child.kill("SIGTERM");
          } catch {}
          finish(
            session.turnError ? "refusal" : "end_turn",
            session.turnError ?? undefined
          );
          continue;
        }
        handleStreamEvent(
          session,
          event,
          () => emittedText,
          (updated) => {
            emittedText = updated;
          }
        );
      }
    });
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk) => {
      stderrTail = (stderrTail + chunk).slice(-2000);
    });
    child.on("error", (error) => {
      log(`CLI 进程错误: ${error.message}`);
      finish("refusal", error.message);
    });
    child.on("close", (code) => {
      if (session.cancelled) {
        finish("cancelled");
      } else if (code === 0) {
        finish("end_turn");
      } else {
        const authFailed = /authentication required|not logged in|login/i.test(
          stderrTail
        );
        log(`CLI 退出 code=${code}${authFailed ? " (auth)" : ""}`);
        if (authFailed) {
          finish(
            "refusal",
            "CodeBuddy 未登录：请打开 WorkBuddy App 登录后重试"
          );
        } else {
          finish(
            "refusal",
            `CodeBuddy CLI 退出码 ${code}: ${stderrTail.slice(-400)}`
          );
        }
      }
    });
    const userMessage = {
      type: "user",
      message: {
        role: "user",
        content: [{ type: "text", text: session.pendingPrompt }]
      }
    };
    child.stdin.write(`${JSON.stringify(userMessage)}\n`);
  });
}

function handleStreamEvent(session, event, getEmitted, setEmitted) {
  if (event.type === "stream_event") {
    const delta = event.event?.delta;
    if (
      event.event?.type === "content_block_delta" &&
      delta?.type === "text_delta" &&
      delta.text
    ) {
      emitChunk(session.bridgeSessionId, delta.text);
      setEmitted(getEmitted() + delta.text);
    }
    return;
  }
  if (event.type === "assistant") {
    const content = event.message?.content;
    if (!Array.isArray(content)) return;
    let completeText = "";
    for (const block of content) {
      if (block?.type === "text" && block.text) completeText += block.text;
    }
    if (!completeText) return;
    const already = getEmitted();
    if (completeText.startsWith(already)) {
      const rest = completeText.slice(already.length);
      if (rest) {
        emitChunk(session.bridgeSessionId, rest);
        setEmitted(completeText);
      }
    } else {
      emitChunk(session.bridgeSessionId, completeText);
      setEmitted(completeText);
    }
    return;
  }
  if (event.type === "system" && event.subtype === "init" && event.session_id) {
    session.cliSessionId = event.session_id;
    return;
  }
  if (event.type === "result") {
    if (event.is_error) {
      session.turnError = event.result || "CodeBuddy 回合失败";
    }
  }
}

async function handlePrompt(params) {
  const target = sessions.get(params.sessionId);
  if (!target) {
    throw new Error(`unknown session: ${params.sessionId}`);
  }
  const promptText = textOfPrompt(params.prompt);
  if (!promptText) {
    return { stopReason: "end_turn" };
  }
  target.turnError = null;
  let effectivePrompt = promptText;
  if (!target.memoryInjected) {
    target.memoryInjected = true;
    const memory = loadAppMemory();
    if (memory) {
      effectivePrompt = `${memory}${promptText}`;
    }
  }
  target.pendingPrompt = effectivePrompt;
  const outcome = await runTurn(target);
  if (outcome.stopReason !== "end_turn" && outcome.errorMessage) {
    throw new Error(outcome.errorMessage);
  }
  return { stopReason: outcome.stopReason || "end_turn" };
}

async function dispatch(message) {
  const { id, method, params } = message;
  if (method === undefined && id !== undefined && pendingAcpResponses.has(id)) {
    const resolve = pendingAcpResponses.get(id);
    pendingAcpResponses.delete(id);
    resolve(message.result ?? null);
    return;
  }
  switch (method) {
    case "initialize":
      return reply(id, {
        protocolVersion: 1,
        agentCapabilities: {
          promptCapabilities: { image: false, embeddedContext: false }
        }
      });
    case "session/new": {
      const sessionId = `workbuddy-${randomUUID()}`;
      sessions.set(sessionId, {
        bridgeSessionId: sessionId,
        cliSessionId: null,
        cwd: typeof params?.cwd === "string" ? params.cwd : "",
        model: "auto"
      });
      return reply(id, {
        sessionId,
        configOptions: modelConfigOptions("auto")
      });
    }
    case "session/set_config_option": {
      const sessionId =
        typeof params?.sessionId === "string" ? params.sessionId : "";
      const session = sessions.get(sessionId);
      const configId =
        typeof params?.configId === "string" ? params.configId : "";
      const value = typeof params?.value === "string" ? params.value : "";
      if (!session) {
        return fail(id, -32000, `unknown session: ${sessionId}`);
      }
      if (configId === "model" && value !== "") {
        session.model = value;
        log(`会话模型已切换: ${value}`);
        return reply(id, {
          configOptions: modelConfigOptions(session.model)
        });
      }
      return reply(id, {});
    }
    case "session/prompt":
      return reply(id, await handlePrompt(params ?? {}));
    case "session/cancel": {
      const sessionId =
        typeof params?.sessionId === "string" ? params.sessionId : "";
      const session = sessions.get(sessionId);
      if (session?.cancelTurn) {
        log("收到取消通知，终止当前回合");
        session.cancelTurn();
      }
      return;
    }
    default:
      if (id === undefined || id === null) return;
      return fail(id, -32601, `method not supported: ${method}`);
  }
}

function main() {
  if (process.argv.includes("--check")) {
    runCheck();
    return;
  }
  if (process.argv.includes("--login")) {
    runLogin();
    return;
  }
  if (!existsSync(CLI_BIN)) {
    log(`CodeBuddy CLI 不存在: ${CLI_BIN}`);
  }
  process.on("SIGTERM", () => {
    for (const child of children) {
      try {
        child.kill("SIGTERM");
      } catch {}
    }
    process.exit(0);
  });
  syncAuthFile();
  log("启动 WorkBuddy ACP 桥接 (codebuddy headless)");
  const reader = createInterface({ input: process.stdin, terminal: false });
  reader.on("line", (line) => {
    const trimmed = line.trim();
    if (!trimmed) return;
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
      const authFailed = /未登录|authentication|not logged in|login/i.test(
        error.message
      );
      fail(
        id,
        authFailed ? -32000 : -32603,
        error.message,
        authFailed ? { category: "auth" } : undefined
      );
    });
  });
  reader.on("close", () => {
    log("stdin 关闭，退出");
    for (const child of children) {
      try {
        child.kill("SIGTERM");
      } catch {}
    }
    process.exit(0);
  });
}

main();
