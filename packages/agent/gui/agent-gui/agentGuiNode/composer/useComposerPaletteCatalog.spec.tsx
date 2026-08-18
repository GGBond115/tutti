import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { AgentGUIComposerSettingsVM } from "../model/agentGuiNodeTypes";
import { resolveSlashCommandSubmitEffect } from "../model/agentSlashCommandProviderPolicy";
import type { AgentComposerProps } from "./AgentComposer.types";
import { useComposerPaletteCatalog } from "./useComposerPaletteCatalog";

describe("useComposerPaletteCatalog", () => {
  it("shows host-managed computer use as a capability while daemon readiness is false", () => {
    const { result } = renderHook(() =>
      useComposerPaletteCatalog({
        provider: "codex",
        isGoalModeActive: false,
        goalSupported: false,
        paletteDraftPrompt: "/",
        availableCommands: [],
        availableSkills: [],
        hasCompactableContext: false,
        compactSupported: false,
        composerSettings: {
          supportsPlanMode: false,
          supportsBrowser: false,
          supportsComputerUse: false,
          slashCommandPolicy: {
            fallbackCommands: [],
            commandEffects: [],
            commandCatalogAuthoritative: true
          }
        } as unknown as AgentGUIComposerSettingsVM,
        capabilityMenuState: {
          computerUse: {
            authorization: "authorized",
            installed: true,
            presentationSupported: true
          }
        },
        capabilityControlsReadOnly: false,
        labels: {
          computerUseCapabilityDescription: "Control the computer",
          computerUseCapabilityLabel: "Computer use",
          computerUseCapabilitySettingsLabel: "Computer use settings",
          capabilityInlineSettingsLabel: "Settings"
        } as AgentComposerProps["labels"],
        uiLanguage: "en",
        editorHandleRef: { current: null }
      })
    );

    expect(result.current.availableCapabilities).toEqual([
      {
        capability: "computerUse",
        label: "Computer use",
        name: "computer",
        trigger: "/computer"
      }
    ]);
    expect(
      result.current.slashPaletteEntries.find(
        (entry) => entry.key === "capability:computerUse"
      )
    ).toMatchObject({ selectAction: "settings", type: "capability" });
    expect(
      resolveSlashCommandSubmitEffect({
        provider: "codex",
        policy: result.current.slashCommandPolicy,
        computerSupported: false,
        commands: result.current.resolvedSlashCommands,
        draft: "/computer click Confirm"
      })
    ).toBeNull();
  });
});
