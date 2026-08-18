import {
  cancelConnectorMarketAuthorization,
  disconnectConnectorMarketAuthorization,
  getConnectorMarket,
  getConnectorMarketConnector,
  getConnectorMarketOperation,
  installConnectorMarketConnector,
  listConnectorMarketCatalog,
  listConnectorMarketCategories,
  refreshConnectorMarket,
  restartConnectorMarketRuntime,
  startConnectorMarketAuthorization,
  uninstallConnectorMarketConnector
} from "./generated/index.ts";
import type {
  ConnectorMarketAuthorizationResponse,
  ConnectorMarketAuthorizationCancelRequest,
  ConnectorMarketAuthorizationRequestWritable,
  ConnectorMarketCatalogPage,
  ConnectorMarketCatalogFreshness,
  ConnectorMarketCategoriesResponse,
  ConnectorMarketConnector,
  ConnectorMarketError,
  ConnectorMarketMutationRequest,
  ConnectorMarketMutationResponse,
  ConnectorMarketOperation,
  ConnectorMarketPresentation,
  ConnectorMarketPresentationAction,
  ConnectorMarketPresentationState,
  ConnectorMarketSnapshot
} from "./generated/index.ts";
import type { Client } from "./generated/client/index.ts";
import { unwrapData } from "./tuttidClientResponse.ts";

interface ConnectorMarketClientResponse<TResult> {
  data?: TResult;
  error?: unknown;
  response?: Response;
}

type ConnectorMarketConnectorMutationRequest =
  ConnectorMarketMutationRequest & {
    expectedConnectorRevision: number;
  };

type ConnectorMarketConnectorAuthorizationRequest =
  ConnectorMarketAuthorizationRequestWritable & {
    expectedConnectorRevision: number;
  };

export type ConnectorMarketCanonicalConnector = Omit<
  ConnectorMarketConnector,
  "presentation"
> & {
  presentation: ConnectorMarketPresentation;
};

export type ConnectorMarketCanonicalSnapshot = Omit<
  ConnectorMarketSnapshot,
  "catalogState" | "sourceRevision" | "catalogFreshness" | "connectors"
> & {
  catalogFreshness: ConnectorMarketCatalogFreshness;
  connectors: ConnectorMarketCanonicalConnector[];
};

export type ConnectorMarketCanonicalCatalogPage = Omit<
  ConnectorMarketCatalogPage,
  "items"
> & {
  items: Array<
    Omit<ConnectorMarketCatalogPage["items"][number], "connector"> & {
      connector: ConnectorMarketCanonicalConnector;
    }
  >;
};

export type ConnectorMarketCanonicalMutationResponse = Omit<
  ConnectorMarketMutationResponse,
  "connector"
> & {
  connector?: ConnectorMarketCanonicalConnector;
};

export type ConnectorMarketCanonicalAuthorizationResponse = Omit<
  ConnectorMarketAuthorizationResponse,
  "connector"
> & {
  connector?: ConnectorMarketCanonicalConnector;
};

export class ConnectorMarketClientError extends Error {
  readonly code: ConnectorMarketError["code"];
  readonly retryable: boolean;
  readonly revision?: number;
  readonly statusCode: number;
  readonly details: Readonly<ConnectorMarketError>;

  constructor(details: ConnectorMarketError, statusCode: number) {
    super(details.message);
    this.name = "ConnectorMarketClientError";
    this.code = details.code;
    this.retryable = details.retryable;
    this.revision = details.revision;
    this.statusCode = statusCode;
    this.details = details;
  }
}

export function isConnectorMarketClientError(
  error: unknown
): error is ConnectorMarketClientError {
  return error instanceof ConnectorMarketClientError;
}

