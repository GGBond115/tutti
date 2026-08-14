import type { AgentGUIAgentTarget, AgentGUIProvider } from "../../../types";
import { resolveAgentGUISessionLaunchTarget } from "../../../agentTargets";
import type {
  AgentGUISessionLaunchMode,
  AgentGUISessionLaunchPreferenceMode
} from "../model/agentSessionLaunchMode";
import { useStableEventCallback } from "./agentGUIViewUtils";

export function useAgentGUISessionLaunchModeChange(input: {
  onPreferenceChange?: (input: {
    mode: AgentGUISessionLaunchPreferenceMode;
    projectSectionKey: string;
  }) => void | Promise<void>;
  onSelectAgentTarget: (input: {
    provider: AgentGUIProvider;
    agentTargetId: string;
  }) => void;
  selectedAgentTarget: AgentGUIAgentTarget | null;
  selectedProjectSectionKey: string;
}): (mode: AgentGUISessionLaunchMode) => void {
  return useStableEventCallback((mode: AgentGUISessionLaunchMode) => {
    if (!input.selectedAgentTarget) return;
    const nextTarget = resolveAgentGUISessionLaunchTarget({
      mode: mode === "cloud" ? "cloud" : "local",
      target: input.selectedAgentTarget
    });
    const nextAgentTargetId = nextTarget?.agentTargetId?.trim() ?? "";
    if (
      nextTarget &&
      nextAgentTargetId &&
      nextAgentTargetId !== input.selectedAgentTarget.agentTargetId
    ) {
      input.onSelectAgentTarget({
        provider: nextTarget.provider,
        agentTargetId: nextAgentTargetId
      });
    }
    if (
      mode !== "cloud" &&
      input.selectedProjectSectionKey &&
      input.onPreferenceChange
    ) {
      void input.onPreferenceChange({
        mode,
        projectSectionKey: input.selectedProjectSectionKey
      });
    }
  });
}
