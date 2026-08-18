import { useCallback, useMemo, useSyncExternalStore } from "react";

import type { ConnectorMarketI18nRuntime } from "../i18n/connectorMarketI18n.ts";
import { ConnectorComposerMenu } from "./components/composer/ConnectorComposerMenu.tsx";
import type { ConnectorRendererEventSink } from "./connectorRendererEvents.ts";
import {
  normalizeConnectorRendererStatus,
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

export function projectConnectorComposerItems(input: {
  readonly items: readonly ConnectorRendererItem[];
  readonly policy: ConnectorRendererAgentPolicySnapshot;
  readonly selectedConnectorKeys: readonly string[];
}): readonly ConnectorComposerItem[] {
  const selectedKeys = new Set(input.selectedConnectorKeys);
  const supportedKeys =
    input.policy.supportedConnectorKeys === null
      ? null
      : new Set(input.policy.supportedConnectorKeys);
  return input.items.map((item) => ({
    connectorKey: item.connectorKey,
    iconUrl: item.iconUrl,
    name: item.name,
    selected: selectedKeys.has(item.connectorKey),
    status:
      input.policy.status === "loading"
        ? "loading"
        : input.policy.status !== "ready"
          ? "unsupported"
          : supportedKeys && !supportedKeys.has(item.connectorKey)
            ? "disabled"
            : normalizeConnectorRendererStatus(item.status)
  }));
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
  const supportedKeys =
    policy.supportedConnectorKeys === null
      ? null
      : new Set(policy.supportedConnectorKeys);

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
      onOpenConnector={(connectorKey, status) => {
        onEvent({
          type:
            status === "authorization_required"
              ? "authorization.requested"
              : "connector.details.requested",
          connectorKey
        });
      }}
      onOpenMarket={() => onEvent({ type: "catalog.requested" })}
      onSelectConnector={(connectorKey, selected) => {
        if (
          !selectedKeys.has(connectorKey) &&
          !snapshot.items.some(
            (item) =>
              item.connectorKey === connectorKey &&
              item.status === "connected" &&
              policy.status === "ready" &&
              (!supportedKeys || supportedKeys.has(connectorKey))
          )
        ) {
          return;
        }
        agent.draft.setSelected(connectorKey, selected);
      }}
    />
  );
}
