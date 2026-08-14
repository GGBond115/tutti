import { describe, expect, it } from "vitest";
import { createLocalAgentGUIAgentTarget } from "../../../agentTargets";
import { projectAgentGUISlashStatusExecutionContext } from "./useAgentGUIProjectedSlashStatus";

const activeConversation = {
  id: "session-1",
  agentTargetId: "personal-agent:codex",
  provider: "codex" as const,
  title: "Cloud session",
  status: "completed" as const,
  cwd: " /workspace/project-a ",
  updatedAtUnixMs: 1
};

const cloudTarget = {
  ...createLocalAgentGUIAgentTarget("codex"),
  agentTargetId: "personal-agent:codex",
  sessionLaunchMode: "cloud" as const
};

describe("projectAgentGUISlashStatusExecutionContext", () => {
  it("preserves the existing status when the Host capability is disabled", () => {
    const baseSlashStatus = { limits: [] };

    const projected = projectAgentGUISlashStatusExecutionContext({
      activeConversation,
      baseSlashStatus,
      enabled: false,
      selectedAgentTarget: cloudTarget
    });

    expect(projected).toBe(baseSlashStatus);
    expect(projected).not.toHaveProperty("cwd");
    expect(projected).not.toHaveProperty("executionLocation");
  });

  it("adds cwd and execution location after the Host opts in", () => {
    const projected = projectAgentGUISlashStatusExecutionContext({
      activeConversation,
      baseSlashStatus: { limits: [] },
      enabled: true,
      selectedAgentTarget: cloudTarget
    });

    expect(projected).toMatchObject({
      cwd: "/workspace/project-a",
      executionLocation: "cloud"
    });
  });
});
