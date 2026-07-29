import { describe, expect, it } from "vitest";
import {
  createAgentActivityEphemeralConversationProjector,
  type AgentActivityEphemeralConversationProjector
} from "@tutti-os/agent-activity-core";
import type { AgentSideUpdatedPayloadV1 } from "@tutti-os/event-protocol";
import { normalizeAgentSideConversationEvent } from "./agentSideConversationProjection";
import { projectAgentSideConversationVM } from "./agentSideConversationViewProjection";

function projector(): AgentActivityEphemeralConversationProjector {
  return createAgentActivityEphemeralConversationProjector({
    workspaceId: "workspace-1",
    agentSessionId: "side-1",
    sourceAgentSessionId: "source-1",
    provider: "codex",
    cwd: "/workspace",
    occurredAtUnixMs: 100
  });
}

function apply(
  subject: AgentActivityEphemeralConversationProjector,
  event: AgentSideUpdatedPayloadV1
) {
  subject.apply(normalizeAgentSideConversationEvent(event));
}

describe("Agent Side conversation projection", () => {
  it("routes Side messages through the shared conversation projection", () => {
    const subject = projector();
    apply(subject, {
      workspaceId: "workspace-1",
      sideAgentSessionId: "side-1",
      sourceAgentSessionId: "source-1",
      sequence: 1,
      eventType: "state_patch",
      data: {
        currentPhase: "working",
        turnLifecycle: { activeTurnId: "turn-1" },
        turn: {
          turnId: "turn-1",
          activeTurnId: "turn-1",
          phase: "running",
          origin: "user_prompt",
          startedAtUnixMs: 101
        }
      }
    });
    apply(subject, {
      workspaceId: "workspace-1",
      sideAgentSessionId: "side-1",
      sourceAgentSessionId: "source-1",
      sequence: 2,
      eventType: "message_update",
      data: {
        messageId: "thinking-1",
        turnId: "turn-1",
        role: "assistant",
        kind: "reasoning",
        status: "completed",
        payload: { text: "Inspecting the source turn." },
        occurredAtUnixMs: 102
      }
    });
    apply(subject, {
      workspaceId: "workspace-1",
      sideAgentSessionId: "side-1",
      sourceAgentSessionId: "source-1",
      sequence: 3,
      eventType: "message_update",
      data: {
        messageId: "tool-1",
        turnId: "turn-1",
        role: "assistant",
        kind: "tool_call",
        status: "running",
        callId: "call-1",
        payload: {
          toolName: "shell",
          input: { command: "git status" }
        },
        occurredAtUnixMs: 103
      }
    });
    apply(subject, {
      workspaceId: "workspace-1",
      sideAgentSessionId: "side-1",
      sourceAgentSessionId: "source-1",
      sequence: 4,
      eventType: "message_delta",
      data: {
        messageId: "assistant-1",
        turnId: "turn-1",
        role: "assistant",
        kind: "text",
        content: { operation: "set", text: "The parent is still running." },
        status: "completed",
        occurredAtUnixMs: 104
      }
    });

    const conversation = projectAgentSideConversationVM(subject.getSnapshot());
    expect(conversation?.activity.sessionId).toBe("side-1");
    expect(
      conversation?.rows.some(
        (row) =>
          row.kind === "tool-group" &&
          row.calls[0]?.payload?.toolName === "shell"
      )
    ).toBe(true);
    expect(
      conversation?.rows.flatMap((row) =>
        row.kind === "message" ? row.thinking.map((item) => item.body) : []
      )
    ).toContain("Inspecting the source turn.");
    expect(
      conversation?.rows.flatMap((row) =>
        row.kind === "message"
          ? row.messages.map((message) => message.body)
          : []
      )
    ).toContain("The parent is still running.");
  });
});
