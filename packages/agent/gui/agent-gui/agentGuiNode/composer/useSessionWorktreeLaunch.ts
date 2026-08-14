import { useEffect, useState } from "react";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import type { AgentGUIAgentTarget } from "../../../types";
import { resolveAgentGUISessionLaunchTarget } from "../../../agentTargets";
import type { AgentGUISessionLaunchMode } from "../model/agentSessionLaunchMode";

export interface SessionWorktreeLaunchState {
  mode: AgentGUISessionLaunchMode;
  visible: boolean;
  availableModes: readonly AgentGUISessionLaunchMode[];
  onModeChange: (mode: AgentGUISessionLaunchMode) => void;
}

export function useSessionWorktreeLaunch(input: {
  agentSessionId?: string | null;
  worktreeEnabled?: boolean;
  mode?: AgentGUISessionLaunchMode;
  onModeChange?: (mode: AgentGUISessionLaunchMode) => void | Promise<void>;
  projectSectionKey?: string | null;
  selectedAgentTarget?: AgentGUIAgentTarget | null;
  selectedProjectPath?: string | null;
}): SessionWorktreeLaunchState {
  const hostApi = useOptionalAgentHostApi();
  const [support, setSupport] = useState<{
    key: string;
    supported: boolean;
  } | null>(null);
  const localTarget = input.selectedAgentTarget
    ? resolveAgentGUISessionLaunchTarget({
        mode: "local",
        target: input.selectedAgentTarget
      })
    : null;
  const agentTargetId = localTarget?.agentTargetId?.trim() ?? "";
  const cwd = input.selectedProjectPath?.trim() ?? "";
  const projectSectionKey = input.projectSectionKey?.trim() ?? "";
  const resolveSupport = hostApi?.workspace.resolveSessionWorktreeSupport;
  const worktreeEligible =
    input.worktreeEnabled === true &&
    !input.agentSessionId?.trim() &&
    input.selectedAgentTarget?.ownership === "self" &&
    Boolean(agentTargetId && cwd && projectSectionKey) &&
    typeof resolveSupport === "function" &&
    typeof input.onModeChange === "function";
  const probeKey = worktreeEligible ? `${agentTargetId}\u0000${cwd}` : "";

  useEffect(() => {
    let active = true;
    if (!worktreeEligible) {
      return () => {
        active = false;
      };
    }
    void Promise.resolve(resolveSupport!({ agentTargetId, cwd }))
      .then((result) => {
        if (active) {
          setSupport({ key: probeKey, supported: result.supported === true });
        }
      })
      .catch(() => {
        if (active) {
          setSupport({ key: probeKey, supported: false });
        }
      });
    return () => {
      active = false;
    };
  }, [agentTargetId, cwd, probeKey, resolveSupport, worktreeEligible]);

  const worktreeVisible =
    worktreeEligible && support?.key === probeKey && support.supported === true;
  const cloudTarget = input.selectedAgentTarget
    ? resolveAgentGUISessionLaunchTarget({
        mode: "cloud",
        target: input.selectedAgentTarget
      })
    : null;
  const cloudVisible = Boolean(
    !input.agentSessionId?.trim() &&
    input.selectedAgentTarget?.ownership === "self" &&
    typeof input.onModeChange === "function" &&
    cloudTarget &&
    cloudTarget.disabled !== true
  );
  const visible = worktreeVisible || cloudVisible;
  const selectedMode = input.selectedAgentTarget?.sessionLaunchMode;
  const mode =
    cloudVisible && selectedMode === "cloud"
      ? "cloud"
      : worktreeVisible && input.mode === "worktree"
        ? "worktree"
        : "local";
  const availableModes: AgentGUISessionLaunchMode[] = ["local"];
  if (worktreeVisible) availableModes.push("worktree");
  if (cloudVisible) availableModes.push("cloud");
  return {
    mode,
    visible,
    availableModes,
    onModeChange: (nextMode) => {
      if (!visible || nextMode === mode || !availableModes.includes(nextMode)) {
        return;
      }
      void input.onModeChange?.(nextMode);
    }
  };
}
