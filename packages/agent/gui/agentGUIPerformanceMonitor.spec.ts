import {
  canonicalTurnKey,
  type AgentSessionEngine,
  type AgentSessionEngineState
} from "@tutti-os/agent-activity-core";
import { describe, expect, it, vi } from "vitest";
import {
  agentGUIPerformanceDuration,
  createAgentGUIPerformanceMonitor
} from "./agentGUIPerformanceMonitor";

describe("createAgentGUIPerformanceMonitor", () => {
  it.each([
    [0, "lt_1s"],
    [999, "lt_1s"],
    [1_000, "1s_to_3s"],
    [3_000, "3s_to_10s"],
    [10_000, "10s_to_30s"],
    [30_000, "30s_to_60s"],
    [60_000, "gte_60s"]
  ] as const)("buckets %d ms as %s", (durationMs, durationBucket) => {
    expect(agentGUIPerformanceDuration(durationMs)).toEqual({
      durationBucket,
      durationMs
    });
  });

  it("reports an exact early queued first-token duration after exact Turn binding", () => {
    let nowUnixMs = 1_000;
    const harness = createEngineHarness(
      engineState({
        submits: {
          "submit-1": pendingSubmit({ status: "requested", turnId: null })
        }
      })
    );
    const onEvent = vi.fn();
    const monitor = createAgentGUIPerformanceMonitor({
      engine: harness.engine,
      nowUnixMs: () => nowUnixMs,
      onEvent,
      subscribeSessionEvents: harness.subscribeSessionEvents
    });

    nowUnixMs = 2_000;
    harness.emitSessionEvent(messageDelta({ turnId: "stale-turn" }));
    nowUnixMs = 61_500;
    const firstToken = messageDelta({ kind: "reasoning", turnId: "turn-1" });
    harness.emitSessionEvent(firstToken);
    expect(onEvent).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "prompt_first_token_received" })
    );

    harness.setState(
      engineState({
        submits: {
          "submit-1": pendingSubmit({ status: "accepted", turnId: "turn-1" })
        }
      })
    );
    harness.emitSessionEvent(firstToken);

    expect(onEvent).toHaveBeenCalledWith({
      agentSessionId: "session-1",
      durationBucket: "gte_60s",
      durationMs: 60_500,
      firstTokenKind: "reasoning",
      observedAtUnixMs: 61_500,
      operationId: "submit-1",
      provider: "codex",
      queued: true,
      source: "submit",
      startedAtUnixMs: 1_000,
      turnId: "turn-1",
      type: "prompt_first_token_received",
      workspaceId: "workspace-1"
    });
    expect(
      onEvent.mock.calls.filter(
        ([event]) => event.type === "prompt_first_token_received"
      )
    ).toHaveLength(1);
    expect(onEvent).not.toHaveBeenCalledWith(
      expect.objectContaining({ turnId: "stale-turn" })
    );

    monitor.dispose();
  });

  it("reports activation, initial-prompt admission, first token, and settled Turn", () => {
    let nowUnixMs = 10_000;
    const activation = pendingActivation({ status: "requested" });
    const harness = createEngineHarness(
      engineState({ activations: { "activation-1": activation } })
    );
    const onEvent = vi.fn();
    const monitor = createAgentGUIPerformanceMonitor({
      engine: harness.engine,
      nowUnixMs: () => nowUnixMs,
      onEvent,
      subscribeSessionEvents: harness.subscribeSessionEvents
    });

    nowUnixMs = 11_200;
    harness.setState(
      engineState({
        activations: {
          "activation-1": pendingActivation({ status: "confirmed" })
        }
      })
    );

    expect(onEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        durationBucket: "1s_to_3s",
        durationMs: 1_200,
        mode: "new",
        outcome: "confirmed",
        type: "session_activation_settled"
      })
    );
    expect(onEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        durationMs: 1_200,
        operationId: "submit-new",
        outcome: "accepted",
        source: "activation",
        type: "prompt_admission_settled"
      })
    );

    nowUnixMs = 12_000;
    harness.emitSessionEvent(
      messageDelta({
        content: { operation: "set", value: [{ text: "private token" }] },
        kind: "plan",
        turnId: "turn-new"
      })
    );
    harness.emitSessionEvent(
      messageDelta({ role: "user", turnId: "turn-new" })
    );
    harness.emitSessionEvent(messageDelta({ text: "  ", turnId: "turn-new" }));

    expect(onEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        durationMs: 2_000,
        firstTokenKind: "plan",
        operationId: "submit-new",
        source: "activation",
        turnId: "turn-new",
        type: "prompt_first_token_received"
      })
    );
    expect(JSON.stringify(onEvent.mock.calls)).not.toContain("private token");

    harness.setState(
      engineState({
        activations: {
          "activation-1": pendingActivation({ status: "confirmed" })
        },
        turns: {
          [canonicalTurnKey("session-1", "turn-new")]: {
            agentSessionId: "session-1",
            origin: "user_prompt",
            outcome: "completed",
            phase: "settled",
            settledAtUnixMs: 13_000,
            startedAtUnixMs: 10_500,
            turnId: "turn-new",
            updatedAtUnixMs: 13_000
          }
        }
      })
    );

    expect(onEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        durationBucket: "1s_to_3s",
        durationMs: 2_500,
        outcome: "completed",
        turnId: "turn-new",
        type: "turn_settled"
      })
    );

    monitor.dispose();
  });
});

