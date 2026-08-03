import type {
  AgentActivityDurableMessage,
  AgentActivityGoalControlResult,
  AgentActivitySession,
  AgentActivitySessionDetailSnapshot
} from "@tutti-os/agent-activity-core";
import {
  agentActivityGoalControlResultFromTuttid,
  agentActivityMessageFromTuttidMessage,
  agentActivitySessionDetailFromTuttid,
  agentActivitySessionFromTuttidSession
} from "@tutti-os/agent-activity-tuttid-adapter";

export interface MobileAgentActivityMapping {
  mapGoalControlResult(
    response: Parameters<typeof agentActivityGoalControlResultFromTuttid>[1]
  ): AgentActivityGoalControlResult;
  mapMessage(
    message: Parameters<typeof agentActivityMessageFromTuttidMessage>[1]
  ): AgentActivityDurableMessage;
  mapSession(
    session: Parameters<typeof agentActivitySessionFromTuttidSession>[1]
  ): AgentActivitySession;
  mapSessionDetail(
    expectedAgentSessionId: string,
    detail: Parameters<typeof agentActivitySessionDetailFromTuttid>[2]
  ): AgentActivitySessionDetailSnapshot;
}

export function createMobileAgentActivityMapping(input: {
  currentUserId: string;
  workspaceId: string;
}): MobileAgentActivityMapping {
  const options = { currentUserId: input.currentUserId };
  return {
    mapGoalControlResult: (response) =>
      agentActivityGoalControlResultFromTuttid(
        input.workspaceId,
        response,
        options
      ),
    mapMessage: (message) =>
      agentActivityMessageFromTuttidMessage(input.workspaceId, message),
    mapSession: (session) =>
      agentActivitySessionFromTuttidSession(
        input.workspaceId,
        session,
        options
      ),
    mapSessionDetail: (expectedAgentSessionId, detail) =>
      agentActivitySessionDetailFromTuttid(
        input.workspaceId,
        expectedAgentSessionId,
        detail,
        options
      )
  };
}
