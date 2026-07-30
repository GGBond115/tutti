import { useCallback, useMemo } from "react";
import type { AgentGUIConversationRailLayout } from "@tutti-os/agent-gui";
import {
  createAgentGuiWorkbenchRailLayoutStore,
  type AgentGuiWorkbenchRailLayoutStore
} from "@tutti-os/agent-gui/workbench";

export function useStandaloneAgentWindowConversationRailLayout(
  nodeId: string
): {
  onConversationRailLayoutChange: (
    layout: AgentGUIConversationRailLayout
  ) => void;
  railLayoutStore: AgentGuiWorkbenchRailLayoutStore;
} {
  const railLayoutStore = useMemo(
    () => createAgentGuiWorkbenchRailLayoutStore(),
    []
  );
  const onConversationRailLayoutChange = useCallback(
    (layout: AgentGUIConversationRailLayout) => {
      railLayoutStore.report(nodeId, layout);
    },
    [nodeId, railLayoutStore]
  );

  return { onConversationRailLayoutChange, railLayoutStore };
}
