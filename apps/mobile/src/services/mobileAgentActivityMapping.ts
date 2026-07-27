import type { AgentActivitySession } from "@tutti-os/agent-activity-core";
import {
  agentActivitySessionDetailFromTuttid,
  agentActivitySessionFromTuttidSession,
  type AgentActivitySessionDetailSnapshot
} from "@tutti-os/agent-activity-tuttid-adapter";

export interface MobileAgentActivityMapping {
  mapSession(
    session: Parameters<typeof agentActivitySessionFromTuttidSession>[1]
  ): AgentActivitySession;
  mapSessionDetail(
    detail: Parameters<typeof agentActivitySessionDetailFromTuttid>[1]
  ): AgentActivitySessionDetailSnapshot;
}

export function createMobileAgentActivityMapping(input: {
  currentUserId: string;
  workspaceId: string;
}): MobileAgentActivityMapping {
  const options = { currentUserId: input.currentUserId };
  return {
    mapSession: (session) =>
      agentActivitySessionFromTuttidSession(
        input.workspaceId,
        session,
        options
      ),
    mapSessionDetail: (detail) =>
      agentActivitySessionDetailFromTuttid(input.workspaceId, detail, options)
  };
}