export interface ConnectorMarketClient {
  getConnectorMarket(): Promise<ConnectorMarketCanonicalSnapshot>;
  listConnectorMarketCategories(): Promise<ConnectorMarketCategoriesResponse>;
  listConnectorMarketCatalog(input: {
    installation?: "not_installed";
    sectionId: string;
    pageSize?: number;
    pageToken?: string;
  }): Promise<ConnectorMarketCanonicalCatalogPage>;
  getConnectorMarketConnector(
    connectorKey: string
  ): Promise<ConnectorMarketCanonicalConnector>;
  getConnectorMarketOperation(
    operationId: string
  ): Promise<ConnectorMarketOperation>;
  refreshConnectorMarket(
    request: ConnectorMarketMutationRequest
  ): Promise<ConnectorMarketCanonicalMutationResponse>;
  installConnectorMarketConnector(
    connectorKey: string,
    request: ConnectorMarketConnectorMutationRequest
  ): Promise<ConnectorMarketCanonicalMutationResponse>;
  uninstallConnectorMarketConnector(
    connectorKey: string,
    request: ConnectorMarketConnectorMutationRequest
  ): Promise<ConnectorMarketCanonicalMutationResponse>;
  restartConnectorMarketRuntime(
    connectorKey: string,
    request: ConnectorMarketConnectorMutationRequest
  ): Promise<ConnectorMarketCanonicalMutationResponse>;
  startConnectorMarketAuthorization(
    connectorKey: string,
    request: ConnectorMarketConnectorAuthorizationRequest
  ): Promise<ConnectorMarketCanonicalAuthorizationResponse>;
  cancelConnectorMarketAuthorization(
    connectorKey: string,
    request: ConnectorMarketAuthorizationCancelRequest
  ): Promise<ConnectorMarketCanonicalMutationResponse>;
  disconnectConnectorMarketAuthorization(
    connectorKey: string,
    request: ConnectorMarketConnectorMutationRequest
  ): Promise<ConnectorMarketCanonicalMutationResponse>;
}

export function createConnectorMarketClient(
  client: Client
): ConnectorMarketClient {
  return {
    async getConnectorMarket() {
      return decodeConnectorMarketSnapshot(
        unwrapConnectorMarketData(
          await getConnectorMarket({ client }),
          "Get connector market request failed."
        )
      );
    },
    async listConnectorMarketCategories() {
      return unwrapConnectorMarketData(
        await listConnectorMarketCategories({ client }),
        "List connector market categories request failed."
      );
    },
    async listConnectorMarketCatalog(input) {
      return decodeConnectorMarketCatalogPage(
        unwrapConnectorMarketData(
          await listConnectorMarketCatalog({ client, query: input }),
          "List connector market catalog request failed."
        )
      );
    },
    async getConnectorMarketConnector(connectorKey) {
      return decodeConnectorMarketConnector(
        unwrapConnectorMarketData(
          await getConnectorMarketConnector({
            client,
            path: { connectorKey }
          }),
          "Get connector market connector request failed."
        )
      );
    },
    async getConnectorMarketOperation(operationId) {
      return unwrapConnectorMarketData(
        await getConnectorMarketOperation({
          client,
          path: { operationID: operationId }
        }),
        "Get connector market operation request failed."
      );
    },
    async refreshConnectorMarket(request) {
      return decodeConnectorMarketMutationResponse(
        await runConnectorMarketCommand(
          () => refreshConnectorMarket({ client, body: request }),
          request.expectedRevision
        )
      );
    },
    async installConnectorMarketConnector(connectorKey, request) {
      return decodeConnectorMarketMutationResponse(
        await runConnectorMarketCommand(
          () =>
            installConnectorMarketConnector({
              client,
              body: request,
              path: { connectorKey }
            }),
          request.expectedRevision
        )
      );
    },
    async uninstallConnectorMarketConnector(connectorKey, request) {
      return decodeConnectorMarketMutationResponse(
        await runConnectorMarketCommand(
          () =>
            uninstallConnectorMarketConnector({
              client,
              body: request,
              path: { connectorKey }
            }),
          request.expectedRevision
        )
      );
    },
    async restartConnectorMarketRuntime(connectorKey, request) {
      return decodeConnectorMarketMutationResponse(
        await runConnectorMarketCommand(
          () =>
            restartConnectorMarketRuntime({
              client,
              body: request,
              path: { connectorKey }
            }),
          request.expectedRevision
        )
      );
    },
    async startConnectorMarketAuthorization(connectorKey, request) {
      return decodeConnectorMarketAuthorizationResponse(
        await runConnectorMarketCommand(
          () =>
            startConnectorMarketAuthorization({
              client,
              body: request,
              path: { connectorKey }
            }),
          request.expectedRevision
        )
      );
    },
    async cancelConnectorMarketAuthorization(connectorKey, request) {
      return decodeConnectorMarketMutationResponse(
        await runConnectorMarketCommand(
          () =>
            cancelConnectorMarketAuthorization({
              client,
              body: request,
              path: { connectorKey }
            }),
          request.expectedRevision
        )
      );
    },
    async disconnectConnectorMarketAuthorization(connectorKey, request) {
      return decodeConnectorMarketMutationResponse(
        await runConnectorMarketCommand(
          () =>
            disconnectConnectorMarketAuthorization({
              client,
              body: request,
              path: { connectorKey }
            }),
          request.expectedRevision
        )
      );
    }
  };
}

