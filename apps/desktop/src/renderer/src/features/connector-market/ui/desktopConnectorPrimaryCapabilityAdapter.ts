import type { AgentGUIPrimaryCapabilitySlotContext } from "@tutti-os/agent-gui";

export function decodeDesktopConnectorSelections(
  selections: AgentGUIPrimaryCapabilitySlotContext["draft"]["selectedCapabilities"]
) {
  return selections.flatMap((selection) =>
    selection.payload.type === "connector" &&
    typeof selection.payload.connectorKey === "string"
      ? [{ key: selection.payload.connectorKey, selection }]
      : []
  );
}

export function createDesktopConnectorSelection(connectorKey: string) {
  return {
    id: connectorKey,
    payload: { type: "connector", connectorKey }
  } as const;
}