function createEngineHarness(initialState: AgentSessionEngineState) {
  let state = initialState;
  const engineListeners = new Set<() => void>();
  const sessionListeners = new Set<(event: unknown) => void>();
  const engine = {
    getSnapshot: () => state,
    identity: { origin: "local", workspaceId: "workspace-1" },
    subscribe: (listener: () => void) => {
      engineListeners.add(listener);
      return () => engineListeners.delete(listener);
    }
  } as unknown as AgentSessionEngine;
  return {
    emitSessionEvent(event: unknown) {
      for (const listener of sessionListeners) listener(event);
    },
    engine,
    setState(nextState: AgentSessionEngineState) {
      state = nextState;
      for (const listener of engineListeners) listener();
    },
    subscribeSessionEvents(listener: (event: unknown) => void) {
      sessionListeners.add(listener);
      return () => sessionListeners.delete(listener);
    }
  };
}

function engineState(input: {
  activations?: Record<string, ReturnType<typeof pendingActivation>>;
  submits?: Record<string, ReturnType<typeof pendingSubmit>>;
  turns?: Record<string, Record<string, unknown>>;
}): AgentSessionEngineState {
  return {
    pendingIntents: {
      activationsByRequestId: input.activations ?? {},
      inactiveSessionIds: {},
      submitsByClientSubmitId: input.submits ?? {}
    },
    sessionLifecycle: {
      sessionsById: {
        "session-1": {
          agentSessionId: "session-1",
          provider: "codex"
        }
      },
      turnsById: input.turns ?? {}
    }
  } as unknown as AgentSessionEngineState;
}

function pendingSubmit(input: {
  status: "accepted" | "requested";
  turnId: string | null;
}) {
  return {
    acceptedSessionVersion: null,
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "prompt", type: "text" as const }],
    errorCode: null,
    errorMessage: null,
    expiresAtUnixMs: 121_000,
    requestedAtUnixMs: 1_000,
    status: input.status,
    submitDiagnostics: {
      queued: true,
      source: "agent-gui",
      submittedAtUnixMs: 1_000
    },
    turnId: input.turnId,
    workspaceId: "workspace-1"
  };
}

function pendingActivation(input: { status: "confirmed" | "requested" }) {
  return {
    agentSessionId: "session-1",
    agentTargetId: "codex",
    clientSubmitId: "submit-new",
    content: [{ text: "prompt", type: "text" as const }],
    cwd: "/workspace",
    errorCode: null,
    errorMessage: null,
    expiresAtUnixMs: 130_000,
    initialPromptRetracted: false,
    initialTurnExpected: true,
    mode: "new" as const,
    requestId: "activation-1",
    requestedAtUnixMs: 10_000,
    status: input.status,
    submitDiagnostics: {
      queued: false,
      source: "agent-gui",
      submittedAtUnixMs: 10_000
    },
    title: null,
    workspaceId: "workspace-1"
  };
}

function messageDelta(input: {
  content?: Record<string, unknown>;
  kind?: string;
  role?: string;
  text?: string;
  turnId: string;
}) {
  return {
    agentSessionId: "session-1",
    data: {
      agentSessionId: "session-1",
      content: input.content ?? {
        operation: "append_text",
        text: input.text ?? "token"
      },
      eventType: "message_delta",
      kind: input.kind ?? "text",
      messageId: `message-${input.turnId}`,
      occurredAtUnixMs: 1,
      role: input.role ?? "assistant",
      turnId: input.turnId,
      workspaceId: "workspace-1"
    },
    eventType: "message_delta",
    workspaceId: "workspace-1"
  };
}
