import type {
  AgentActivityGoalControlResult,
  AgentActivitySessionGoalState
} from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentSessionGoalControlResponse } from "@tutti-os/client-tuttid-ts";
import {
  agentActivitySessionFromTuttidSession,
  type AgentActivitySessionMappingOptions
} from "./mappers.ts";

export function agentActivityGoalControlResultFromTuttid(
  workspaceId: string,
  response: WorkspaceAgentSessionGoalControlResponse,
  options: AgentActivitySessionMappingOptions
): AgentActivityGoalControlResult {
  const goal = Object.prototype.hasOwnProperty.call(response, "goal")
    ? (response.goal ?? null)
    : (response.session.goal ?? null);
  return {
    goal: goal ? { ...goal } : null,
    operationId: response.operationId?.trim() || null,
    session: agentActivitySessionFromTuttidSession(
      workspaceId,
      response.session,
      options
    ),
    state: response.state ? cloneGoalState(response.state) : null
  };
}

function cloneGoalState(
  state: NonNullable<WorkspaceAgentSessionGoalControlResponse["state"]>
): AgentActivitySessionGoalState {
  return {
    ...(Object.prototype.hasOwnProperty.call(state, "desired")
      ? { desired: state.desired ? { ...state.desired } : null }
      : {}),
    ...(Object.prototype.hasOwnProperty.call(state, "observed")
      ? { observed: state.observed ? { ...state.observed } : null }
      : {}),
    lastEvidence: { ...state.lastEvidence },
    ...(state.lastError === undefined ? {} : { lastError: state.lastError }),
    ...(state.observedAtUnixMs === undefined
      ? {}
      : { observedAtUnixMs: state.observedAtUnixMs }),
    ...(state.pendingOperationId === undefined
      ? {}
      : { pendingOperationId: state.pendingOperationId }),
    revision: state.revision,
    syncStatus: state.syncStatus,
    tombstoned: state.tombstoned,
    updatedAtUnixMs: state.updatedAtUnixMs
  };
}
