import {
  loadAllAgentSessionMessages,
  type AgentActivityAdapter,
  type AgentActivityMessage,
  type AgentActivityMessagePage
} from "@tutti-os/agent-activity-core";
import { reconcileAfterVersion } from "./workspaceAgentActivityDiagnostics.ts";

interface ReconcileAgentSessionMessagePagesInput {
  adapter: AgentActivityAdapter;
  agentSessionId: string;
  cached: AgentActivityMessage[];
  messageWindowKnown: boolean;
  shouldAbort: () => boolean;
  workspaceId: string;
}

export async function reconcileAgentSessionMessagePages(
  input: ReconcileAgentSessionMessagePagesInput
): Promise<AgentActivityMessagePage> {
  if (input.cached.length === 0 || !input.messageWindowKnown) {
    return input.adapter.listSessionMessages({
      workspaceId: input.workspaceId,
      agentSessionId: input.agentSessionId,
      limit: 100,
      order: "desc"
    });
  }

  const afterVersion = reconcileAfterVersion(input.cached);
  const result = await loadAllAgentSessionMessages({
    afterVersion,
    listPage: (cursor) =>
      input.adapter.listSessionMessages({
        workspaceId: input.workspaceId,
        agentSessionId: input.agentSessionId,
        afterVersion: cursor,
        order: "asc"
      }),
    shouldAbort: input.shouldAbort
  });
  return {
    hasMore: false,
    latestVersion: result.messages.reduce(
      (latest, message) => Math.max(latest, message.version),
      afterVersion
    ),
    messages: result.messages
  };
}
