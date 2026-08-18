import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentSlashCommandPalette } from "./AgentSlashCommandPalette";

describe("AgentSlashCommandPalette", () => {
  it("renders a capability section and dispatches capability selection", () => {
    const onSelectCapability = vi.fn();

    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "capability",
            key: "capability:browserUse",
            label: "Browser",
            description: "Let the agent use a browser.",
            capability: {
              kind: "capability",
              capability: "browserUse",
              name: "browser",
              aliases: ["浏览器"]
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={onSelectCapability}
        onSelectSkill={vi.fn()}
      />
    );

    expect(screen.getByText("Capabilities")).toBeInTheDocument();
    screen.getByRole("option", { name: /Browser/i }).click();
    expect(onSelectCapability).toHaveBeenCalledWith({
      kind: "capability",
      capability: "browserUse",
      name: "browser",
      aliases: ["浏览器"]
    });
    expect(
      screen.getByRole("option", { name: /Browser/i }).querySelector("svg")
    ).toBeTruthy();
  });

  it("renders inline settings on capability entries and dispatches settings selection", () => {
    const onSelectCapabilitySettings = vi.fn();

    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "capability",
            key: "capability:computerUse",
            label: "Computer",
            description: "Install or grant access.",
            settingsAriaLabel: "Computer use setup",
            settingsLabel: "Settings",
            capability: {
              kind: "capability",
              capability: "computerUse",
              name: "computer",
              aliases: ["电脑"]
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={vi.fn()}
        onSelectCapabilitySettings={onSelectCapabilitySettings}
        onSelectSkill={vi.fn()}
      />
    );

    expect(screen.getByText("Capabilities")).toBeInTheDocument();
    screen.getByRole("button", { name: "Computer use setup" }).click();
    expect(onSelectCapabilitySettings).toHaveBeenCalledWith({
      kind: "capability",
      capability: "computerUse",
      name: "computer",
      aliases: ["电脑"]
    });
  });

  it("keeps a read-only capability visible without dispatching mutations", () => {
    const onSelectCapability = vi.fn();
    const onSelectCapabilitySettings = vi.fn();

    render(
      <AgentSlashCommandPalette
        label="Slash commands"
        commandsGroupLabel="Commands"
        capabilitiesGroupLabel="Capabilities"
        skillsGroupLabel="Skills"
        pluginsGroupLabel="Plugins"
        mcpGroupLabel="MCP"
        highlightedIndex={0}
        entries={[
          {
            type: "capability",
            key: "capability:computerUse",
            label: "Computer",
            disabled: true,
            settingsAriaLabel: "Computer use setup",
            settingsLabel: "Settings",
            capability: {
              kind: "capability",
              capability: "computerUse",
              name: "computer"
            }
          }
        ]}
        onHighlightChange={vi.fn()}
        onSelect={vi.fn()}
        onSelectCapability={onSelectCapability}
        onSelectCapabilitySettings={onSelectCapabilitySettings}
        onSelectSkill={vi.fn()}
      />
    );

    const capability = screen.getByRole("option", { name: /computer/i });
    expect(capability).toHaveAttribute("aria-disabled", "true");
    capability.click();
    screen.getByRole("button", { name: "Computer use setup" }).click();

    expect(onSelectCapability).not.toHaveBeenCalled();
    expect(onSelectCapabilitySettings).not.toHaveBeenCalled();
  });
});
