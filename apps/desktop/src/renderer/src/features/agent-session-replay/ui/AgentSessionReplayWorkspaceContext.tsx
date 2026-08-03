import { createContext, useContext } from "react";
import type { AgentSessionReplayWorkspaceCoordinator } from "../services/agentSessionReplayWorkspaceCoordinator.ts";

const AgentSessionReplayWorkspaceContext =
  createContext<AgentSessionReplayWorkspaceCoordinator | null>(null);

export const AgentSessionReplayWorkspaceProvider =
  AgentSessionReplayWorkspaceContext.Provider;

export function useAgentSessionReplayWorkspaceCoordinator(): AgentSessionReplayWorkspaceCoordinator | null {
  return useContext(AgentSessionReplayWorkspaceContext);
}
