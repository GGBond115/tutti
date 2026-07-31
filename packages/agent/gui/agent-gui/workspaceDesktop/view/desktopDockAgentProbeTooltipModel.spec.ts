import { describe, expect, it } from "vitest";
import type { TranslateFn } from "../../../i18n/index";
import { buildDockAgentProbeTooltipLines } from "./desktopDockAgentProbeTooltipModel";

const translate = ((key: string) => key) as TranslateFn;

describe("buildDockAgentProbeTooltipLines", () => {
  it("renders a stable subscription error instead of a generic empty-usage row", () => {
    const lines = buildDockAgentProbeTooltipLines(
      {
        provider: "acp:kimi-code",
        availability: {
          status: "unavailable",
          detailsVisible: false
        },
        lastError: {
          code: "subscription_required"
        }
      },
      false,
      translate,
      { includeUsageLines: true }
    );

    expect(lines).toContainEqual({
      label: "agentHost.workspaceAgentProbeDetailQuota",
      primary: "agentHost.workspaceAgentProbeErrorSubscriptionRequired"
    });
  });
});
