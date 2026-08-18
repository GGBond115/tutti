import type {
  ConnectorOperationStage,
  ConnectorPresentationAction,
  ConnectorPresentationState
} from "../../contracts/index.ts";
import type { AuthorizationViewEnvelopeV1 } from "@tutti-os/connector-authorization-protocol/v1";

export type ConnectorMarketViewStatus = "empty" | "error" | "loading" | "ready";

export interface ConnectorCatalogErrorView {
  kind: "invalid_data" | "unavailable" | "unknown";
  retryable: boolean;
}

export type ConnectorCardAction =
  | "authorize"
  | "cancel"
  | "details"
  | "disconnect"
  | "install"
  | "manage"
  | "unavailable"
  | "update";

export interface ConnectorCardView {
  action: ConnectorCardAction;
  allowedActions: ConnectorPresentationAction[];
  connectorKey: string;
  description: string;
  displayName: string;
  iconUrl: string;
  implementationTags: string[];
  operationStage: ConnectorOperationStage | null;
  reasonCode?: string;
  canUninstall: boolean;
  status: ConnectorPresentationState;
}

export interface ConnectorSectionView {
  id: string;
  displayNameZh?: string;
  displayNameEn?: string;
  connectorKeys: string[];
  error: boolean;
  hasMore: boolean;
  itemCount: number;
  loading: boolean;
}

export interface ConnectorPermissionView {
  id: string;
  name: string;
}

export interface ConnectorDetailFieldView {
  id:
    | "authorization"
    | "compatibility"
    | "implementation"
    | "releaseStatus"
    | "runtime"
    | "transport"
    | "version";
  value: string;
}

interface ConnectorDialogBaseView {
  connectorKey: string;
  description: string;
  displayName: string;
  iconUrl: string;
  permissions: ConnectorPermissionView[];
}

export interface ConnectorAuthorizationDialogView extends ConnectorDialogBaseView {
  authorizationInteraction?: unknown;
  authorizationKind: string;
  authorizationQrCodeDataUrl?: string;
  authorizationView?: AuthorizationViewEnvelopeV1;
  authorizing: boolean;
  brokeredAuthorization: boolean;
  canAuthorize: boolean;
  canCancel: boolean;
  kind: "authorization";
  pending: boolean;
}

export interface ConnectorInstallationDialogView extends ConnectorDialogBaseView {
  installing: boolean;
  kind: "installation";
  updating: boolean;
}

export interface ConnectorManagementDialogView extends ConnectorDialogBaseView {
  canDisconnect: boolean;
  canTry: boolean;
  canUninstall: boolean;
  details: ConnectorDetailFieldView[];
  kind: "management";
}

export interface ConnectorBlockedDialogView extends ConnectorDialogBaseView {
  kind: "blocked";
  reason: string;
}

export interface ConnectorUninstallConfirmationDialogView extends ConnectorDialogBaseView {
  kind: "uninstall_confirmation";
}

export type ConnectorDialogView =
  | ConnectorAuthorizationDialogView
  | ConnectorBlockedDialogView
  | ConnectorInstallationDialogView
  | ConnectorManagementDialogView
  | ConnectorUninstallConfirmationDialogView;

export interface ConnectorMarketViewState {
  availableCount: number;
  cardsByKey: Record<string, ConnectorCardView>;
  catalogError: ConnectorCatalogErrorView | null;
  dialog: ConnectorDialogView | null;
  installedCount: number;
  refreshing: boolean;
  sections: ConnectorSectionView[];
  status: ConnectorMarketViewStatus;
}
