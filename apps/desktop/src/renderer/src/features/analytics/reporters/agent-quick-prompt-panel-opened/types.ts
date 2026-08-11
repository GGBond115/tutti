import type { AgentChatEngagementBaseParams } from "../agent-chat-engagement-params.ts";

export interface AgentQuickPromptPanelOpenedParams extends AgentChatEngagementBaseParams {
  source: "composer_input";
}
