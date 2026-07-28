import assert from "node:assert/strict";
import test from "node:test";
import {
  forkClaudeSession,
  inspectClaudeForkCheckpoints
} from "./sessionFork.ts";

const source = [
  message("user", "prompt-1", { role: "user", content: "one" }),
  message("assistant", "answer-1", { role: "assistant", content: "first" }),
  message("user", "prompt-2", { role: "user", content: "two" }),
  message("assistant", "answer-2", { role: "assistant", content: "second" })
];
const child = [
  message("user", "child-prompt-1", { role: "user", content: "one" }, "child"),
  message(
    "assistant",
    "child-answer-1",
    { role: "assistant", content: "first" },
    "child"
  )
];

test("Claude fork inspection exposes only root user message UUIDs", async () => {
  const result = await inspectClaudeForkCheckpoints(
    { sessionId: "source", cwd: "/workspace" },
    fakeSDK()
  );
  assert.deepEqual(result, { providerTurnIds: ["prompt-1", "prompt-2"] });
});

test("Claude fork verifies the inclusive prefix and maps remapped UUIDs", async () => {
  const calls: Array<Record<string, unknown>> = [];
  const result = await forkClaudeSession(
    {
      sessionId: "source",
      providerTurnId: "prompt-1",
      providerTurnIds: ["prompt-1"],
      cwd: "/workspace",
      title: "Source (2)"
    },
    fakeSDK(calls)
  );
  assert.equal(result.providerSessionId, "child");
  assert.deepEqual(result.targetProviderTurnIds, ["child-prompt-1"]);
  assert.equal(result.stateBindingMode, "provider_owned");
  assert.match(String(result.stateBindingReceipt), /^claude-sdk-fork-v1:/);
  assert.deepEqual(calls, [
    {
      sessionId: "source",
      options: {
        dir: "/workspace",
        upToMessageId: "answer-1",
        title: "Source (2)"
      }
    }
  ]);
});

test("Claude fork reports unknown once the SDK mutation was invoked", async () => {
  const sdk = fakeSDK();
  sdk.forkSession = async () => {
    throw new Error("connection lost");
  };
  await assert.rejects(
    forkClaudeSession(
      {
        sessionId: "source",
        providerTurnId: "prompt-1",
        providerTurnIds: ["prompt-1"],
        cwd: "/workspace",
        title: "Source (2)"
      },
      sdk
    ),
    (error: unknown) =>
      error instanceof Error &&
      "deliveryDisposition" in error &&
      error.deliveryDisposition === "unknown"
  );
});

test("Claude fork validates the checkpoint identity before SDK mutation", async () => {
  const calls: Array<Record<string, unknown>> = [];
  const sdk = fakeSDK(calls);
  sdk.getSessionMessages = async () => [
    message("user", "prompt-1", { role: "user", content: "one" }),
    message("assistant", "", { role: "assistant", content: "first" })
  ];
  await assert.rejects(
    forkClaudeSession(
      {
        sessionId: "source",
        providerTurnId: "prompt-1",
        providerTurnIds: ["prompt-1"],
        cwd: "/workspace",
        title: ""
      },
      sdk
    ),
    (error: unknown) =>
      error instanceof Error &&
      "deliveryDisposition" in error &&
      error.deliveryDisposition === "not_started"
  );
  assert.deepEqual(calls, []);
});

test("Claude fork supports an untitled canonical session", async () => {
  const calls: Array<Record<string, unknown>> = [];
  const result = await forkClaudeSession(
    {
      sessionId: "source",
      providerTurnId: "prompt-1",
      providerTurnIds: ["prompt-1"],
      cwd: "/workspace",
      title: " "
    },
    fakeSDK(calls)
  );
  assert.equal(result.providerSessionId, "child");
  assert.deepEqual(calls, [
    {
      sessionId: "source",
      options: {
        dir: "/workspace",
        upToMessageId: "answer-1"
      }
    }
  ]);
});

function fakeSDK(calls: Array<Record<string, unknown>> = []) {
  return {
    getSessionMessages: async (sessionId: string) =>
      sessionId === "child" ? child : source,
    getSessionInfo: async () => ({
      sessionId: "child",
      summary: "child",
      lastModified: 1
    }),
    forkSession: async (
      sessionId: string,
      options?: {
        dir?: string;
        upToMessageId?: string;
        title?: string;
      }
    ) => {
      calls.push({ sessionId, options });
      return { sessionId: "child" };
    }
  };
}

function message(
  type: "user" | "assistant",
  uuid: string,
  content: unknown,
  sessionId = "source"
) {
  return {
    type,
    uuid,
    session_id: sessionId,
    message: content,
    parent_tool_use_id: null
  };
}
