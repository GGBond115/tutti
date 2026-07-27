import type {
  AgentActivitySession,
  AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentSessionDetailResponse } from "@tutti-os/client-tuttid-ts";
import {
  agentActivitySessionFromTuttidSession,
  agentActivityTurnFromTuttidTurn,
  type AgentActivitySessionMappingOptions
} from "./mappers.ts";

export interface AgentActivitySessionDetailSnapshot {
  session: AgentActivitySession;
  childSessions: readonly AgentActivitySession[];
  turns: readonly AgentActivityTurn[];
}

/**
 * Maps one authoritative tuttid detail response without performing transport
 * or dispatch work. Hosts feed the returned aggregate to the engine through a
 * single `session/detailSnapshotReceived` intent so root, child, and Turn
 * state cannot become observably half-applied.
 */
export function agentActivitySessionDetailFromTuttid(
  workspaceId: string,
  detail: WorkspaceAgentSessionDetailResponse,
  options: AgentActivitySessionMappingOptions
): AgentActivitySessionDetailSnapshot {
  return {
    session: agentActivitySessionFromTuttidSession(
      workspaceId,
      detail.session,
      options
    ),
    childSessions: detail.childSessions.map((session) =>
      agentActivitySessionFromTuttidSession(workspaceId, session, options)
    ),
    turns: detail.turns.map(agentActivityTurnFromTuttidTurn)
  };
}
