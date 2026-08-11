import {
  BaseAnalyticsReporter,
  type AnalyticsReporterDependencies,
  type AnalyticsReporterParams
} from "../baseReporter.ts";
import { projectAgentChatEngagementBaseParams } from "../agent-chat-engagement-params.ts";
import type { AgentQuickPromptUsedParams } from "./types.ts";

export class AgentQuickPromptUsedReporter extends BaseAnalyticsReporter<AnalyticsReporterParams> {
  protected readonly eventName = "agent.quick_prompt_used";

  constructor(
    params: AgentQuickPromptUsedParams,
    dependencies: AnalyticsReporterDependencies
  ) {
    super(
      {
        ...projectAgentChatEngagementBaseParams(params),
        promptType: params.promptType,
        source: params.source
      },
      dependencies
    );
  }
}
