import type {
  Connector,
  ConnectorCatalogFreshness,
  ConnectorPresentation,
  ConnectorPresentationAction,
  ConnectorPresentationState
} from "../contracts/index.ts";
import type {
  ConnectorRendererInstallOutcome,
  ConnectorRendererSegment,
  ConnectorRendererSurfaceSnapshot
} from "./connectorRendererSurface.ts";

export type { ConnectorRendererSurfaceSnapshot } from "./connectorRendererSurface.ts";
export type ConnectorRendererStatus = ConnectorPresentationState;

export interface ConnectorRendererItem {
  readonly connectorKey: string;
  readonly iconUrl?: string;
  readonly name: string;
  readonly presentation: Readonly<ConnectorPresentation>;
  readonly revision: number;
}

export interface ConnectorRendererSnapshot {
  /** Application-owned entry admission; hosts must not infer daemon health. */
  readonly entryAvailable: boolean;
  readonly phase: "loading" | "ready" | "failed";
  readonly catalogFreshness: Readonly<ConnectorCatalogFreshness>;
  readonly items: readonly ConnectorRendererItem[];
  readonly revision: number;
  readonly stale: boolean;
}

export interface ConnectorRendererCommands {
  refresh(): Promise<void>;
  refreshCatalog(): Promise<void>;
  loadMore(sectionId: string): Promise<void>;
  install(connectorKey: string): Promise<ConnectorRendererInstallOutcome>;
  uninstall(connectorKey: string): Promise<unknown>;
  restartRuntime(connectorKey: string): Promise<unknown>;
  disconnectAuthorization(connectorKey: string): Promise<void>;
  cancelAuthorization(connectorKey: string): Promise<void>;
  openAuthorizationUrl(url: string): Promise<void>;
  dismissUninstallNotification(operationId: string): void;
  setQuery(query: string): void;
  selectSegment(segment: ConnectorRendererSegment): void;
  openConnector(connectorKey: string): void;
  requestUninstall(connectorKey: string): void;
  closeDialog(): void;
}

export interface ConnectorRendererAgentTarget {
  readonly agentTargetId: string;
  readonly ownership: "local" | "shared";
}

/** Per-Connector application projection for one Agent target. */
export interface ConnectorRendererAgentPolicySnapshot {
  readonly status: "loading" | "ready" | "unavailable";
  readonly presentationsByConnectorKey: Readonly<
    Record<string, Readonly<ConnectorPresentation>>
  >;
}

export interface ConnectorRendererAgentPolicyPort {
  getSnapshot(
    target: ConnectorRendererAgentTarget
  ): ConnectorRendererAgentPolicySnapshot;
  subscribe?(
    target: ConnectorRendererAgentTarget,
    listener: () => void
  ): () => void;
}

export interface ConnectorRendererModel {
  readonly commands: ConnectorRendererCommands;
  getAgentPolicy(
    target: ConnectorRendererAgentTarget
  ): ConnectorRendererAgentPolicySnapshot;
  subscribeAgentPolicy(
    target: ConnectorRendererAgentTarget,
    listener: () => void
  ): () => void;
  getSnapshot(): ConnectorRendererSnapshot;
  subscribe(listener: () => void): () => void;
  getSurfaceSnapshot(): ConnectorRendererSurfaceSnapshot;
  subscribeSurface(listener: () => void): () => void;
}

export interface DisposableConnectorRendererModel extends ConnectorRendererModel {
  dispose(): void;
}

interface ConnectorRendererSecureSubmissionPort {
  beginAuthorization(connectorKey: string, secret?: string): Promise<void>;
}

const secureSubmissionPorts = new WeakMap<
  ConnectorRendererModel,
  ConnectorRendererSecureSubmissionPort
>();

export function requireConnectorRendererSecureSubmissionPort(
  model: ConnectorRendererModel
): ConnectorRendererSecureSubmissionPort {
  return (
    secureSubmissionPorts.get(model) ?? {
      beginAuthorization: () =>
        Promise.reject(new Error("Connector renderer model is not available"))
    }
  );
}

export function registerConnectorRendererSecureSubmissionPort(
  model: ConnectorRendererModel,
  port: ConnectorRendererSecureSubmissionPort
): void {
  secureSubmissionPorts.set(model, port);
}

const presentationStates = new Set<ConnectorPresentationState>([
  "unavailable",
  "loading",
  "setup_required",
  "authorization_required",
  "connecting",
  "connected",
  "degraded",
  "disabled",
  "unsupported",
  "failed"
]);
const presentationActions = new Set<ConnectorPresentationAction>([
  "details",
  "install",
  "update",
  "authorize",
  "cancel",
  "select",
  "remove_selection",
  "disconnect",
  "uninstall",
  "restart_runtime"
]);

const unsupportedPresentation = Object.freeze<ConnectorPresentation>({
  state: "unsupported",
  reasonCode: "unsupported_connector_presentation",
  allowedActions: Object.freeze([
    "details",
    "remove_selection"
  ]) as unknown as ConnectorPresentationAction[]
});

/** Defensive validation only; lifecycle and action derivation belong upstream. */
export function normalizeConnectorPresentation(
  value: Readonly<ConnectorPresentation>
): Readonly<ConnectorPresentation> {
  const state = value.state as string;
  const actions = value.allowedActions as readonly string[];
  if (
    !presentationStates.has(state as ConnectorPresentationState) ||
    !Array.isArray(actions) ||
    actions.some(
      (action) =>
        !presentationActions.has(action as ConnectorPresentationAction)
    ) ||
    new Set(actions).size !== actions.length ||
    (state === "connected") !== actions.includes("select") ||
    (state !== "connected" && !value.reasonCode?.trim())
  ) {
    return unsupportedPresentation;
  }
  return Object.freeze({
    state: state as ConnectorPresentationState,
    ...(value.reasonCode ? { reasonCode: value.reasonCode } : {}),
    allowedActions: Object.freeze([...actions]) as ConnectorPresentationAction[]
  });
}

export function projectConnectorRendererSnapshot(
  market: ConnectorRendererProjectionSource
): ConnectorRendererSnapshot {
  const items = market.connectorKeys.flatMap((connectorKey) => {
    const connector = market.connectorsByKey[connectorKey];
    if (!connector) return [];
    return [
      Object.freeze({
        connectorKey,
        iconUrl: connector.release.manifest.iconUrl,
        name: connector.release.manifest.displayName,
        presentation: normalizeConnectorPresentation(connector.presentation),
        revision: connector.revision
      })
    ];
  });
  const stale =
    market.loadState !== "ready" ||
    market.catalogFreshness.state === "stale" ||
    market.catalogFreshness.state === "unavailable" ||
    Boolean(market.catalogFreshness.staleSince);
  return Object.freeze({
    entryAvailable:
      market.loadState === "ready" ||
      (market.loadState === "error" && items.length > 0),
    phase:
      market.loadState === "loading" || market.loadState === "idle"
        ? "loading"
        : market.loadState === "error"
          ? "failed"
          : "ready",
    catalogFreshness: Object.freeze({ ...market.catalogFreshness }),
    items: Object.freeze(items),
    revision: market.revision,
    stale
  });
}

export interface ConnectorRendererProjectionSource {
  readonly loadState: string;
  readonly catalogFreshness: Readonly<ConnectorCatalogFreshness>;
  readonly connectorKeys: readonly string[];
  readonly connectorsByKey: Readonly<Record<string, Connector>>;
  readonly revision: number;
}