const connectorPresentationStates = new Set<ConnectorMarketPresentationState>([
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

const connectorPresentationActions = new Set<ConnectorMarketPresentationAction>(
  [
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
  ]
);

function decodeConnectorMarketSnapshot(
  snapshot: ConnectorMarketSnapshot
): ConnectorMarketCanonicalSnapshot {
  const freshness = normalizeConnectorMarketFreshness(snapshot);
  const {
    catalogState: _catalogState,
    sourceRevision: _sourceRevision,
    ...rest
  } = snapshot;
  return {
    ...rest,
    catalogFreshness: freshness,
    connectors: snapshot.connectors.map((connector) =>
      decodeConnectorMarketConnector(connector, freshness)
    )
  };
}

function decodeConnectorMarketCatalogPage(
  page: ConnectorMarketCatalogPage
): ConnectorMarketCanonicalCatalogPage {
  return {
    ...page,
    items: page.items.map((item) => ({
      ...item,
      connector: decodeConnectorMarketConnector(item.connector)
    }))
  };
}

function decodeConnectorMarketMutationResponse(
  response: ConnectorMarketMutationResponse
): ConnectorMarketCanonicalMutationResponse {
  const { connector, ...rest } = response;
  return connector
    ? { ...rest, connector: decodeConnectorMarketConnector(connector) }
    : rest;
}

function decodeConnectorMarketAuthorizationResponse(
  response: ConnectorMarketAuthorizationResponse
): ConnectorMarketCanonicalAuthorizationResponse {
  const { connector, ...rest } = response;
  return connector
    ? { ...rest, connector: decodeConnectorMarketConnector(connector) }
    : rest;
}

function decodeConnectorMarketConnector(
  connector: ConnectorMarketConnector,
  freshness?: ConnectorMarketCatalogFreshness
): ConnectorMarketCanonicalConnector {
  return {
    ...connector,
    presentation: normalizeConnectorMarketPresentation(connector, freshness)
  };
}

function normalizeConnectorMarketFreshness(
  snapshot: ConnectorMarketSnapshot
): ConnectorMarketCatalogFreshness {
  const raw = snapshot.catalogFreshness;
  if (
    raw &&
    (raw.state === "unavailable" ||
      raw.state === "refreshing" ||
      raw.state === "fresh" ||
      raw.state === "stale")
  ) {
    return { ...raw };
  }
  const sourceRevision =
    typeof snapshot.sourceRevision === "string" &&
    snapshot.sourceRevision.length > 0
      ? snapshot.sourceRevision
      : undefined;
  switch (snapshot.catalogState as string) {
    case "ready":
      return { state: "fresh", sourceRevision };
    case "refreshing":
      return { state: "refreshing", sourceRevision };
    case "stale":
      return { state: "stale", sourceRevision };
    case "failed":
      return {
        state: snapshot.connectors.length > 0 ? "stale" : "unavailable",
        sourceRevision
      };
    default:
      return { state: "unavailable", sourceRevision };
  }
}

function normalizeConnectorMarketPresentation(
  connector: ConnectorMarketConnector,
  freshness?: ConnectorMarketCatalogFreshness
): ConnectorMarketPresentation {
  const presentation = connector.presentation as unknown;
  if (isConnectorMarketPresentation(presentation)) {
    return {
      state: presentation.state,
      ...(presentation.reasonCode
        ? { reasonCode: presentation.reasonCode }
        : {}),
      allowedActions: [...presentation.allowedActions]
    };
  }
  if (presentation !== undefined) {
    return unsupportedConnectorMarketPresentation(
      "unsupported_connector_presentation"
    );
  }
  return deriveLegacyConnectorMarketPresentation(connector, freshness);
}

function isConnectorMarketPresentation(
  value: unknown
): value is ConnectorMarketPresentation {
  if (
    value === null ||
    typeof value !== "object" ||
    !("state" in value) ||
    typeof value.state !== "string" ||
    !connectorPresentationStates.has(
      value.state as ConnectorMarketPresentationState
    ) ||
    !("allowedActions" in value) ||
    !Array.isArray(value.allowedActions)
  ) {
    return false;
  }
  const actions = value.allowedActions;
  if (
    actions.some(
      (action) =>
        typeof action !== "string" ||
        !connectorPresentationActions.has(
          action as ConnectorMarketPresentationAction
        )
    ) ||
    new Set(actions).size !== actions.length ||
    (value.state === "connected") !== actions.includes("select") ||
    (value.state !== "connected" &&
      (!("reasonCode" in value) ||
        typeof value.reasonCode !== "string" ||
        value.reasonCode.length === 0)) ||
    ("reasonCode" in value &&
      value.reasonCode !== undefined &&
      typeof value.reasonCode !== "string")
  ) {
    return false;
  }
  return true;
}

function deriveLegacyConnectorMarketPresentation(
  connector: ConnectorMarketConnector,
  freshness?: ConnectorMarketCatalogFreshness
): ConnectorMarketPresentation {
  // One-version compatibility is intentionally read-only when a page/get/
  // command response does not carry snapshot freshness. In particular, an
  // installed and authorized legacy Connector has no exact runtime evidence,
  // so it can never decode as connected or selectable.
  const safeActions: ConnectorMarketPresentationAction[] = [
    "details",
    "remove_selection"
  ];
  const permitsNewMutation =
    freshness?.state === "fresh" ||
    (freshness?.state === "refreshing" && freshness.staleSince === undefined);
  if (connector.compatibility.state !== "supported") {
    return unsupportedConnectorMarketPresentation(
      connector.compatibility.state
    );
  }
  switch (connector.installation.state as string) {
    case "not_installed":
      return {
        state: "setup_required",
        reasonCode: "connector_not_installed",
        allowedActions: permitsNewMutation
          ? [...safeActions, "install"]
          : safeActions
      };
    case "installing":
    case "updating":
    case "uninstalling":
      return {
        state: "connecting",
        reasonCode: "installation_converging",
        allowedActions: safeActions
      };
    case "failed":
      return {
        state: "failed",
        reasonCode:
          connector.installation.failureCode || "connector_installation_failed",
        allowedActions: safeActions
      };
    case "installed":
      break;
    default:
      return unsupportedConnectorMarketPresentation(
        "unknown_installation_state"
      );
  }
  switch (connector.authorization.state as string) {
    case "disconnected":
    case "expired":
      return {
        state: "authorization_required",
        reasonCode:
          connector.authorization.failureCode ||
          "connector_authorization_required",
        allowedActions: permitsNewMutation
          ? [...safeActions, "authorize"]
          : safeActions
      };
    case "pending":
      return {
        state: "connecting",
        reasonCode: "authorization_pending",
        allowedActions: [...safeActions, "cancel"]
      };
    case "failed":
      return {
        state: "failed",
        reasonCode:
          connector.authorization.failureCode ||
          "connector_authorization_failed",
        allowedActions: safeActions
      };
    case "not_required":
    case "connected":
      return unsupportedConnectorMarketPresentation(
        "legacy_runtime_observation_missing"
      );
    default:
      return unsupportedConnectorMarketPresentation(
        "unknown_authorization_state"
      );
  }
}

function unsupportedConnectorMarketPresentation(
  reasonCode: string
): ConnectorMarketPresentation {
  return {
    state: "unsupported",
    reasonCode,
    allowedActions: ["details", "remove_selection"]
  };
}

async function runConnectorMarketCommand<
  TResult extends ConnectorMarketMutationResponse
>(
  request: () => Promise<ConnectorMarketClientResponse<TResult>>,
  expectedRevision: number
): Promise<TResult> {
  try {
    const response = await request();
    const details = connectorMarketErrorDetails(response.error);
    if (details) {
      return {
        outcome: "rejected",
        revision: details.revision ?? expectedRevision,
        failure: {
          code: details.code,
          message: details.message,
          // A revision conflict is a user-visible stale command, never an
          // admission to automatically replay the side effect.
          retryable:
            details.code === "connector_market_revision_conflict"
              ? false
              : details.retryable
        }
      } as TResult;
    }
    if (isConnectorMarketCommandResult(response.data)) {
      return response.data as TResult;
    }
  } catch {
    // A thrown transport error after dispatch cannot establish whether the
    // daemon durably accepted the command.
  }
  return {
    outcome: "uncertain",
    revision: expectedRevision,
    failure: {
      code: "connector_market_unavailable",
      message: "connector command acceptance could not be determined",
      retryable: true
    }
  } as TResult;
}

function isConnectorMarketCommandResult(
  value: unknown
): value is ConnectorMarketMutationResponse {
  if (
    value === null ||
    typeof value !== "object" ||
    !("outcome" in value) ||
    !("revision" in value) ||
    typeof value.revision !== "number" ||
    !Number.isSafeInteger(value.revision) ||
    value.revision < 0
  ) {
    return false;
  }
  if (value.outcome === "accepted") {
    const operation = "operation" in value ? value.operation : undefined;
    return (
      isConnectorMarketOperationState(operation, "accepted", "running") &&
      (!("failure" in value) || value.failure === undefined)
    );
  }
  if (value.outcome === "completed") {
    const operation = "operation" in value ? value.operation : undefined;
    return (
      (!("failure" in value) || value.failure === undefined) &&
      (operation === undefined ||
        isConnectorMarketOperationState(operation, "completed"))
    );
  }
  if (value.outcome !== "rejected" && value.outcome !== "uncertain") {
    return false;
  }
  if (!("failure" in value) || !isConnectorMarketErrorDetails(value.failure)) {
    return false;
  }
  return (
    (!("operation" in value) || value.operation === undefined) &&
    value.failure.code.length > 0 &&
    value.failure.message.length > 0 &&
    (value.outcome !== "uncertain" || value.failure.retryable) &&
    (value.failure.code !== "connector_market_revision_conflict" ||
      !value.failure.retryable)
  );
}

function isConnectorMarketOperationState(
  value: unknown,
  ...states: readonly string[]
): boolean {
  return (
    value !== null &&
    typeof value === "object" &&
    "state" in value &&
    typeof value.state === "string" &&
    states.includes(value.state)
  );
}

function unwrapConnectorMarketData<TResult>(
  response: ConnectorMarketClientResponse<TResult>,
  fallback: string
): TResult {
  const details = connectorMarketErrorDetails(response.error);
  if (details) {
    throw new ConnectorMarketClientError(
      details,
      response.response?.status ?? 0
    );
  }
  return unwrapData(response, fallback);
}

function connectorMarketErrorDetails(
  error: unknown
): ConnectorMarketError | null {
  if (isConnectorMarketErrorDetails(error)) {
    return error;
  }
  if (
    error &&
    typeof error === "object" &&
    "error" in error &&
    isConnectorMarketErrorDetails(error.error)
  ) {
    return error.error;
  }
  return null;
}

function isConnectorMarketErrorDetails(
  value: unknown
): value is ConnectorMarketError {
  return (
    value !== null &&
    typeof value === "object" &&
    "code" in value &&
    typeof value.code === "string" &&
    value.code.startsWith("connector_") &&
    "message" in value &&
    typeof value.message === "string" &&
    "retryable" in value &&
    typeof value.retryable === "boolean" &&
    (!("revision" in value) ||
      value.revision === undefined ||
      (typeof value.revision === "number" &&
        Number.isSafeInteger(value.revision) &&
        value.revision >= 0))
  );
}
