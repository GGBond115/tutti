import {
  BaseAnalyticsReporter,
  type AnalyticsReporterDependencies,
  type AnalyticsReporterParams
} from "../baseReporter.ts";
import { projectAgentChatEngagementBaseParams } from "../agent-chat-engagement-params.ts";
import type { AgentQuickPromptPanelOpenedParams } from "./types.ts";

export class AgentQuickPromptPanelOpenedReporter extends BaseAnalyticsReporter<AnalyticsReporterParams> {
  protected readonly eventName = "agent.quick_prompt_panel_opened";

  constructor(
    params: AgentQuickPromptPanelOpenedParams,
    dependencies: AnalyticsReporterDependencies
  ) {
    super(
      {
        ...projectAgentChatEngagementBaseParams(params),
        source: params.source
      },
      dependencies
    );
  }
}
