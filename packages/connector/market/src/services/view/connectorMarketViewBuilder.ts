import type { AuthorizationViewEnvelopeV1 } from "@tutti-os/connector-authorization-protocol/v1";

import type {
  Connector,
  ConnectorPresentationAction
} from "../../contracts/index.ts";
import type { ConnectorMarketStoreState } from "../connectorMarketService.interface.ts";
import type { ConnectorMarketUiState } from "../ui-state/connectorMarketUiStateService.interface.ts";
import { projectAuthorizationQrCodeDataUrl } from "./connectorAuthorizationQrCode.ts";
import type {
  ConnectorCardAction,
  ConnectorCardView,
  ConnectorCatalogErrorView,
  ConnectorDetailFieldView,
  ConnectorDialogView,
  ConnectorMarketViewState
} from "./connectorMarketViewTypes.ts";

export function buildConnectorMarketView(
  market: ConnectorMarketStoreState,
  uiState: ConnectorMarketUiState
): ConnectorMarketViewState {
  const allConnectors = market.connectorKeys
    .map((key) => market.connectorsByKey[key])
    .filter((connector): connector is Connector => Boolean(connector));
  const installedCount = allConnectors.filter(connectorCanUninstall).length;
  const query = uiState.query.trim().toLocaleLowerCase();
  const matchesQuery = (connector: Connector) =>
    !query ||
    [
      connector.key,
      connector.release.manifest.displayName,
      connector.release.manifest.description ?? ""
    ].some((value) => value.toLocaleLowerCase().includes(query));
  const sections =
    uiState.segment === "installed"
      ? [
          {
            id: "installed",
            connectorKeys: allConnectors
              .filter(connectorCanUninstall)
              .filter(matchesQuery)
              .sort((left, right) =>
                left.release.manifest.displayName.localeCompare(
                  right.release.manifest.displayName
                )
              )
              .map((connector) => connector.key),
            error: false,
            hasMore: false,
            itemCount: installedCount,
            loading: false
          }
        ]
      : market.catalogSections.map((section) => ({
          id: section.categoryId,
          ...(section.displayNameZh === undefined
            ? {}
            : { displayNameZh: section.displayNameZh }),
          ...(section.displayNameEn === undefined
            ? {}
            : { displayNameEn: section.displayNameEn }),
          connectorKeys: section.connectorKeys.filter((key) => {
            const connector = market.connectorsByKey[key];
            return (
              connector !== undefined &&
              !connectorCanUninstall(connector) &&
              matchesQuery(connector)
            );
          }),
          error: section.loadState === "error",
          hasMore:
            section.loadState === "ready" && Boolean(section.nextPageToken),
          itemCount: section.itemCount,
          loading: section.loadState === "loading"
        }));
  const cardsByKey = Object.fromEntries(
    allConnectors.map((connector) => [
      connector.key,
      buildConnectorCardView(
        connector,
        market.operationsByConnectorKey[connector.key]?.stage ?? null
      )
    ])
  );
  const dialogConnector = uiState.dialog
    ? market.connectorsByKey[uiState.dialog.connectorKey]
    : undefined;
  const dialogKey = uiState.dialog?.connectorKey;

  return {
    availableCount: allConnectors.length - installedCount,
    cardsByKey,
    catalogError: buildCatalogErrorView(market.lastError),
    dialog: buildConnectorDialogView(
      dialogConnector,
      uiState.dialog?.kind ?? null,
      dialogKey ? Boolean(market.authorizingConnectorKeys[dialogKey]) : false,
      dialogKey
        ? market.pendingAuthorizationsByConnectorKey[dialogKey] === true
        : false,
      dialogKey
        ? market.pendingInstallationsByConnectorKey[dialogKey] === true
        : false,
      dialogKey ? market.authorizationViewsByConnectorKey[dialogKey] : undefined
    ),
    installedCount,
    refreshing: market.catalogFreshness.state === "refreshing",
    sections: sections.filter(
      (section) =>
        section.connectorKeys.length > 0 ||
        section.error ||
        section.loading ||
        section.hasMore
    ),
    status:
      market.loadState === "loading" || market.loadState === "idle"
        ? "loading"
        : market.loadState === "error"
          ? "error"
          : sections.every(
                (section) =>
                  section.connectorKeys.length === 0 &&
                  !section.error &&
                  !section.loading
              )
            ? "empty"
            : "ready"
  };
}

function buildCatalogErrorView(
  error: ConnectorMarketStoreState["lastError"]
): ConnectorCatalogErrorView | null {
  if (!error) return null;
  switch (error.code) {
    case "connector_manifest_invalid":
    case "connector_implementation_unsupported":
      return { kind: "invalid_data", retryable: error.retryable };
    case "connector_market_upstream_unavailable":
    case "connector_market_unavailable":
      return { kind: "unavailable", retryable: error.retryable };
    default:
      return { kind: "unknown", retryable: error.retryable };
  }
}

