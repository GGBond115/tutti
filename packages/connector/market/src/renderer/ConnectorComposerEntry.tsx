import { useCallback, useMemo, useSyncExternalStore } from "react";

import type { ConnectorMarketI18nRuntime } from "../i18n/connectorMarketI18n.ts";
import { ConnectorComposerMenu } from "./components/composer/ConnectorComposerMenu.tsx";
import type { ConnectorRendererEventSink } from "./connectorRendererEvents.ts";
import {
  normalizeConnectorPresentation,
  type ConnectorRendererAgentPolicySnapshot,
  type ConnectorRendererAgentTarget,
  type ConnectorRendererItem,
  type ConnectorRendererModel
} from "./connectorRendererModel.ts";
import type { ConnectorComposerItem } from "./components/composer/ConnectorComposerMenu.tsx";

export interface ConnectorComposerAgentContext {
  readonly target: {
    readonly agentTargetId: string;
    readonly ownership: "local" | "shared";
  };
  readonly draft: {
    readonly selectedConnectorKeys: readonly string[];
    setSelected(connectorKey: string, selected: boolean): void;
  };
}

export interface ConnectorComposerEntryProps {
  agent: ConnectorComposerAgentContext;
  disabled?: boolean;
  i18n: ConnectorMarketI18nRuntime;
  model: ConnectorRendererModel;
  onEvent: ConnectorRendererEventSink;
}

export function connectorComposerSelectionAllowed(
  item: Pick<ConnectorComposerItem, "allowedActions"> | undefined,
  selected: boolean
): boolean {
  return Boolean(
    item?.allowedActions.includes(selected ? "remove_selection" : "select")
  );
}

export function projectConnectorComposerItems(input: {
  readonly items: readonly ConnectorRendererItem[];
  readonly policy: ConnectorRendererAgentPolicySnapshot;
  readonly selectedConnectorKeys: readonly string[];
}): readonly ConnectorComposerItem[] {
  const selectedKeys = new Set(input.selectedConnectorKeys);
  return input.items.map((item) => {
    const presentation =
      input.policy.status === "loading"
        ? {
            state: "loading" as const,
            reasonCode: "agent_connector_policy_loading",
            allowedActions: ["details" as const, "remove_selection" as const]
          }
        : input.policy.status === "ready"
          ? normalizeConnectorPresentation(
              input.policy.presentationsByConnectorKey[item.connectorKey] ?? {
                state: "unsupported",
                reasonCode: "agent_connector_policy_missing",
                allowedActions: ["details", "remove_selection"]
              }
            )
          : {
              state: "unsupported" as const,
              reasonCode: "agent_connector_policy_unavailable",
              allowedActions: ["details" as const, "remove_selection" as const]
            };
    return {
      allowedActions: presentation.allowedActions,
      connectorKey: item.connectorKey,
      iconUrl: item.iconUrl,
      name: item.name,
      selected: selectedKeys.has(item.connectorKey),
      status: presentation.state
    };
  });
}

export function useConnectorRendererAgentPolicy(
  model: ConnectorRendererModel,
  target: ConnectorRendererAgentTarget
): ConnectorRendererAgentPolicySnapshot {
  const stableTarget = useMemo<ConnectorRendererAgentTarget>(
    () => ({
      agentTargetId: target.agentTargetId,
      ownership: target.ownership
    }),
    [target.agentTargetId, target.ownership]
  );
  const subscribePolicy = useCallback(
    (listener: () => void) =>
      model.subscribeAgentPolicy(stableTarget, listener),
    [model, stableTarget]
  );
  const getPolicy = useCallback(
    () => model.getAgentPolicy(stableTarget),
    [model, stableTarget]
  );
  return useSyncExternalStore(subscribePolicy, getPolicy, getPolicy);
}

/** Connector-owned primary capability entry for Agent composers. */
export function ConnectorComposerEntry({
  agent,
  disabled = false,
  i18n,
  model,
  onEvent
}: ConnectorComposerEntryProps): React.JSX.Element | null {
  const snapshot = useSyncExternalStore(
    model.subscribe,
    model.getSnapshot,
    model.getSnapshot
  );
  const selectedKeys = new Set(agent.draft.selectedConnectorKeys);
  const policy = useConnectorRendererAgentPolicy(model, agent.target);

  if (!snapshot.entryAvailable) {
    return null;
  }

  return (
    <ConnectorComposerMenu
      disabled={disabled}
      items={projectConnectorComposerItems({
        items: snapshot.items,
        policy,
        selectedConnectorKeys: agent.draft.selectedConnectorKeys
      })}
      labels={{
        authorizationRequired: i18n.t("actionAuthorize"),
        connected: i18n.t("connectedStatus"),
        connectors: i18n.t("composerConnectors"),
        degraded: i18n.t("statusDegraded"),
        disabled: i18n.t("statusDisabled"),
        empty: i18n.t("composerEmpty"),
        failed: i18n.t("operationFailed"),
        loading: i18n.t("loading"),
        more: i18n.t("composerMore"),
        selected: i18n.t("composerSelected"),
        setupRequired: i18n.t("actionInstall"),
        unavailable: i18n.t("statusUnavailable"),
        unsupported: i18n.t("statusUnsupported"),
        connecting: i18n.t("statusConnecting")
      }}
      loading={snapshot.phase === "loading"}
      onOpenChange={(open) => {
        if (open) {
          void model.commands.refresh().catch(() => undefined);
        }
      }}
      onOpenConnector={(connectorKey, _status, allowedActions) => {
        onEvent({
          type: allowedActions.includes("authorize")
            ? "authorization.requested"
            : "connector.details.requested",
          connectorKey
        });
      }}
      onOpenMarket={() => onEvent({ type: "catalog.requested" })}
      onSelectConnector={(connectorKey, selected) => {
        const item = projectConnectorComposerItems({
          items: snapshot.items,
          policy,
          selectedConnectorKeys: agent.draft.selectedConnectorKeys
        }).find((candidate) => candidate.connectorKey === connectorKey);
        if (
          !connectorComposerSelectionAllowed(
            item,
            selectedKeys.has(connectorKey)
          )
        ) {
          return;
        }
        agent.draft.setSelected(connectorKey, selected);
      }}
    />
  );
}
