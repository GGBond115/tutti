import type { AgentGUIPrimaryCapabilitySlotContext } from "@tutti-os/agent-gui";
import {
  ConnectorComposerEntry,
  type ConnectorRendererEventSink,
  type ConnectorRendererModel
} from "@tutti-os/connector-market/renderer";
import type { ConnectorMarketI18nRuntime } from "@tutti-os/connector-market/i18n";
import {
  createDesktopConnectorSelection,
  decodeDesktopConnectorSelections
} from "./desktopConnectorPrimaryCapabilityAdapter.ts";

export interface DesktopConnectorPrimaryCapabilityBridgeProps {
  context: AgentGUIPrimaryCapabilitySlotContext;
  i18n: ConnectorMarketI18nRuntime;
  model: ConnectorRendererModel;
  onEvent: ConnectorRendererEventSink;
}

/** The only Desktop adapter from AgentGUI's neutral slot to Connector UI. */
export function DesktopConnectorPrimaryCapabilityBridge({
  context,
  i18n,
  model,
  onEvent
}: DesktopConnectorPrimaryCapabilityBridgeProps): React.JSX.Element {
  const selectedConnectors = decodeDesktopConnectorSelections(
    context.draft.selectedCapabilities
  );
  return (
    <ConnectorComposerEntry
      agent={{
        target: {
          agentTargetId: context.target.agentTargetId,
          ownership: context.target.ownership === "shared" ? "shared" : "local"
        },
        draft: {
          selectedConnectorKeys: selectedConnectors.map(({ key }) => key),
          setSelected: (connectorKey, selected) => {
            const restored = selectedConnectors.find(
              (candidate) => candidate.key === connectorKey
            )?.selection;
            context.draft.setSelected(
              restored ?? createDesktopConnectorSelection(connectorKey),
              selected
            );
          }
        }
      }}
      disabled={context.disabled}
      i18n={i18n}
      model={model}
      onEvent={onEvent}
    />
  );
}
