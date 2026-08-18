import type {
  Connector,
  ConnectorAuthorizationResult,
  ConnectorCatalogFreshness,
  ConnectorMarketBackend,
  ConnectorMarketCatalogPage,
  ConnectorMarketSnapshot,
  ConnectorMutationResult,
  ConnectorOperation,
  ConnectorPresentation,
  ConnectorPresentationAction,
  ConnectorPresentationState
} from "@tutti-os/connector-market/contracts";
import type {
  ConnectorMarketCanonicalAuthorizationResponse,
  ConnectorMarketCanonicalCatalogPage,
  ConnectorMarketCanonicalConnector,
  ConnectorMarketCanonicalMutationResponse,
  ConnectorMarketCanonicalSnapshot,
  ConnectorMarketClient
} from "@tutti-os/client-tuttid-ts";

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
const freshnessStates = new Set<ConnectorCatalogFreshness["state"]>([
  "unavailable",
  "refreshing",
  "fresh",
  "stale"
]);
const unsupportedPresentation: ConnectorPresentation = {
  state: "unsupported",
  reasonCode: "unsupported_connector_presentation",
  allowedActions: ["details", "remove_selection"]
};

export function createDesktopConnectorMarketBackend(
  client: ConnectorMarketClient
): ConnectorMarketBackend {
  return {
    async getSnapshot() {
      return mapSnapshot(await client.getConnectorMarket());
    },
    async listCategories() {
      return (await client.listConnectorMarketCategories()).categories.map(
        (category) => ({
          categoryId: category.categoryId,
          kind: category.kind,
          sortOrder: category.sortOrder,
          itemCount: category.itemCount,
          ...(category.displayNameZh
            ? { displayNameZh: category.displayNameZh }
            : {}),
          ...(category.displayNameEn
            ? { displayNameEn: category.displayNameEn }
            : {})
        })
      );
    },
    async listCatalogPage(input) {
      return mapCatalogPage(await client.listConnectorMarketCatalog(input));
    },
    async getConnector({ connectorKey }) {
      return mapConnector(
        await client.getConnectorMarketConnector(connectorKey)
      );
    },
    getOperation({ operationId }) {
      return client.getConnectorMarketOperation(operationId).then(mapOperation);
    },
    async refreshCatalog(input) {
      return mapMutationResult(await client.refreshConnectorMarket(input));
    },
    async installConnector({ connectorKey, ...request }) {
      return mapMutationResult(
        await client.installConnectorMarketConnector(connectorKey, request)
      );
    },
    async uninstallConnector({ connectorKey, ...request }) {
      return mapMutationResult(
        await client.uninstallConnectorMarketConnector(connectorKey, request)
      );
    },
    async restartRuntime({ connectorKey, ...request }) {
      return mapMutationResult(
        await client.restartConnectorMarketRuntime(connectorKey, request)
      );
    },
    async beginAuthorization({ connectorKey, ...request }) {
      return mapAuthorizationResult(
        await client.startConnectorMarketAuthorization(connectorKey, request)
      );
    },
    async cancelAuthorization({ connectorKey, ...request }) {
      return mapMutationResult(
        await client.cancelConnectorMarketAuthorization(connectorKey, request)
      );
    },
    async disconnectAuthorization({ connectorKey, ...request }) {
      return mapMutationResult(
        await client.disconnectConnectorMarketAuthorization(
          connectorKey,
          request
        )
      );
    }
  };
}

function mapSnapshot(
  snapshot: ConnectorMarketCanonicalSnapshot
): ConnectorMarketSnapshot {
  return {
    catalogFreshness: mapCatalogFreshness(snapshot.catalogFreshness),
    connectors: snapshot.connectors.map(mapConnector),
    operations: snapshot.operations.map(mapOperation),
    revision: snapshot.revision,
    eventCursor: snapshot.eventCursor
  };
}

function mapCatalogPage(
  page: ConnectorMarketCanonicalCatalogPage
): ConnectorMarketCatalogPage {
  return {
    sectionId: page.sectionId,
    items: page.items.map((item) => ({
      categoryId: item.categoryId,
      featured: item.featured,
      connector: mapConnector(item.connector)
    })),
    ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}),
    revision: page.revision
  };
}

function mapMutationResult(
  result: ConnectorMarketCanonicalMutationResponse
): ConnectorMutationResult {
  return {
    outcome: result.outcome,
    ...(result.connector ? { connector: mapConnector(result.connector) } : {}),
    ...(result.operation ? { operation: mapOperation(result.operation) } : {}),
    ...(result.failure
      ? {
          failure: {
            code: result.failure.code,
            message: result.failure.message,
            retryable: result.failure.retryable
          }
        }
      : {}),
    revision: result.revision
  };
}

function mapAuthorizationResult(
  result: ConnectorMarketCanonicalAuthorizationResponse
): ConnectorAuthorizationResult {
  return {
    ...mapMutationResult(result),
    ...(result.authorizationUrl
      ? { authorizationUrl: result.authorizationUrl }
      : {}),
    ...(result.authorizationExpiresAt
      ? { authorizationExpiresAt: result.authorizationExpiresAt }
      : {}),
    ...(result.authorizationView
      ? { authorizationView: result.authorizationView }
      : {})
  };
}

