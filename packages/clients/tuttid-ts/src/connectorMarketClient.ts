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
  startConnectorMarketAuthorization,
  uninstallConnectorMarketConnector
} from "./generated/index.ts";
import type {
  ConnectorMarketAuthorizationResponse,
  ConnectorMarketAuthorizationCancelRequest,
  ConnectorMarketAuthorizationRequestWritable,
  ConnectorMarketCatalogPage,
  ConnectorMarketCategoriesResponse,
  ConnectorMarketConnector,
  ConnectorMarketError,
  ConnectorMarketMutationRequest,
  ConnectorMarketMutationResponse,
  ConnectorMarketOperation,
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
  getConnectorMarket(): Promise<ConnectorMarketSnapshot>;
  listConnectorMarketCategories(): Promise<ConnectorMarketCategoriesResponse>;
  listConnectorMarketCatalog(input: {
    installation?: "not_installed";
    sectionId: string;
    pageSize?: number;
    pageToken?: string;
  }): Promise<ConnectorMarketCatalogPage>;
  getConnectorMarketConnector(
    connectorKey: string
  ): Promise<ConnectorMarketConnector>;
  getConnectorMarketOperation(
    operationId: string
  ): Promise<ConnectorMarketOperation>;
  refreshConnectorMarket(
    request: ConnectorMarketMutationRequest
  ): Promise<ConnectorMarketMutationResponse>;
  installConnectorMarketConnector(
    connectorKey: string,
    request: ConnectorMarketConnectorMutationRequest
  ): Promise<ConnectorMarketMutationResponse>;
  uninstallConnectorMarketConnector(
    connectorKey: string,
    request: ConnectorMarketConnectorMutationRequest
  ): Promise<ConnectorMarketMutationResponse>;
  startConnectorMarketAuthorization(
    connectorKey: string,
    request: ConnectorMarketConnectorAuthorizationRequest
  ): Promise<ConnectorMarketAuthorizationResponse>;
  cancelConnectorMarketAuthorization(
    connectorKey: string,
    request: ConnectorMarketAuthorizationCancelRequest
  ): Promise<ConnectorMarketMutationResponse>;
  disconnectConnectorMarketAuthorization(
    connectorKey: string,
    request: ConnectorMarketConnectorMutationRequest
  ): Promise<ConnectorMarketMutationResponse>;
}

export function createConnectorMarketClient(
  client: Client
): ConnectorMarketClient {
  return {
    async getConnectorMarket() {
      return unwrapConnectorMarketData(
        await getConnectorMarket({ client }),
        "Get connector market request failed."
      );
    },
    async listConnectorMarketCategories() {
      return unwrapConnectorMarketData(
        await listConnectorMarketCategories({ client }),
        "List connector market categories request failed."
      );
    },
    async listConnectorMarketCatalog(input) {
      return unwrapConnectorMarketData(
        await listConnectorMarketCatalog({ client, query: input }),
        "List connector market catalog request failed."
      );
    },
    async getConnectorMarketConnector(connectorKey) {
      return unwrapConnectorMarketData(
        await getConnectorMarketConnector({
          client,
          path: { connectorKey }
        }),
        "Get connector market connector request failed."
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
      return runConnectorMarketCommand(
        () => refreshConnectorMarket({ client, body: request }),
        request.expectedRevision
      );
    },
    async installConnectorMarketConnector(connectorKey, request) {
      return runConnectorMarketCommand(
        () =>
          installConnectorMarketConnector({
            client,
            body: request,
            path: { connectorKey }
          }),
        request.expectedRevision
      );
    },
    async uninstallConnectorMarketConnector(connectorKey, request) {
      return runConnectorMarketCommand(
        () =>
          uninstallConnectorMarketConnector({
            client,
            body: request,
            path: { connectorKey }
          }),
        request.expectedRevision
      );
    },
    async startConnectorMarketAuthorization(connectorKey, request) {
      return runConnectorMarketCommand(
        () =>
          startConnectorMarketAuthorization({
            client,
            body: request,
            path: { connectorKey }
          }),
        request.expectedRevision
      );
    },
    async cancelConnectorMarketAuthorization(connectorKey, request) {
      return runConnectorMarketCommand(
        () =>
          cancelConnectorMarketAuthorization({
            client,
            body: request,
            path: { connectorKey }
          }),
        request.expectedRevision
      );
    },
    async disconnectConnectorMarketAuthorization(connectorKey, request) {
      return runConnectorMarketCommand(
        () =>
          disconnectConnectorMarketAuthorization({
            client,
            body: request,
            path: { connectorKey }
          }),
        request.expectedRevision
      );
    }
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
