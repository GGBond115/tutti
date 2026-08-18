import type { AuthorizationViewEnvelopeV1 } from "@tutti-os/connector-authorization-protocol/v1";
import type {
  ConnectorOperationStage,
  ConnectorPresentationAction,
  ConnectorPresentationState
} from "../contracts/index.ts";

export type ConnectorRendererSegment = "available" | "installed";
export type ConnectorRendererInstallOutcome = "installed" | "not_admitted";

export interface ConnectorRendererCardView {
  action:
    | "authorize"
    | "cancel"
    | "details"
    | "disconnect"
    | "install"
    | "manage"
    | "unavailable"
    | "update";
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

export interface ConnectorRendererSectionView {
  id: string;
  displayNameZh?: string;
  displayNameEn?: string;
  connectorKeys: string[];
  error: boolean;
  hasMore: boolean;
  itemCount: number;
  loading: boolean;
}

interface ConnectorRendererDialogBase {
  connectorKey: string;
  description: string;
  displayName: string;
  iconUrl: string;
  permissions: ConnectorRendererPermissionView[];
}

export interface ConnectorRendererPermissionView {
  id: string;
  name: string;
}
export interface ConnectorRendererDetailFieldView {
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

export type ConnectorRendererDialogView =
  | (ConnectorRendererDialogBase & {
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
    })
  | (ConnectorRendererDialogBase & {
      installing: boolean;
      kind: "installation";
      updating: boolean;
    })
  | (ConnectorRendererDialogBase & {
      canDisconnect: boolean;
      canTry: boolean;
      canUninstall: boolean;
      details: ConnectorRendererDetailFieldView[];
      kind: "management";
    })
  | (ConnectorRendererDialogBase & { kind: "blocked"; reason: string })
  | (ConnectorRendererDialogBase & { kind: "uninstall_confirmation" });

export interface ConnectorRendererSurfaceSnapshot {
  readonly market: {
    readonly pendingUninstallNotificationsByOperationId: Readonly<
      Record<
        string,
        {
          connectorKey: string;
          displayName: string;
          operationId: string;
          state: string;
        }
      >
    >;
  };
  readonly ui: {
    readonly dialog: {
      connectorKey: string;
      kind: "connector" | "uninstall_confirmation";
    } | null;
    readonly query: string;
    readonly segment: ConnectorRendererSegment;
  };
  readonly view: {
    readonly availableCount: number;
    readonly cardsByKey: Readonly<Record<string, ConnectorRendererCardView>>;
    readonly catalogError: {
      kind: "invalid_data" | "unavailable" | "unknown";
      retryable: boolean;
    } | null;
    readonly dialog: ConnectorRendererDialogView | null;
    readonly installedCount: number;
    readonly refreshing: boolean;
    readonly sections: readonly ConnectorRendererSectionView[];
    readonly status: "empty" | "error" | "loading" | "ready";
  };
}
