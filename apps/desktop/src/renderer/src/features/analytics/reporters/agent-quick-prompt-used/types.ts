import type { AgentChatEngagementBaseParams } from "../agent-chat-engagement-params.ts";

export interface AgentQuickPromptUsedParams extends AgentChatEngagementBaseParams {
  promptType: "saved" | "recommended_template";
  source: "composer_input";
}
