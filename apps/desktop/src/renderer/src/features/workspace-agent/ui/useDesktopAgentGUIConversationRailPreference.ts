import { useCallback, type RefObject } from "react";
import type { DesktopRuntimeApi } from "@preload/types";
import type { IDesktopPreferencesService } from "@renderer/features/desktop-preferences/services/desktopPreferencesService.interface.ts";
import { isDesktopAgentProvider } from "../../../../../shared/preferences/index.ts";
import type { DesktopAgentGUINodeState } from "../desktopAgentGUINodeState.ts";
import { logAgentGUIConversationRailPreferenceDiagnostic } from "./desktopAgentGUIWorkbenchDiagnostics.ts";

interface DesktopAgentGUIConversationRailPreferenceInput {
  desktopPreferencesService: Pick<
    IDesktopPreferencesService,
    "rememberAgentGuiConversationRailCollapsed"
  >;
  runtimeApi?: Pick<DesktopRuntimeApi, "logTerminalDiagnostic">;
  workspaceId: string;
}

type DesktopAgentGUIConversationRailStateOwner =
  | "surface"
  | "workbench-node-source";
type DesktopAgentGUINodeUpdater = (
  updater: (current: DesktopAgentGUINodeState) => DesktopAgentGUINodeState
) => void;

export function rememberDesktopAgentGUIConversationRailPreference(
  input: DesktopAgentGUIConversationRailPreferenceInput & {
    conversationRailCollapsed: boolean;
    provider: DesktopAgentGUINodeState["provider"];
  }
): Promise<void> | null {
  const {
    conversationRailCollapsed,
    desktopPreferencesService,
    provider,
    runtimeApi,
    workspaceId
  } = input;
  if (!isDesktopAgentProvider(provider)) {
    return null;
  }
  return desktopPreferencesService
    .rememberAgentGuiConversationRailCollapsed(
      provider,
      conversationRailCollapsed
    )
    .then(() => {
      logAgentGUIConversationRailPreferenceDiagnostic({
        collapsed: conversationRailCollapsed,
        provider,
        runtimeApi,
        workspaceId
      });
    })
    .catch((error) => {
      logAgentGUIConversationRailPreferenceDiagnostic({
        collapsed: conversationRailCollapsed,
        error,
        provider,
        runtimeApi,
        workspaceId
      });
    });
}

export function useDesktopAgentGUIConversationRailPreference(
  input: DesktopAgentGUIConversationRailPreferenceInput
): (
  provider: DesktopAgentGUINodeState["provider"],
  conversationRailCollapsed: boolean
) => void {
  const { desktopPreferencesService, runtimeApi, workspaceId } = input;
  return useCallback(
    (provider, conversationRailCollapsed) => {
      void rememberDesktopAgentGUIConversationRailPreference({
        conversationRailCollapsed,
        desktopPreferencesService,
        provider,
        runtimeApi,
        workspaceId
      });
    },
    [desktopPreferencesService, runtimeApi, workspaceId]
  );
}

export function handleDesktopAgentGUIConversationRailToggle(input: {
  conversationRailCollapsed: boolean;
  currentNodeState: DesktopAgentGUINodeState;
  rememberConversationRailPreference: (
    provider: DesktopAgentGUINodeState["provider"],
    conversationRailCollapsed: boolean
  ) => void;
  stateOwner: DesktopAgentGUIConversationRailStateOwner;
  updateNode: DesktopAgentGUINodeUpdater;
}): void {
  if (input.stateOwner === "workbench-node-source") {
    if (
      (input.currentNodeState.conversationRailCollapsed === true) !==
      input.conversationRailCollapsed
    ) {
      input.rememberConversationRailPreference(
        input.currentNodeState.provider,
        input.conversationRailCollapsed
      );
    }
    return;
  }
  input.updateNode((current) => ({
    ...current,
    conversationRailCollapsed: input.conversationRailCollapsed
  }));
}

export function useDesktopAgentGUIConversationRailToggle(input: {
  nodeStateRef: RefObject<DesktopAgentGUINodeState>;
  rememberConversationRailPreference: (
    provider: DesktopAgentGUINodeState["provider"],
    conversationRailCollapsed: boolean
  ) => void;
  stateOwner: DesktopAgentGUIConversationRailStateOwner;
  updateNode: DesktopAgentGUINodeUpdater;
}): (conversationRailCollapsed: boolean) => void {
  const {
    nodeStateRef,
    rememberConversationRailPreference,
    stateOwner,
    updateNode
  } = input;
  return useCallback(
    (conversationRailCollapsed) => {
      handleDesktopAgentGUIConversationRailToggle({
        conversationRailCollapsed,
        currentNodeState: nodeStateRef.current,
        rememberConversationRailPreference,
        stateOwner,
        updateNode
      });
    },
    [nodeStateRef, rememberConversationRailPreference, stateOwner, updateNode]
  );
}
