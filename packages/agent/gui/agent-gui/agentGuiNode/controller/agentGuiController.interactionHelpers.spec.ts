import { describe, expect, it } from "vitest";
import type { AgentActivityInteraction } from "@tutti-os/agent-activity-core";
import {
  resolveAgentGUIInteractionReadinessIdentity,
  resolveAgentGUIInteractionTarget
} from "./agentGuiController.interactionHelpers";

describe("resolveAgentGUIInteractionTarget", () => {
  it("keeps the canonical child session and turn tuple", () => {
    const interactions = [
      {
        agentSessionId: "root",
        requestId: "approval-1",
        turnId: "root-turn"
      },
      {
        agentSessionId: "child-1",
        requestId: "approval-1",
        turnId: "child-turn-1"
      }
    ] as AgentActivityInteraction[];

    expect(
      resolveAgentGUIInteractionTarget(interactions, "approval-1")
    ).toEqual({
      agentSessionId: "child-1",
      turnId: "child-turn-1"
    });
  });

  it("does not fall back to the active root session for an unknown request", () => {
    expect(resolveAgentGUIInteractionTarget([], "missing")).toBeNull();
  });

  it("projects the complete Host readiness identity from the canonical target", () => {
    const interactions = [
      {
        agentSessionId: "session-1",
        requestId: "request-1",
        turnId: "turn-1"
      }
    ] as AgentActivityInteraction[];

    expect(
      resolveAgentGUIInteractionReadinessIdentity({
        interactions,
        requestId: " request-1 ",
        workspaceId: " workspace-1 "
      })
    ).toEqual({
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      turnId: "turn-1",
      requestId: "request-1"
    });
  });
});
