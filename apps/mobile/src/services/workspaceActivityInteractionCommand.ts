import {
  canonicalInteractionKey,
  type AgentActivityInteraction,
  type AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type { WorkspaceActivitySnapshot } from "./workspaceActivityTypes";

type InteractionResponseInput = {
  action?: string;
  optionId?: string;
  payload?: Readonly<Record<string, unknown>>;
};

export function requestWorkspaceActivityInteractionResponse(input: {
  commandId: string;
  engine: AgentSessionEngine;
  interaction: AgentActivityInteraction;
  response: InteractionResponseInput;
  states: WorkspaceActivitySnapshot["interactionStates"];
  timeoutMs: number;
  workspaceId: string;
}): boolean {
  const interaction = input.interaction;
  const state =
    input.states[
      canonicalInteractionKey(
        interaction.agentSessionId,
        interaction.turnId,
        interaction.requestId
      )
    ];
  if (!state || state.submitting || !state.runtimeAvailable) return false;
  input.engine.dispatch({
    ...input.response,
    agentSessionId: interaction.agentSessionId,
    commandId: input.commandId,
    requestId: interaction.requestId,
    retry: false,
    timeoutMs: input.timeoutMs,
    turnId: interaction.turnId,
    type: "interaction/responseRequested",
    workspaceId: input.workspaceId
  });
  return true;
}
