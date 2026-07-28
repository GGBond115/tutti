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

test("Claude fork includes trailing system messages in its exact checkpoint", async () => {
  const calls: Array<Record<string, unknown>> = [];
  const sourceWithSystem = [
    message("user", "prompt-1", { role: "user", content: "one" }),
    message("assistant", "answer-1", {
      role: "assistant",
      content: "first"
    }),
    message("system", "compact-1", {
      subtype: "compact_boundary",
      compact_metadata: { trigger: "auto" }
    })
  ];
  const childWithSystem = [
    message(
      "user",
      "child-prompt-1",
      { role: "user", content: "one" },
      "child"
    ),
    message(
      "assistant",
      "child-answer-1",
      { role: "assistant", content: "first" },
      "child"
    ),
    message(
      "system",
      "child-compact-1",
      {
        subtype: "compact_boundary",
        compact_metadata: { trigger: "auto" }
      },
      "child"
    )
  ];
  const transcriptReads: Array<Record<string, unknown>> = [];
  const sdk = {
    getSessionMessages: async (
      sessionId: string,
      options?: Record<string, unknown>
    ) => {
      transcriptReads.push({ sessionId, options });
      return sessionId === "child" ? childWithSystem : sourceWithSystem;
    },
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

  const result = await forkClaudeSession(
    {
      sessionId: "source",
      providerTurnId: "prompt-1",
      providerTurnIds: ["prompt-1"],
      cwd: "/workspace",
      title: "Source (2)"
    },
    sdk
  );

  assert.deepEqual(result.targetProviderTurnIds, ["child-prompt-1"]);
  assert.deepEqual(calls, [
    {
      sessionId: "source",
      options: {
        dir: "/workspace",
        upToMessageId: "compact-1",
        title: "Source (2)"
      }
    }
  ]);
  assert.equal(transcriptReads.length, 3);
  for (const read of transcriptReads) {
    assert.deepEqual(read.options, {
      dir: "/workspace",
      includeSystemMessages: true
    });
  }
});

test("Claude fork reports unknown when child omits a trailing system message", async () => {
  const sourceWithSystem = [
    message("user", "prompt-1", { role: "user", content: "one" }),
    message("assistant", "answer-1", {
      role: "assistant",
      content: "first"
    }),
    message("system", "compact-1", {
      subtype: "compact_boundary"
    })
  ];
  const sdk = {
    getSessionMessages: async (sessionId: string) =>
      sessionId === "child" ? child : sourceWithSystem,
    getSessionInfo: async () => ({
      sessionId: "child",
      summary: "child",
      lastModified: 1
    }),
    forkSession: async () => ({ sessionId: "child" })
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

test("Claude fork can branch again from a provider-owned child", async () => {
  const calls: Array<Record<string, unknown>> = [];
  const grandchild = [
    message(
      "user",
      "grandchild-prompt-1",
      { role: "user", content: "one" },
      "grandchild"
    ),
    message(
      "assistant",
      "grandchild-answer-1",
      { role: "assistant", content: "first" },
      "grandchild"
    )
  ];
  const sdk = {
    getSessionMessages: async (sessionId: string) => {
      if (sessionId === "child") {
        return child;
      }
      if (sessionId === "grandchild") {
        return grandchild;
      }
      return source;
    },
    getSessionInfo: async (sessionId: string) => ({
      sessionId,
      summary: "grandchild",
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
      return { sessionId: "grandchild" };
    }
  };

  const result = await forkClaudeSession(
    {
      sessionId: "child",
      providerTurnId: "child-prompt-1",
      providerTurnIds: ["child-prompt-1"],
      cwd: "/workspace",
      title: "Grandchild"
    },
    sdk
  );

  assert.equal(result.providerSessionId, "grandchild");
  assert.deepEqual(result.targetProviderTurnIds, ["grandchild-prompt-1"]);
  assert.deepEqual(calls, [
    {
      sessionId: "child",
      options: {
        dir: "/workspace",
        upToMessageId: "child-answer-1",
        title: "Grandchild"
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
  type: "user" | "assistant" | "system",
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
