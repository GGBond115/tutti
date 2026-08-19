import { openWorkspaceSettingsPanel } from "../../../shared/workspaceSettingsPanel/workspaceSettingsPanelStore";
import type { AgentComposerProps } from "./AgentComposer.types";
import { ComposerConnectorsMenu } from "./ComposerConnectorsMenu";
import { ComposerTuttiModeChip } from "./ComposerTuttiModeChip";

interface Props {
  availableSkills: AgentComposerProps["availableSkills"];
  connectorsVisible: boolean;
  connectorsReadOnly?: boolean;
  showConnectorViewMore?: boolean;
  disabled: boolean;
  isTuttiModeActive: boolean;
  isTuttiModeUpdating: boolean;
  labels: AgentComposerProps["labels"];
  loading: boolean;
  onRetryComposerOptions?: AgentComposerProps["onRetryComposerOptions"];
  onCapabilitySettingsRequest: AgentComposerProps["onCapabilitySettingsRequest"];
  onConnectorSelected: (connectorKey: string, selected: boolean) => void;
  onTuttiModeChange?: (active: boolean) => void;
  selectedConnectorKeys: readonly string[];
  tuttiModeSupported: boolean;
}

/**
 * Owns the host-gated capability controls between the mention and handoff
 * controls. Tutti Mode and Connectors are independent capabilities, so either
 * may render alone and both remain visible when both host gates are enabled.
 */
export function ComposerPrimaryCapabilityControl({
  availableSkills,
  connectorsVisible,
  connectorsReadOnly = false,
  disabled,
  isTuttiModeActive,
  isTuttiModeUpdating,
  labels,
  loading,
  onRetryComposerOptions,
  onCapabilitySettingsRequest,
  onConnectorSelected,
  onTuttiModeChange,
  selectedConnectorKeys,
  showConnectorViewMore = true,
  tuttiModeSupported
}: Props): React.JSX.Element | null {
  if (!connectorsVisible) {
    return (
      <ComposerTuttiModeChip
        active={isTuttiModeActive}
        updating={isTuttiModeUpdating}
        label={labels.tuttiModeLabel}
        description={labels.tuttiModeDescription}
        tuttiModeSupported={tuttiModeSupported}
        onTuttiModeChange={onTuttiModeChange}
      />
    );
  }

  return (
    <>
      <ComposerTuttiModeChip
        active={isTuttiModeActive}
        updating={isTuttiModeUpdating}
        label={labels.tuttiModeLabel}
        description={labels.tuttiModeDescription}
        tuttiModeSupported={tuttiModeSupported}
        onTuttiModeChange={onTuttiModeChange}
      />
      <ComposerConnectorsMenu
        connectors={availableSkills ?? []}
        disabled={disabled}
        labels={{
          connectors: labels.addContentConnectors,
          connectorConnected: labels.addContentConnectorConnected,
          connectorConnect: labels.addContentConnectorConnect,
          connectorAuthorize: labels.addContentConnectorAuthorize,
          connectorEmpty: labels.addContentConnectorEmpty,
          connectorLoading: labels.addContentConnectorLoading,
          connectorMore: labels.addContentConnectorMore,
          connectorSelected: labels.addContentConnectorSelected
        }}
        loading={loading}
        onOpenChange={(open) => {
          if (open) {
            onRetryComposerOptions?.({ section: "connectors" });
          }
        }}
        onOpenConnector={
          connectorsReadOnly
            ? undefined
            : (connectorKey) =>
                onCapabilitySettingsRequest?.({
                  kind: "connector",
                  connectorKey,
                  action: "open"
                })
        }
        onOpenConnectors={
          !showConnectorViewMore
            ? undefined
            : () =>
                openWorkspaceSettingsPanel({
                  section: "agent",
                  pane: "connectors"
                })
        }
        onSelectConnector={connectorsReadOnly ? undefined : onConnectorSelected}
        readOnly={connectorsReadOnly}
        selectedConnectorKeys={connectorsReadOnly ? [] : selectedConnectorKeys}
      />
    </>
  );
}
