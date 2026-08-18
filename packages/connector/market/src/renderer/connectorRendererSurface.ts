import type { AuthorizationViewEnvelopeV1 } from "@tutti-os/connector-authorization-protocol/v1";
import type {
  ConnectorAuthorizationState,
  ConnectorCompatibilityState,
  ConnectorInstallationState,
  ConnectorOperationStage
} from "../contracts/index.ts";

export type ConnectorRendererSegment = "available" | "installed";
export type ConnectorRendererInstallOutcome = "installed" | "not_admitted";

export interface ConnectorRendererCardView {
  action:
    | "authorize"
    | "busy"
    | "disconnect"
    | "install"
    | "manage"
    | "unavailable"
    | "update";
  authorizationState: ConnectorAuthorizationState;
  compatibilityState: ConnectorCompatibilityState;
  connectorKey: string;
  description: string;
  displayName: string;
  iconUrl: string;
  implementationTags: string[];
  installationState: ConnectorInstallationState;
  operationStage: ConnectorOperationStage | null;
  canUninstall: boolean;
  status:
    | "authorization_required"
    | "connected"
    | "installing"
    | "not_installed"
    | "unavailable"
    | "updating"
    | "update_available";
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
      kind: "authorization";
      pending: boolean;
    })
  | (ConnectorRendererDialogBase & {
      installing: boolean;
      kind: "installation";
      updating: boolean;
    })
  | (ConnectorRendererDialogBase & {
      canAuthorize: boolean;
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
