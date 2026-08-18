package application

import (
	"context"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

type CatalogSource interface {
	FetchSnapshot(context.Context) (contracts.CatalogSnapshot, error)
}

// SharedAgentSupportSource supplies the server-owned declaration of Connector
// support for a shared Agent. Support is not an execution grant; unavailable,
// loading, or stale declarations are evaluated fail closed.
type SharedAgentSupportSource interface {
	SupportedConnectorKeys(context.Context, string) (contracts.SupportedConnectorSet, error)
}

type AgentConnectorGrantSource interface {
	GrantedConnectorKeys(context.Context, string, contracts.OperationScope) (contracts.AgentConnectorGrantSet, error)
}

// ArtifactDownloadResolver exchanges an immutable server-owned release digest
// for one short-lived, authenticated artifact download descriptor.
type ArtifactDownloadResolver interface {
	ResolveArtifactDownload(context.Context, string) (contracts.ArtifactDownload, error)
}

type Repository interface {
	Snapshot(ctx context.Context) (contracts.Snapshot, error)
	CatalogView(ctx context.Context) (contracts.CatalogView, error)
	BeginCatalogRefresh(ctx context.Context, now time.Time) (uint64, error)
	FailCatalogRefresh(ctx context.Context, generation uint64, failureCode string, now time.Time) error
	Connector(ctx context.Context, connectorKey string) (contracts.Connector, error)
	Operation(ctx context.Context, operationID string) (contracts.Operation, error)
	OperationForScope(ctx context.Context, scope contracts.OperationScope, operationID string) (contracts.Operation, error)
	// UnresolvedAuthorizationSessionOperations exposes private durable receipts
	// only for the explicitly active account. Snapshot remains safe for public
	// presentation and must not contain Operation.Execution.
	UnresolvedAuthorizationSessionOperations(ctx context.Context, scope contracts.OperationScope) ([]contracts.Operation, error)
	ResolveAuthorizationSession(ctx context.Context, operationID string, resolution contracts.AuthorizationSessionResolution) error
	ClaimOperation(ctx context.Context, operationID, owner string, now, leaseExpiresAt time.Time) (contracts.Operation, bool, error)
	RenewOperationLease(ctx context.Context, operationID, owner string, token uint64, now, leaseExpiresAt time.Time) error
	ReleaseOperationLease(ctx context.Context, operationID, owner string, token uint64) error
	Transaction(ctx context.Context, fn func(Transaction) error) error
	RecoverableOperations(ctx context.Context) ([]contracts.Operation, error)
	InstalledRelease(ctx context.Context, connectorKey, releaseDigest string) (contracts.Release, error)
	InstalledReleases(ctx context.Context, refs []contracts.InstalledReleaseRef) (map[contracts.InstalledReleaseRef]contracts.Release, error)
	RuntimeConvergence(ctx context.Context, scope contracts.OperationScope, connectorKey string) (contracts.RuntimeConvergence, error)
	RuntimeConvergences(ctx context.Context, scope contracts.OperationScope, limit int) ([]contracts.RuntimeConvergence, error)
	DueRuntimeConvergences(ctx context.Context, scope contracts.OperationScope, bootEpoch string, now time.Time, limit int) ([]contracts.RuntimeConvergence, error)
	ClaimRuntimeConvergence(ctx context.Context, scope contracts.OperationScope, connectorKey, bootEpoch, owner string, now, leaseExpiresAt time.Time) (contracts.RuntimeConvergence, bool, error)
	RenewRuntimeConvergenceLease(ctx context.Context, scope contracts.OperationScope, connectorKey, owner string, token uint64, now, leaseExpiresAt time.Time) error
	ReleaseRuntimeConvergenceLease(ctx context.Context, scope contracts.OperationScope, connectorKey, owner string, token uint64) error
	CompleteRuntimeConvergence(ctx context.Context, scope contracts.OperationScope, connectorKey, owner string, token, desiredGeneration uint64, observed contracts.RuntimeObserved, now time.Time) error
	RetryRuntimeConvergence(ctx context.Context, scope contracts.OperationScope, connectorKey, owner string, token, desiredGeneration uint64, nextAttemptAt time.Time, errorCode, errorMessage string, now time.Time) error
}

type Transaction interface {
	Revision() uint64
	AdvanceRevision() uint64
	Connectors() ([]contracts.Connector, error)
	Connector(connectorKey string) (contracts.Connector, error)
	Operation(operationID string) (contracts.Operation, error)
	OperationByClientRequestID(ownerAccountID, clientRequestID string) (*contracts.Operation, error)
	ActiveOperation(connectorKey string) (*contracts.Operation, error)
	CatalogFreshness() (contracts.CatalogFreshness, error)
	ReplaceCatalogSnapshot(generation uint64, snapshot contracts.CatalogSnapshot, acceptedAt time.Time) (bool, error)
	SaveConnector(contracts.Connector) error
	DeleteConnector(connectorKey string) error
	SaveOperation(contracts.Operation) error
	RuntimeConvergence(scope contracts.OperationScope, connectorKey string) (contracts.RuntimeConvergence, error)
	SaveRuntimeConvergence(contracts.RuntimeConvergence) error
	DeleteRuntimeConvergence(scope contracts.OperationScope, connectorKey string) error
	EnqueueConnectorMarketChanged(contracts.ChangedEvent) error
}

// ReleaseInstallationManager owns the complete physical release installation
// boundary. A same-machine host may compose artifact import and CLI package
// installation locally, while a remote host may download on the control-plane
// machine, transfer verified bytes, and ask the runtime machine to install them
// in one idempotent operation.
//
// Installation never implies capability publication. Runtime activation is a
// separate ImplementationCommands reconcile driven by authorization state.
type ReleaseInstallationManager interface {
	InstallRelease(ctx context.Context, request contracts.InstallReleaseRequest) (contracts.ReleaseInstallationReceipt, error)
	InspectReleaseInstallation(ctx context.Context, request contracts.InspectReleaseInstallationRequest) (contracts.ReleaseInstallationObservation, error)
	CommitReleaseInstallation(ctx context.Context, request contracts.CommitReleaseInstallationRequest) error
	UninstallRelease(ctx context.Context, request contracts.UninstallReleaseRequest) error
}

// ArtifactPreparer is the same-machine artifact import boundary used by the
// runtime package's ReleaseInstaller composition. service hosts depend on
// ReleaseInstallationManager instead of orchestrating this lower-level port.
type ArtifactPreparer interface {
	Prepare(ctx context.Context, request contracts.PrepareArtifactRequest) (contracts.PreparedArtifactReceipt, error)
	ResolvePrepared(ctx context.Context, release contracts.Release) (contracts.PreparedArtifactReceipt, error)
	Remove(ctx context.Context, request contracts.RemoveArtifactRequest) error
	RemoveConnector(ctx context.Context, request contracts.RemoveConnectorInstallationRequest) error
}

// CLIInstallationManager installs and resolves daemon-managed CLI packages.
// Implementations must bind installation and launch to the same managed
// runtime and keep package storage outside the user's global package manager.
type CLIInstallationManager interface {
	InstallCLI(ctx context.Context, request contracts.InstallCLIRequest) (contracts.CLIInstallationReceipt, error)
	ResolveCLI(ctx context.Context, release contracts.Release) (contracts.CLIInstallationReceipt, error)
	RemoveCLI(ctx context.Context, request contracts.RemoveCLIRequest) error
	RemoveConnector(ctx context.Context, request contracts.RemoveConnectorInstallationRequest) error
}

// ImplementationCommands is the narrow command side for physical Connector
// runtimes. It deliberately carries no observation or watch methods.
type ImplementationCommands interface {
	Reconcile(ctx context.Context, request contracts.RuntimeReconcileRequest) (contracts.RuntimeReceipt, error)
	DeactivateRuntime(ctx context.Context, request contracts.RuntimeDeactivationRequest) error
	// FailClosed stops all capability publication before best-effort fencing.
	FailClosed(ctx context.Context, deadline time.Time) error
	// Close permanently releases the implementation host. Callers always supply
	// a bounded lifecycle context; implementations must not invent an unbounded
	// shutdown path behind this port.
	Close(ctx context.Context) error
}

// RouteObservation is the narrow physical-state side of the runtime boundary.
// Snapshot is level-triggered truth; Watch only reduces repair latency.
type RouteObservation interface {
	Snapshot(context.Context) (contracts.PhysicalRouteSnapshot, error)
	Watch(context.Context) (contracts.PhysicalRouteWatch, error)
}

// RuntimeBindingResolver converts device installation plus an explicit
// operation scope into account-aware runtime intent. It is the only port that
// may obtain a short-lived credential grant; the service clears the grant
// after the ImplementationCommands call returns.
type RuntimeBindingResolver interface {
	ResolveRuntimeBinding(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error)
}

// RuntimeIntentResolver is the side-effect-free query counterpart of
// RuntimeBindingResolver. It must never mint or return credential grants.
type RuntimeIntentResolver interface {
	ResolveRuntimeIntent(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeIntent, error)
}

// AuthorizationProjectionStore keeps account authorization separate from the
// device-scoped Connector installation fact.
type AuthorizationProjectionStore interface {
	AuthorizationProjection(ctx context.Context, accountID, connectorKey string) (contracts.AuthorizationProjection, error)
	SaveAuthorizationProjection(ctx context.Context, projection contracts.AuthorizationProjection) error
}

type AuthorizationSnapshotStore interface {
	AuthorizationProjectionStore
	ApplyAuthorizationSnapshot(ctx context.Context, accountID string, snapshot contracts.AuthorizationSnapshot) (contracts.AuthorizationSnapshotApplyResult, error)
}

type AuthorizationSnapshotSource interface {
	AuthorizationSnapshot(ctx context.Context, accountID string) (contracts.AuthorizationSnapshot, error)
}

type AuthorizationEventSource interface {
	RunAuthorizationEvents(ctx context.Context, accountID string, notify func()) error
}

type CredentialBrokerGrantIssuer interface {
	IssueCredentialBrokerGrant(ctx context.Context, accountID, connectorKey, connectionID string) ([]byte, error)
}

type AuthorizationProvider interface {
	Begin(ctx context.Context, request contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error)
	Disconnect(ctx context.Context, request contracts.AuthorizationDisconnectRequest) error
}

// AuthorizationAttemptCanceler terminates one provider authorization attempt
// and returns only after it can no longer publish credentials or events. A
// provider that cannot make that guarantee must not implement this interface.
type AuthorizationAttemptCanceler interface {
	Cancel(ctx context.Context, request contracts.AuthorizationCancelRequest) error
}

// AuthorizationObserver is an optional asynchronous extension implemented by
// providers whose user interaction completes outside the daemon process.
type AuthorizationObserver interface {
	Observe(ctx context.Context, request contracts.AuthorizationObserveRequest) (contracts.AuthorizationObservation, error)
}

// AuthorizationInspector is the synchronous calibration boundary used by a
// runtime owner after boot, before reconcile, and after authorization errors.
type AuthorizationInspector interface {
	InspectAuthorization(ctx context.Context, request contracts.AuthorizationInspectRequest) (contracts.AuthorizationObservation, error)
}

type CompatibilityEvaluator interface {
	Evaluate(manifest contracts.Manifest) contracts.Compatibility
}

type OperationScheduler interface {
	Schedule(ctx context.Context, operationID string) error
}

// ChangedEventOutbox is a host persistence extension. Events are appended by
// Repository.Transaction and delivered after commit by a host-owned worker.
type ChangedEventOutbox interface {
	PendingChangedEvents(ctx context.Context, limit int) ([]contracts.ChangedEventRecord, error)
	MarkChangedEventPublished(ctx context.Context, sequence int64, publishedAt time.Time) error
}

// LifecycleCleanupStore removes only terminal operation results and events
// whose publication has already been recorded. Active operations and pending
// events are deliberately outside this contract so cleanup cannot weaken
// crash recovery or at-least-once event delivery.
type LifecycleCleanupStore interface {
	CleanupLifecycle(ctx context.Context, request contracts.LifecycleCleanupRequest) (contracts.LifecycleCleanupResult, error)
}
