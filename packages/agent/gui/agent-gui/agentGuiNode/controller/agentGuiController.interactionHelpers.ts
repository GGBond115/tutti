import type { AgentActivityInteraction } from "@tutti-os/agent-activity-core";
import type { AgentGUIInteractionReadinessIdentity } from "../../../types";

export interface AgentGUIInteractionTarget {
  agentSessionId: string;
  turnId: string;
}

export function resolveAgentGUIInteractionTarget(
  interactions: readonly AgentActivityInteraction[],
  requestId: string
): AgentGUIInteractionTarget | null {
  const normalizedRequestId = requestId.trim();
  if (!normalizedRequestId) return null;
  for (let index = interactions.length - 1; index >= 0; index -= 1) {
    const interaction = interactions[index];
    if (interaction?.requestId.trim() !== normalizedRequestId) continue;
    const agentSessionId = interaction.agentSessionId.trim();
    const turnId = interaction.turnId.trim();
    if (!agentSessionId || !turnId) return null;
    return { agentSessionId, turnId };
  }
  return null;
}

export function resolveAgentGUIInteractionReadinessIdentity(input: {
  interactions: readonly AgentActivityInteraction[];
  requestId: string | null | undefined;
  workspaceId: string;
}): AgentGUIInteractionReadinessIdentity | null {
  const target = resolveAgentGUIInteractionTarget(
    input.interactions,
    input.requestId?.trim() ?? ""
  );
  const workspaceId = input.workspaceId.trim();
  const requestId = input.requestId?.trim() ?? "";
  return target && workspaceId && requestId
    ? { workspaceId, requestId, ...target }
    : null;
}