function buildConnectorCardView(
  connector: Connector,
  operationStage: ConnectorCardView["operationStage"]
): ConnectorCardView {
  const presentation = connector.presentation;
  return {
    action: primaryCardAction(presentation.allowedActions),
    allowedActions: [...presentation.allowedActions],
    connectorKey: connector.key,
    description: connector.release.manifest.description ?? "",
    displayName: connector.release.manifest.displayName,
    iconUrl: connector.release.manifest.iconUrl,
    implementationTags: implementationTags(connector),
    operationStage,
    ...(presentation.reasonCode ? { reasonCode: presentation.reasonCode } : {}),
    canUninstall: hasAction(connector, "uninstall"),
    status: presentation.state
  };
}

function primaryCardAction(
  actions: readonly ConnectorPresentationAction[]
): ConnectorCardAction {
  const priority = [
    "update",
    "install",
    "authorize",
    "cancel",
    "disconnect",
    "manage",
    "details"
  ] as const satisfies readonly ConnectorPresentationAction[];
  return priority.find((action) => actions.includes(action)) ?? "unavailable";
}

function buildConnectorDialogView(
  connector: Connector | undefined,
  requestKind: NonNullable<ConnectorMarketUiState["dialog"]>["kind"] | null,
  authorizing: boolean,
  pendingAuthorization: boolean,
  pendingInstallation: boolean,
  authorizationView: AuthorizationViewEnvelopeV1 | undefined
): ConnectorDialogView | null {
  if (!connector) return null;
  const base = {
    connectorKey: connector.key,
    description: connector.release.manifest.description ?? "",
    displayName: connector.release.manifest.displayName,
    iconUrl: connector.release.manifest.iconUrl,
    permissions: connector.release.manifest.permissions.map((permission) => ({
      id: permission,
      name: permission
    }))
  };
  if (requestKind === "uninstall_confirmation") {
    return hasAction(connector, "uninstall")
      ? { ...base, kind: "uninstall_confirmation" }
      : null;
  }
  const state = connector.presentation.state;
  const canInstall = hasAction(connector, "install");
  const canUpdate = hasAction(connector, "update");
  if (canInstall || canUpdate) {
    return {
      ...base,
      installing: pendingInstallation || state === "connecting",
      kind: "installation",
      updating: canUpdate
    };
  }
  const canAuthorize = hasAction(connector, "authorize");
  const canCancel = hasAction(connector, "cancel");
  if (canAuthorize || canCancel) {
    return {
      ...base,
      authorizationInteraction:
        connector.release.manifest.authorizationInteraction,
      authorizationKind: connector.release.manifest.authorizationKind,
      authorizationQrCodeDataUrl:
        projectAuthorizationQrCodeDataUrl(authorizationView),
      authorizationView,
      authorizing,
      brokeredAuthorization:
        connector.release.manifest.authorizationInteractionMode === "managed",
      canAuthorize,
      canCancel,
      kind: "authorization",
      pending: pendingAuthorization || state === "connecting"
    };
  }
  const canManage = hasAction(connector, "manage");
  const canDisconnect = hasAction(connector, "disconnect");
  const canUninstall = hasAction(connector, "uninstall");
  const canTry = hasAction(connector, "select");
  if (canManage || canDisconnect || canUninstall || canTry) {
    return {
      ...base,
      canDisconnect,
      canTry,
      canUninstall,
      details: buildDetailFields(connector),
      kind: "management"
    };
  }
  return {
    ...base,
    kind: "blocked",
    reason: connector.presentation.reasonCode ?? connector.presentation.state
  };
}

function connectorCanUninstall(connector: Connector): boolean {
  return hasAction(connector, "uninstall");
}

function hasAction(
  connector: Connector,
  action: ConnectorPresentationAction
): boolean {
  return connector.presentation.allowedActions.includes(action);
}

function buildDetailFields(connector: Connector): ConnectorDetailFieldView[] {
  const implementation = connector.release.manifest.implementation;
  return [
    { id: "version", value: connector.release.version },
    { id: "releaseStatus", value: connector.release.status },
    { id: "compatibility", value: connector.compatibility.state },
    { id: "transport", value: implementationTags(connector).join(" + ") },
    { id: "implementation", value: implementation.kind },
    { id: "runtime", value: implementation.kind },
    {
      id: "authorization",
      value: connector.release.manifest.authorizationKind
    }
  ];
}

function implementationTags(connector: Connector): string[] {
  switch (connector.release.manifest.implementation.kind) {
    case "builtin":
      return ["BUILTIN"];
    case "managed_stdio":
      return ["STDIO"];
    case "remote_streamable_http":
      return ["HTTP"];
  }
}
