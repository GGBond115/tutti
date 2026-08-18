import type {
  ConnectorRendererInstallOutcome,
  ConnectorRendererSegment,
  ConnectorRendererSurfaceSnapshot
} from "./connectorRendererSurface.ts";
import type { Connector, ConnectorOperation } from "../contracts/index.ts";
export type { ConnectorRendererSurfaceSnapshot } from "./connectorRendererSurface.ts";

export type ConnectorRendererStatus =
  | "unavailable"
  | "loading"
  | "setup_required"
  | "authorization_required"
  | "connecting"
  | "connected"
  | "degraded"
  | "disabled"
  | "unsupported"
  | "failed";

export interface ConnectorRendererItem {
  readonly connectorKey: string;
  readonly iconUrl?: string;
  readonly name: string;
  readonly revision: number;
  readonly status: ConnectorRendererStatus;
}

export interface ConnectorRendererSnapshot {
  /** Application-owned entry admission; hosts must not infer daemon health. */
  readonly entryAvailable: boolean;
  readonly phase: "loading" | "ready" | "failed";
  readonly items: readonly ConnectorRendererItem[];
  readonly revision: number;
  readonly stale: boolean;
}

export interface ConnectorRendererCommands {
  /** Forces an authoritative local-daemon reload. */
  refresh(): Promise<void>;
  refreshCatalog(): Promise<void>;
  loadMore(sectionId: string): Promise<void>;
  install(connectorKey: string): Promise<ConnectorRendererInstallOutcome>;
  uninstall(connectorKey: string): Promise<unknown>;
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

export interface ConnectorRendererAgentPolicySnapshot {
  readonly status: "loading" | "ready" | "unavailable";
  /** `null` means the local Agent supports the validated catalog. */
  readonly supportedConnectorKeys: readonly string[] | null;
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
  const port = secureSubmissionPorts.get(model);
  return (
    port ?? {
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

const connectorRendererStatuses = new Set<ConnectorRendererStatus>([
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

const localAgentPolicySnapshot: ConnectorRendererAgentPolicySnapshot =
  Object.freeze({ status: "ready", supportedConnectorKeys: null });
const unavailableSharedAgentPolicySnapshot: ConnectorRendererAgentPolicySnapshot =
  Object.freeze({ status: "unavailable", supportedConnectorKeys: [] });

/** Unknown future values remain visible but fail closed. */
export function normalizeConnectorRendererStatus(
  status: string
): ConnectorRendererStatus {
  return connectorRendererStatuses.has(status as ConnectorRendererStatus)
    ? (status as ConnectorRendererStatus)
    : "unsupported";
}

export function projectConnectorRendererSnapshot(
  market: ConnectorRendererProjectionSource
): ConnectorRendererSnapshot {
  const stale =
    market.loadState !== "ready" || market.catalogMutationState === "blocked";
  const items = market.connectorKeys.flatMap((connectorKey) => {
    const connector = market.connectorsByKey[connectorKey];
    if (!connector) {
      return [];
    }
    const operation = market.operationsByConnectorKey[connectorKey];
    const status = projectConnectorStatus({
      authorization: connector.authorization.state,
      compatibility: connector.compatibility.state,
      installation: connector.installation.state,
      operationState: operation?.state,
      operationStage: operation?.stage,
      pendingAuthorization:
        market.pendingAuthorizationsByConnectorKey[connectorKey] === true,
      pendingInstallation:
        market.pendingInstallationsByConnectorKey[connectorKey] === true
    });
    return [
      {
        connectorKey,
        iconUrl: connector.release.manifest.iconUrl,
        name: connector.release.manifest.displayName,
        revision: connector.revision,
        status
      }
    ];
  });

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
    items: Object.freeze(items),
    revision: market.revision,
    stale
  });
}

export interface ConnectorRendererProjectionSource {
  readonly loadState: string;
  readonly catalogMutationState: "allowed" | "blocked";
  readonly connectorKeys: readonly string[];
  readonly connectorsByKey: Readonly<Record<string, Connector>>;
  readonly operationsByConnectorKey: Readonly<
    Record<string, ConnectorOperation>
  >;
  readonly pendingAuthorizationsByConnectorKey: Readonly<Record<string, true>>;
  readonly pendingInstallationsByConnectorKey: Readonly<Record<string, true>>;
  readonly revision: number;
}

export function projectConnectorStatus(input: {
  authorization: string;
  compatibility: string;
  installation: string;
  operationState?: string;
  operationStage?: string;
  pendingAuthorization: boolean;
  pendingInstallation: boolean;
}): ConnectorRendererStatus {
  if (input.compatibility !== "supported") {
    return input.compatibility.startsWith("unsupported_")
      ? "unsupported"
      : "unavailable";
  }
  if (
    input.installation === "failed" ||
    input.authorization === "failed" ||
    input.operationState === "failed" ||
    input.operationStage === "failed"
  ) {
    return "failed";
  }
  if (
    input.pendingInstallation ||
    input.pendingAuthorization ||
    ["installing", "updating", "uninstalling"].includes(input.installation) ||
    input.authorization === "pending" ||
    (input.operationState !== undefined &&
      !["completed", "failed"].includes(input.operationState))
  ) {
    return "connecting";
  }
  if (input.installation === "not_installed") {
    return "setup_required";
  }
  if (input.installation !== "installed") {
    return "unsupported";
  }
  if (["disconnected", "expired"].includes(input.authorization)) {
    return "authorization_required";
  }
  if (["connected", "not_required"].includes(input.authorization)) {
    return "connected";
  }
  return "unsupported";
}
