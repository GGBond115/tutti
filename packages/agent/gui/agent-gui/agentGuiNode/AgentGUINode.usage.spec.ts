import { describe, expect, it } from "vitest";
import {
  resolveAgentGUIRailConfigProvider,
  slashStatusUsageErrorMessage
} from "./AgentGUINode.usage";

describe("resolveAgentGUIRailConfigProvider", () => {
  it("preserves an explicit unscoped provider for the all-agents view", () => {
    expect(resolveAgentGUIRailConfigProvider(null, "codex")).toBeNull();
  });

  it("falls back to the shell provider only when the prop is absent", () => {
    expect(resolveAgentGUIRailConfigProvider(undefined, "codex")).toBe("codex");
    expect(resolveAgentGUIRailConfigProvider("claude-code", "codex")).toBe(
      "claude-code"
    );
  });

  it("preserves parse failures as account-status parsing errors", () => {
    expect(slashStatusUsageErrorMessage("parse_failed", (key) => key)).toBe(
      "agentHost.agentGui.slashStatusUsageParseFailed"
    );
  });
});
