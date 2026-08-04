package daemon

import (
	"context"
	"time"
)

type CatalogSource interface {
	Refresh(context.Context) (CatalogSnapshot, error)
}

type CatalogSnapshot struct {
	SourceRevision string
	Trust          CatalogTrustState
	Releases       []Release
	Statuses       []ReleaseCatalogStatus
}

type ReleaseCatalogStatus struct {
	ConnectorKey  string
	ReleaseDigest string
	Status        ReleaseStatus
}

type Repository interface {
	Snapshot(ctx context.Context, workspaceID string) (Snapshot, error)
	Connector(ctx context.Context, connectorKey, workspaceID string) (Connector, error)
	Operation(ctx context.Context, operationID string) (Operation, error)
	ClaimOperation(ctx context.Context, operationID, owner string, now, leaseExpiresAt time.Time) (Operation, bool, error)
	ReleaseOperationLease(ctx context.Context, operationID, owner string) error
	Transaction(ctx context.Context, fn func(Transaction) error) error
	RecoverableOperations(ctx context.Context) ([]Operation, error)
	WorkspaceBindings(ctx context.Context, connectorKey string) ([]WorkspaceBinding, error)
	CatalogTrustState(ctx context.Context) (CatalogTrustState, error)
}

type Transaction interface {
	Revision() uint64
	AdvanceRevision() uint64
	Connectors() ([]Connector, error)
	Connector(connectorKey string) (Connector, error)
	Operation(operationID string) (Operation, error)
	OperationByClientRequestID(clientRequestID string) (*Operation, error)
	ActiveOperation(connectorKey string) (*Operation, error)
	SaveCatalogRevision(sourceRevision string) error
	SaveCatalogTrustState(CatalogTrustState) error
	SetCatalogState(state CatalogState) error
	SaveConnector(Connector) error
	DeleteConnector(connectorKey string) error
	SaveOperation(Operation) error
	SetWorkspaceBinding(connectorKey string, binding WorkspaceBinding) (Connector, error)
	EnqueueConnectorMarketChanged(ChangedEvent) error
}

type ArtifactPreparer interface {
	Prepare(ctx context.Context, request PrepareArtifactRequest) (PreparedArtifactReceipt, error)
	Remove(ctx context.Context, request RemoveArtifactRequest) error
}

type PrepareArtifactRequest struct {
	OperationID string
	WorkspaceID string
	Release     Release
}

type RemoveArtifactRequest struct {
	OperationID   string
	ConnectorKey  string
	Version       string
	ReleaseDigest string
}

type RuntimeState string

const (
	RuntimeStateInactive RuntimeState = "inactive"
	RuntimeStateActive   RuntimeState = "active"
	RuntimeStateUnknown  RuntimeState = "unknown"
)

type RuntimeObservation struct {
	State         RuntimeState
	ReleaseDigest string
}

// ImplementationHost reconciles durable workspace intent into MCP routes and
// CLI registrations. Installing an artifact never starts a connector process.
type ImplementationHost interface {
	Reconcile(ctx context.Context, request WorkspaceReconcileRequest) (WorkspaceRuntimeReceipt, error)
	Revoke(ctx context.Context, request SecurityRevocationRequest) error
}

type WorkspaceReconcileRequest struct {
	OperationID string
	WorkspaceID string
	Connector   Connector
	Enabled     bool
	Generation  HostGeneration
}

type SecurityRevocationRequest struct {
	WorkspaceID   string
	ConnectorKey  string
	ReleaseDigest string
	Generation    HostGeneration
	Deadline      time.Time
}

type RuntimeObserveRequest struct {
	ConnectorKey string
}

type RuntimeActivationRequest struct {
	OperationID string
	Release     Release
	Prepared    PreparedArtifactReceipt
}

type RuntimeDeactivationRequest struct {
	OperationID   string
	ConnectorKey  string
	Version       string
	ReleaseID     string
	ReleaseDigest string
}

type AuthorizationProvider interface {
	Begin(ctx context.Context, request AuthorizationStartRequest) (AuthorizationSession, error)
	Disconnect(ctx context.Context, request AuthorizationDisconnectRequest) error
}

type AuthorizationStartRequest struct {
	OperationID     string
	ClientRequestID string
	Connector       Connector
	Release         Release
}

type AuthorizationDisconnectRequest struct {
	OperationID string
	Connector   Connector
}

type CompatibilityEvaluator interface {
	Evaluate(manifest Manifest) Compatibility
}

type OperationScheduler interface {
	Schedule(ctx context.Context, operationID string) error
}

type ChangedEvent struct {
	ConnectorKey string `json:"connectorKey,omitempty"`
	OperationID  string `json:"operationId,omitempty"`
	Revision     uint64 `json:"revision"`
}

type ChangedEventRecord struct {
	Sequence int64
	Event    ChangedEvent
}

// ChangedEventOutbox is a host persistence extension. Events are appended by
// Repository.Transaction and delivered after commit by a host-owned worker.
type ChangedEventOutbox interface {
	PendingChangedEvents(ctx context.Context, limit int) ([]ChangedEventRecord, error)
	MarkChangedEventPublished(ctx context.Context, sequence int64, publishedAt time.Time) error
}
