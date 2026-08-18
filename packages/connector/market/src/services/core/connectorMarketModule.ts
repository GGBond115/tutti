import {
  createDecorator,
  type IInstantiationService
} from "@tutti-os/infra/di";

import type { ConnectorMarketServiceDependencies } from "../connectorMarketService.interface.ts";
import type { ConnectorMarketScope } from "../ui-state/connectorMarketUiStateService.interface.ts";
import type { ConnectorMarketLifecycle } from "./connectorMarketLifecycle.ts";
import type { IConnectorMarketRoot } from "./connectorMarketRoot.interface.ts";
import { ConnectorMarketRuntime } from "./connectorMarketRuntime.ts";
import type { IConnectorMarketService } from "../connectorMarketService.interface.ts";
import type { IConnectorMarketUiStateService } from "../ui-state/connectorMarketUiStateService.interface.ts";
import type { IConnectorMarketViewService } from "../view/connectorMarketViewService.interface.ts";

export interface ConnectorMarketModuleDependencies {
  market: ConnectorMarketServiceDependencies;
  scope: ConnectorMarketScope;
}

export interface IConnectorMarketModule {
  readonly _serviceBrand: undefined;
  readonly lifecycle: ConnectorMarketLifecycle;
  readonly rendererPorts: ConnectorMarketRendererApplicationPorts;

  activate(parentInstantiationService: IInstantiationService): Promise<void>;
  dispose(): void;
}

export interface ConnectorMarketRendererApplicationPorts {
  readonly market: ConnectorMarketRendererMarketPort;
  readonly uiState: ConnectorMarketRendererUiStatePort;
  readonly view: ConnectorMarketRendererViewPort;
  onDispose(listener: () => void): () => void;
}

export type ConnectorMarketRendererMarketPort = Pick<
  IConnectorMarketService,
  | "dataStore"
  | "reload"
  | "refreshCatalog"
  | "loadMore"
  | "install"
  | "uninstall"
  | "dismissUninstallNotification"
  | "beginAuthorization"
  | "cancelAuthorization"
  | "openAuthorizationUrl"
  | "disconnectAuthorization"
>;

export type ConnectorMarketRendererUiStatePort = Pick<
  IConnectorMarketUiStateService,
  | "dataStore"
  | "setQuery"
  | "selectSegment"
  | "openConnector"
  | "requestUninstall"
  | "closeDialog"
>;

export type ConnectorMarketRendererViewPort = Pick<
  IConnectorMarketViewService,
  "dataStore"
>;

export const IConnectorMarketModule = createDecorator<IConnectorMarketModule>(
  "connector-market-module"
);

export class ConnectorMarketModule implements IConnectorMarketModule {
  declare readonly _serviceBrand: undefined;

  private runtime: ConnectorMarketRuntime | null = null;
  private readonly disposeListeners = new Set<() => void>();
  private currentRendererPorts: ConnectorMarketRendererApplicationPorts | null =
    null;
  private activationPromise: Promise<void> | null = null;
  private disposed = false;

  constructor(
    private readonly dependencies: ConnectorMarketModuleDependencies
  ) {}

  get lifecycle(): ConnectorMarketLifecycle {
    return this.requireRuntime().lifecycle;
  }

  private get root(): IConnectorMarketRoot {
    const runtime = this.requireRuntime();
    if (runtime.lifecycle.phase !== "ready") {
      throw new Error("Connector market module is not ready");
    }
    return runtime.root;
  }

  get rendererPorts(): ConnectorMarketRendererApplicationPorts {
    if (this.currentRendererPorts) return this.currentRendererPorts;
    const root = this.root;
    this.currentRendererPorts = {
      market: {
        dataStore: root.market.dataStore,
        reload: () => root.market.reload(),
        refreshCatalog: () => root.market.refreshCatalog(),
        loadMore: (sectionId) => root.market.loadMore(sectionId),
        install: (connectorKey) => root.market.install(connectorKey),
        uninstall: (connectorKey) => root.market.uninstall(connectorKey),
        dismissUninstallNotification: (operationId) =>
          root.market.dismissUninstallNotification(operationId),
        beginAuthorization: (connectorKey, secret) =>
          root.market.beginAuthorization(connectorKey, secret),
        cancelAuthorization: (connectorKey) =>
          root.market.cancelAuthorization(connectorKey),
        openAuthorizationUrl: (url) => root.market.openAuthorizationUrl(url),
        disconnectAuthorization: (connectorKey) =>
          root.market.disconnectAuthorization(connectorKey)
      },
      uiState: {
        dataStore: root.uiState.dataStore,
        setQuery: (query) => root.uiState.setQuery(query),
        selectSegment: (segment) => root.uiState.selectSegment(segment),
        openConnector: (connectorKey) =>
          root.uiState.openConnector(connectorKey),
        requestUninstall: (connectorKey) =>
          root.uiState.requestUninstall(connectorKey),
        closeDialog: () => root.uiState.closeDialog()
      },
      view: { dataStore: root.view.dataStore },
      onDispose: (listener) => {
        this.disposeListeners.add(listener);
        return () => this.disposeListeners.delete(listener);
      }
    };
    return this.currentRendererPorts;
  }

  activate(parentInstantiationService: IInstantiationService): Promise<void> {
    if (this.disposed) {
      return Promise.reject(new Error("Connector market module is disposed"));
    }
    if (!this.activationPromise) {
      this.runtime = new ConnectorMarketRuntime({
        marketDependencies: this.dependencies.market,
        parentInstantiationService,
        scope: this.dependencies.scope
      });
      this.activationPromise = this.runtime.start();
    }
    return this.activationPromise;
  }

  dispose(): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    this.disposeListeners.forEach((listener) => listener());
    this.disposeListeners.clear();
    this.currentRendererPorts = null;
    this.runtime?.dispose();
  }

  private requireRuntime(): ConnectorMarketRuntime {
    if (!this.runtime) {
      throw new Error("Connector market module has not been activated");
    }
    return this.runtime;
  }
}
