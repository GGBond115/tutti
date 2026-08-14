import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentGUIAgentTarget } from "../../../types";
import { useAgentGUISessionLaunchModeChange } from "./useAgentGUISessionLaunchModeChange";

const localTarget: AgentGUIAgentTarget = {
  targetId: "local:codex",
  agentTargetId: "local:codex",
  label: "Codex",
  ownership: "self",
  provider: "codex",
  ref: { kind: "local", provider: "codex" },
  sessionLaunchMode: "local",
  sessionLaunchTargets: [
    {
      mode: "local",
      agentTargetId: "local:codex",
      availability: { status: "ready" },
      setupKind: "target_runtime"
    },
    {
      mode: "cloud",
      agentTargetId: "personal-agent:codex",
      availability: { status: "ready" },
      setupKind: null
    }
  ]
};

describe("useAgentGUISessionLaunchModeChange", () => {
  it("selects the exact Cloud target without changing project preference", () => {
    const onPreferenceChange = vi.fn();
    const onSelectAgentTarget = vi.fn();
    const { result } = renderHook(() =>
      useAgentGUISessionLaunchModeChange({
        onPreferenceChange,
        onSelectAgentTarget,
        selectedAgentTarget: localTarget,
        selectedProjectSectionKey: "project:/workspace"
      })
    );

    act(() => result.current("cloud"));

    expect(onSelectAgentTarget).toHaveBeenCalledWith({
      provider: "codex",
      agentTargetId: "personal-agent:codex"
    });
    expect(onPreferenceChange).not.toHaveBeenCalled();
  });

  it("returns to the exact local target before enabling Worktree", () => {
    const onPreferenceChange = vi.fn();
    const onSelectAgentTarget = vi.fn();
    const { result } = renderHook(() =>
      useAgentGUISessionLaunchModeChange({
        onPreferenceChange,
        onSelectAgentTarget,
        selectedAgentTarget: {
          ...localTarget,
          agentTargetId: "personal-agent:codex",
          sessionLaunchMode: "cloud"
        },
        selectedProjectSectionKey: "project:/workspace"
      })
    );

    act(() => result.current("worktree"));

    expect(onSelectAgentTarget).toHaveBeenCalledWith({
      provider: "codex",
      agentTargetId: "local:codex"
    });
    expect(onPreferenceChange).toHaveBeenCalledWith({
      mode: "worktree",
      projectSectionKey: "project:/workspace"
    });
  });
});
