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

const unavailableSharedPolicy = Object.freeze({
  status: "unavailable" as const,
  presentationsByConnectorKey: Object.freeze({})
});
const agentPolicyBinders = new WeakMap<
  ConnectorRendererModel,
  (policy: ConnectorRendererAgentPolicyPort) => void
>();

export function createConnectorRendererModel(
  root: ConnectorMarketRendererApplicationPorts,
  agentPolicy?: ConnectorRendererAgentPolicyPort
): DisposableConnectorRendererModel {
  let disposed = false;
  let boundAgentPolicy = agentPolicy;
  let sharedPolicySubscriptionStarted = false;
  let current = projectConnectorRendererSnapshot(root.market.dataStore);
  let localPolicy = localAgentPolicy(current);
  let surface = projectSurface(root);
  const listeners = new Set<() => void>();
  const agentPolicyUnsubscribers = new Set<() => void>();
  const publish = (): void => {
    if (disposed) return;
    current = projectConnectorRendererSnapshot(root.market.dataStore);
    localPolicy = localAgentPolicy(current);
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
      target.ownership === "local"
        ? localPolicy
        : (boundAgentPolicy?.getSnapshot(target) ?? unavailableSharedPolicy),
    subscribeAgentPolicy: (target, listener) => {
      if (target.ownership === "local") return subscribeStore(listener);
      sharedPolicySubscriptionStarted = true;
      if (disposed || !boundAgentPolicy?.subscribe) return () => undefined;
      const unsubscribePolicy = boundAgentPolicy.subscribe(target, listener);
      let active = true;
      const unsubscribe = (): void => {
        if (!active) return;
        active = false;
        agentPolicyUnsubscribers.delete(unsubscribe);
        unsubscribePolicy();
      };
      agentPolicyUnsubscribers.add(unsubscribe);
      return unsubscribe;
    },
    getSnapshot: () => current,
    subscribe: subscribeStore,
    getSurfaceSnapshot: () => surface,
    subscribeSurface: subscribeStore,
    dispose() {
      if (disposed) return;
      disposed = true;
      unsubscribers.forEach((unsubscribe) => unsubscribe());
      agentPolicyUnsubscribers.forEach((unsubscribe) => unsubscribe());
      agentPolicyUnsubscribers.clear();
      listeners.clear();
      agentPolicyBinders.delete(model);
    }
  };
  agentPolicyBinders.set(model, (policy) => {
    if (boundAgentPolicy === policy) return;
    if (boundAgentPolicy) {
      throw new Error(
        "Connector renderer model already has a different Agent policy port"
      );
    }
    if (sharedPolicySubscriptionStarted) {
      throw new Error(
        "Connector Agent policy must be bound before shared policy subscription"
      );
    }
    boundAgentPolicy = policy;
  });
  registerConnectorRendererSecureSubmissionPort(model, {
    beginAuthorization: (key, secret) =>
      root.market.beginAuthorization(key, secret)
  });
  return model;
}

function localAgentPolicy(
  snapshot: ReturnType<ConnectorRendererModel["getSnapshot"]>
) {
  return Object.freeze({
    status:
      snapshot.phase === "loading"
        ? ("loading" as const)
        : snapshot.entryAvailable
          ? ("ready" as const)
          : ("unavailable" as const),
    presentationsByConnectorKey: Object.freeze(
      Object.fromEntries(
        snapshot.items.map((item) => [item.connectorKey, item.presentation])
      )
    )
  });
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

export interface ConnectorRendererModelOptions {
  agentPolicy?: ConnectorRendererAgentPolicyPort;
}

const modelsByPorts = new WeakMap<
  ConnectorMarketRendererApplicationPorts,
  DisposableConnectorRendererModel
>();

export function getConnectorRendererModel(
  ports: ConnectorMarketRendererApplicationPorts,
  options: ConnectorRendererModelOptions = {}
): ConnectorRendererModel {
  let model = modelsByPorts.get(ports);
  if (!model) {
    model = createConnectorRendererModel(ports, options.agentPolicy);
    modelsByPorts.set(ports, model);
    const ownedModel = model;
    ports.onDispose(() => {
      ownedModel.dispose();
      modelsByPorts.delete(ports);
    });
  } else if (options.agentPolicy) {
    agentPolicyBinders.get(model)?.(options.agentPolicy);
  }
  return model;
}
