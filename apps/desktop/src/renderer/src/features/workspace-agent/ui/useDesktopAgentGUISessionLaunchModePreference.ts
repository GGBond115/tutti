import { useCallback } from "react";
import type { IDesktopPreferencesService } from "@renderer/features/desktop-preferences/services/desktopPreferencesService.interface.ts";
import type { DesktopAgentSessionLaunchMode } from "@shared/preferences";

interface DesktopAgentGUISessionLaunchModePreferenceInput {
  desktopPreferencesService: Pick<
    IDesktopPreferencesService,
    "rememberAgentSessionLaunchMode"
  >;
  workspaceId: string;
}

export function useDesktopAgentGUISessionLaunchModePreference({
  desktopPreferencesService,
  workspaceId
}: DesktopAgentGUISessionLaunchModePreferenceInput): (input: {
  mode: DesktopAgentSessionLaunchMode;
  projectSectionKey: string;
}) => void {
  return useCallback(
    (input) => {
      void desktopPreferencesService.rememberAgentSessionLaunchMode(
        workspaceId,
        input.projectSectionKey,
        input.mode
      );
    },
    [desktopPreferencesService, workspaceId]
  );
}
