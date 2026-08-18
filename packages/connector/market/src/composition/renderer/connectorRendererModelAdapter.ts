import { snapshot, subscribe } from "valtio/vanilla";

import {
  projectConnectorRendererSnapshot,
  registerConnectorRendererSecureSubmissionPort,
  type ConnectorRendererAgentPolicyPort,
  type ConnectorRendererCommands,
  type ConnectorRendererModel,
  type ConnectorRendererSurfaceSnapshot,
  type DisposableConnectorRendererModel
} from "../../renderer/connectorRendererModel.ts";
import type { ConnectorMarketRendererApplicationPorts } from "../../services/core/connectorMarketModule.ts";

const localPolicy = Object.freeze({
  status: "ready" as const,
  supportedConnectorKeys: null
});
const unavailableSharedPolicy = Object.freeze({
  status: "unavailable" as const,
  supportedConnectorKeys: Object.freeze([])
});

export function createConnectorRendererModel(
  root: ConnectorMarketRendererApplicationPorts,
  agentPolicy?: ConnectorRendererAgentPolicyPort
): DisposableConnectorRendererModel {
  let disposed = false;
  let current = projectConnectorRendererSnapshot(root.market.dataStore);
  let surface = projectSurface(root);
  const listeners = new Set<() => void>();
  const publish = (): void => {
    if (disposed) return;
    current = projectConnectorRendererSnapshot(root.market.dataStore);
    surface = projectSurface(root);
    listeners.forEach((listener) => listener());
  };
  const unsubscribers = [
    subscribe(root.market.dataStore, publish),
    subscribe(root.uiState.dataStore, publish),
    subscribe(root.view.dataStore, publish)
  ];
  const commands = Object.freeze<ConnectorRendererCommands>({
    refresh: () => root.market.reload(),
    refreshCatalog: () => root.market.refreshCatalog(),
    loadMore: (sectionId) => root.market.loadMore(sectionId),
    install: (key) => root.market.install(key),
    uninstall: (key) => root.market.uninstall(key),
    disconnectAuthorization: (key) => root.market.disconnectAuthorization(key),
    cancelAuthorization: (key) => root.market.cancelAuthorization(key),
    openAuthorizationUrl: (url) => root.market.openAuthorizationUrl(url),
    dismissUninstallNotification: (id) =>
      root.market.dismissUninstallNotification(id),
    setQuery: (query) => root.uiState.setQuery(query),
    selectSegment: (segment) => root.uiState.selectSegment(segment),
    openConnector: (key) => root.uiState.openConnector(key),
    requestUninstall: (key) => root.uiState.requestUninstall(key),
    closeDialog: () => root.uiState.closeDialog()
  });
  const subscribeStore = (listener: () => void): (() => void) => {
    if (disposed) return () => undefined;
    listeners.add(listener);
    return () => listeners.delete(listener);
  };
  const model: DisposableConnectorRendererModel = {
    commands,
    getAgentPolicy: (target) =>
      agentPolicy?.getSnapshot(target) ??
      (target.ownership === "local" ? localPolicy : unavailableSharedPolicy),
    subscribeAgentPolicy: (target, listener) =>
      agentPolicy?.subscribe?.(target, listener) ?? (() => undefined),
    getSnapshot: () => current,
    subscribe: subscribeStore,
    getSurfaceSnapshot: () => surface,
    subscribeSurface: subscribeStore,
    dispose() {
      if (disposed) return;
      disposed = true;
      unsubscribers.forEach((unsubscribe) => unsubscribe());
      listeners.clear();
    }
  };
  registerConnectorRendererSecureSubmissionPort(model, {
    beginAuthorization: (key, secret) =>
      root.market.beginAuthorization(key, secret)
  });
  return model;
}

function projectSurface(
  root: ConnectorMarketRendererApplicationPorts
): ConnectorRendererSurfaceSnapshot {
  return Object.freeze({
    market: snapshot(root.market.dataStore),
    ui: snapshot(root.uiState.dataStore),
    view: snapshot(root.view.dataStore)
  }) as unknown as ConnectorRendererSurfaceSnapshot;
}

const modelsByPorts = new WeakMap<
  ConnectorMarketRendererApplicationPorts,
  DisposableConnectorRendererModel
>();

export function getConnectorRendererModel(
  ports: ConnectorMarketRendererApplicationPorts
): ConnectorRendererModel {
  let model = modelsByPorts.get(ports);
  if (!model) {
    model = createConnectorRendererModel(ports);
    modelsByPorts.set(ports, model);
    ports.onDispose(() => {
      model?.dispose();
      modelsByPorts.delete(ports);
    });
  }
  return model;
}