function mapConnector(connector: ConnectorMarketCanonicalConnector): Connector {
  return {
    key: connector.key,
    release: {
      schemaVersion: connector.release.schemaVersion,
      releaseId: connector.release.releaseId,
      connectorKey: connector.release.connectorKey,
      version: connector.release.version,
      releaseDigest: connector.release.releaseDigest,
      manifestDigest: connector.release.manifestDigest,
      manifest: {
        schemaVersion: connector.release.manifest.schemaVersion,
        displayName: connector.release.manifest.displayName,
        iconUrl: connector.release.manifest.iconUrl,
        ...(connector.release.manifest.description
          ? { description: connector.release.manifest.description }
          : {}),
        ...(connector.release.manifest.agentRouting
          ? {
              agentRouting: {
                aliases: [...connector.release.manifest.agentRouting.aliases]
              }
            }
          : {}),
        permissions: [...connector.release.manifest.permissions],
        implementation: {
          kind: connector.release.manifest.implementation.kind
        },
        authorizationKind: connector.release.manifest.authorizationKind,
        ...(connector.release.manifest.authorizationInteraction !== undefined
          ? {
              authorizationInteraction:
                connector.release.manifest.authorizationInteraction
            }
          : {}),
        ...(connector.release.manifest.authorizationInteractionMode
          ? {
              authorizationInteractionMode:
                connector.release.manifest.authorizationInteractionMode
            }
          : {}),
        ...(connector.release.manifest.compatibility
          ? {
              compatibility: {
                ...(connector.release.manifest.compatibility.products
                  ? {
                      products: [
                        ...connector.release.manifest.compatibility.products
                      ]
                    }
                  : {}),
                ...(connector.release.manifest.compatibility.platforms
                  ? {
                      platforms: [
                        ...connector.release.manifest.compatibility.platforms
                      ]
                    }
                  : {}),
                ...(connector.release.manifest.compatibility.minimumHostVersion
                  ? {
                      minimumHostVersion:
                        connector.release.manifest.compatibility
                          .minimumHostVersion
                    }
                  : {})
              }
            }
          : {})
      },
      artifact: {
        sha256: connector.release.artifact.sha256,
        sizeBytes: connector.release.artifact.sizeBytes,
        mediaType: connector.release.artifact.mediaType
      },
      publishedAt: connector.release.publishedAt,
      status: connector.release.status
    },
    installation: {
      state: connector.installation.state,
      ...(connector.installation.installedVersion
        ? { installedVersion: connector.installation.installedVersion }
        : {}),
      ...(connector.installation.installedReleaseId
        ? { installedReleaseId: connector.installation.installedReleaseId }
        : {}),
      ...(connector.installation.installedReleaseDigest
        ? {
            installedReleaseDigest:
              connector.installation.installedReleaseDigest
          }
        : {}),
      ...(connector.installation.failureCode
        ? { failureCode: connector.installation.failureCode }
        : {})
    },
    authorization: {
      state: connector.authorization.state,
      ...(connector.authorization.failureCode
        ? { failureCode: connector.authorization.failureCode }
        : {})
    },
    compatibility: {
      state: connector.compatibility.state,
      ...(connector.compatibility.reason
        ? { reason: connector.compatibility.reason }
        : {})
    },
    presentation: mapPresentation(connector.presentation),
    revision: connector.revision
  };
}

function mapOperation(
  operation: ConnectorMarketCanonicalSnapshot["operations"][number]
): ConnectorOperation {
  return {
    operationId: operation.operationId,
    clientRequestId: operation.clientRequestId,
    ...(operation.connectorKey ? { connectorKey: operation.connectorKey } : {}),
    kind: operation.kind,
    state: operation.state,
    ...(operation.stage ? { stage: operation.stage } : {}),
    ...(operation.target
      ? {
          target: {
            connectorKey: operation.target.connectorKey,
            version: operation.target.version,
            releaseId: operation.target.releaseId,
            releaseDigest: operation.target.releaseDigest,
            ...(operation.target.artifactSha256
              ? { artifactSha256: operation.target.artifactSha256 }
              : {})
          }
        }
      : {}),
    attempt: operation.attempt,
    ...(operation.failureCode ? { failureCode: operation.failureCode } : {}),
    createdAt: operation.createdAt,
    updatedAt: operation.updatedAt
  };
}

function mapCatalogFreshness(value: unknown): ConnectorCatalogFreshness {
  if (!isRecord(value) || !freshnessStates.has(value.state as never)) {
    return {
      state: "unavailable",
      lastFailure: "unsupported_catalog_freshness"
    };
  }
  const optionalKeys = [
    "snapshotId",
    "sourceRevision",
    "acceptedAt",
    "staleSince",
    "lastFailure"
  ] as const;
  if (
    optionalKeys.some(
      (key) => value[key] !== undefined && typeof value[key] !== "string"
    )
  ) {
    return {
      state: "unavailable",
      lastFailure: "unsupported_catalog_freshness"
    };
  }
  return {
    state: value.state as ConnectorCatalogFreshness["state"],
    ...Object.fromEntries(
      optionalKeys
        .filter((key) => typeof value[key] === "string")
        .map((key) => [key, value[key]])
    )
  };
}

function mapPresentation(value: unknown): ConnectorPresentation {
  if (
    !isRecord(value) ||
    !presentationStates.has(value.state as never) ||
    !Array.isArray(value.allowedActions) ||
    value.allowedActions.some(
      (action) =>
        typeof action !== "string" ||
        !presentationActions.has(action as ConnectorPresentationAction)
    ) ||
    new Set(value.allowedActions).size !== value.allowedActions.length ||
    (value.state === "connected") !== value.allowedActions.includes("select") ||
    (value.state !== "connected" &&
      (typeof value.reasonCode !== "string" ||
        value.reasonCode.length === 0)) ||
    (value.reasonCode !== undefined && typeof value.reasonCode !== "string")
  ) {
    return {
      ...unsupportedPresentation,
      allowedActions: [...unsupportedPresentation.allowedActions]
    };
  }
  return {
    state: value.state as ConnectorPresentationState,
    ...(typeof value.reasonCode === "string"
      ? { reasonCode: value.reasonCode }
      : {}),
    allowedActions: [...value.allowedActions] as ConnectorPresentationAction[]
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object";
}
