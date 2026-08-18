export type ConnectorInstallationState =
  | "not_installed"
  | "installing"
  | "installed"
  | "updating"
  | "uninstalling"
  | "failed";

export type ConnectorAuthorizationState =
  | "not_required"
  | "disconnected"
  | "pending"
  | "connected"
  | "expired"
  | "failed";

export type ConnectorCompatibilityState =
  | "supported"
  | "unsupported_product"
  | "unsupported_platform"
  | "unsupported_version"
  | "unsupported_implementation";

export type ConnectorPresentationState =
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

export type ConnectorPresentationAction =
  | "details"
  | "install"
  | "update"
  | "authorize"
  | "cancel"
  | "select"
  | "remove_selection"
  | "manage"
  | "disconnect"
  | "uninstall";

export interface ConnectorPresentation {
  state: ConnectorPresentationState;
  reasonCode?: string;
  allowedActions: ConnectorPresentationAction[];
}

export interface ConnectorCatalogFreshness {
  state: "unavailable" | "refreshing" | "fresh" | "stale";
  snapshotId?: string;
  sourceRevision?: string;
  acceptedAt?: string;
  staleSince?: string;
  lastFailure?: string;
}

export type ConnectorOperationKind =
  | "refresh_catalog"
  | "install"
  | "uninstall"
  | "start_authorization"
  | "disconnect_authorization";

export type ConnectorOperationState =
  | "accepted"
  | "running"
  | "completed"
  | "failed";

export type ConnectorOperationStage =
  | "accepted"
  | "refreshing"
  | "installing"
  | "installed"
  | "runtime_pending"
  | "deactivating"
  | "removing"
  | "authorizing"
  | "disconnecting"
  | "completed"
  | "failed";

export interface ConnectorManifestImplementation {
  kind: "builtin" | "managed_stdio" | "remote_streamable_http";
}

export interface ConnectorReleaseArtifact {
  sha256: string;
  sizeBytes: number;
  mediaType: string;
}

export interface ConnectorCompatibilityRequirements {
  products?: string[];
  platforms?: string[];
  minimumHostVersion?: string;
}

export interface ConnectorAgentRouting {
  aliases: string[];
}

export interface ConnectorManifest {
  schemaVersion: "1";
  displayName: string;
  iconUrl: string;
  description?: string;
  agentRouting?: ConnectorAgentRouting;
  permissions: string[];
  implementation: ConnectorManifestImplementation;
  authorizationKind: string;
  authorizationInteraction?: unknown;
  authorizationInteractionMode?: "managed";
  compatibility?: ConnectorCompatibilityRequirements;
}

export interface ConnectorRelease {
  schemaVersion: "1";
  releaseId: string;
  connectorKey: string;
  version: string;
  releaseDigest: string;
  manifestDigest: string;
  manifest: ConnectorManifest;
  artifact: ConnectorReleaseArtifact;
  publishedAt: string;
  status: "available" | "superseded";
}

export interface ConnectorInstallation {
  state: ConnectorInstallationState;
  installedVersion?: string;
  installedReleaseId?: string;
  installedReleaseDigest?: string;
  failureCode?: string;
}

export interface ConnectorAuthorization {
  state: ConnectorAuthorizationState;
  failureCode?: string;
}

export interface ConnectorCompatibility {
  state: ConnectorCompatibilityState;
  reason?: string;
}

export interface Connector {
  key: string;
  release: ConnectorRelease;
  installation: ConnectorInstallation;
  authorization: ConnectorAuthorization;
  compatibility: ConnectorCompatibility;
  presentation: ConnectorPresentation;
  revision: number;
}

export interface ConnectorOperation {
  operationId: string;
  clientRequestId: string;
  connectorKey?: string;
  kind: ConnectorOperationKind;
  state: ConnectorOperationState;
  stage?: ConnectorOperationStage;
  target?: ConnectorOperationTarget;
  attempt: number;
  failureCode?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ConnectorOperationTarget {
  connectorKey: string;
  version: string;
  releaseId: string;
  releaseDigest: string;
  artifactSha256?: string;
}

export interface ConnectorMarketSnapshot {
  catalogFreshness: ConnectorCatalogFreshness;
  connectors: Connector[];
  operations: ConnectorOperation[];
  revision: number;
  eventCursor: number;
}

export type ConnectorMarketCategoryKind = "category" | "featured";

export interface ConnectorMarketCategory {
  categoryId: string;
  kind: ConnectorMarketCategoryKind;
  sortOrder: number;
  itemCount: number;
  displayNameZh?: string;
  displayNameEn?: string;
}

export interface ConnectorMarketCatalogItem {
  categoryId: string;
  featured: boolean;
  connector: Connector;
}

export interface ConnectorMarketCatalogPage {
  sectionId: string;
  items: ConnectorMarketCatalogItem[];
  nextPageToken?: string;
  revision: number;
}

export interface ConnectorMarketMutationInput {
  clientRequestId: string;
  expectedRevision: number;
}

export interface ConnectorMutationInput extends ConnectorMarketMutationInput {
  connectorKey: string;
  expectedConnectorRevision: number;
}

export interface ConnectorAuthorizationInput extends ConnectorMutationInput {
  replacementPolicy?: "replace_active";
  secret?: string;
}

export type ConnectorCommandOutcome =
  | "accepted"
  | "completed"
  | "rejected"
  | "uncertain";

export interface ConnectorCommandFailure {
  code: string;
  message: string;
  retryable: boolean;
}

export interface ConnectorMutationResult {
  outcome: ConnectorCommandOutcome;
  connector?: Connector;
  operation?: ConnectorOperation;
  failure?: ConnectorCommandFailure;
  revision: number;
}

export interface ConnectorAuthorizationResult extends ConnectorMutationResult {
  authorizationUrl?: string;
  authorizationExpiresAt?: string;
  authorizationView?: unknown;
}

export interface ConnectorAuthorizationCancelInput extends ConnectorMutationInput {
  operationId: string;
}

export interface ConnectorMarketChangedEvent {
  type: "connector.market.changed";
  revision: number;
  cursor?: number;
  connectorKey?: string;
  operationId?: string;
}

export interface ConnectorMarketErrorShape {
  code: string;
  message: string;
  retryable: boolean;
}
