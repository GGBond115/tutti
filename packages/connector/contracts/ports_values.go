package contracts

import (
	"errors"
	"time"
)

var (
	ErrReleaseInstallationAbsent  = errors.New("connector release installation is absent")
	ErrReleaseInstallationInvalid = errors.New("connector release installation is invalid")
)

type ArtifactDownload struct {
	URL           string
	ExpiresAt     time.Time
	ReleaseDigest string
	SHA256        string
	SizeBytes     int64
	MediaType     string
}

type CatalogInstallationFilter string

const (
	CatalogInstallationFilterNotInstalled CatalogInstallationFilter = "not_installed"
)

type CatalogPageQuery struct {
	SectionID          string
	PageSize           int
	PageToken          string
	InstallationFilter CatalogInstallationFilter
}

type CatalogCategory struct {
	CategoryID    string `json:"categoryId"`
	Kind          string `json:"kind"`
	SortOrder     int32  `json:"sortOrder"`
	ItemCount     int64  `json:"itemCount"`
	DisplayNameZH string `json:"displayNameZh,omitempty"`
	DisplayNameEN string `json:"displayNameEn,omitempty"`
}

type CatalogEntry struct {
	SectionID  string  `json:"sectionId"`
	CategoryID string  `json:"categoryId"`
	Featured   bool    `json:"featured"`
	Order      int     `json:"order"`
	Release    Release `json:"release"`
}

type CatalogListing struct {
	CategoryID string    `json:"categoryId"`
	Featured   bool      `json:"featured"`
	Connector  Connector `json:"connector"`
}

type CatalogPage struct {
	SectionID     string           `json:"sectionId"`
	Items         []CatalogListing `json:"items"`
	NextPageToken string           `json:"nextPageToken,omitempty"`
	Revision      uint64           `json:"revision"`
}

type CatalogSnapshot struct {
	SourceRevision string
	Categories     []CatalogCategory
	Entries        []CatalogEntry
}

type CatalogView struct {
	Freshness         CatalogFreshness
	Categories        []CatalogCategory
	ListingsBySection map[string][]CatalogListing
	Revision          uint64
}

type InstalledReleaseRef struct {
	ConnectorKey  string
	ReleaseDigest string
}

type InstallReleaseRequest struct {
	OperationID string
	Scope       OperationScope
	Generation  HostGeneration
	Release     Release
}

type UninstallReleaseRequest struct {
	OperationID string
	Scope       OperationScope
	Generation  HostGeneration
	Release     Release
}

type InspectReleaseInstallationRequest struct {
	OperationID string
	Scope       OperationScope
	Generation  HostGeneration
	Release     Release
}

type ReleaseInstallationObservationState string

const (
	ReleaseInstallationPresent       ReleaseInstallationObservationState = "present"
	ReleaseInstallationAbsent        ReleaseInstallationObservationState = "absent"
	ReleaseInstallationInvalid       ReleaseInstallationObservationState = "invalid"
	ReleaseInstallationIndeterminate ReleaseInstallationObservationState = "indeterminate"
)

type ReleaseInstallationObservation struct {
	State         ReleaseInstallationObservationState `json:"state"`
	ConnectorKey  string                              `json:"connectorKey"`
	ReleaseDigest string                              `json:"releaseDigest"`
	ReasonCode    string                              `json:"reasonCode,omitempty"`
	Receipt       *ReleaseInstallationReceipt         `json:"receipt,omitempty"`
}

// CommitReleaseInstallation is invoked only after installed truth is durable
// in the business repository. Cross-machine hosts use it to promote a cached
// candidate to current; same-machine installers may implement it as a no-op.
type CommitReleaseInstallationRequest struct {
	OperationID string
	Scope       OperationScope
	Generation  HostGeneration
	Release     Release
	Receipt     ReleaseInstallationReceipt
}

// RemoveConnectorInstallationRequest identifies an explicit Connector
// uninstall. Unlike the release-scoped removal requests used by installation
// rollback, this request removes every locally retained release for the
// Connector while preserving storage shared by other Connectors.
type RemoveConnectorInstallationRequest struct {
	OperationID  string
	Scope        OperationScope
	Generation   HostGeneration
	ConnectorKey string
}

type InstallCLIRequest struct {
	OperationID string
	Scope       OperationScope
	Generation  HostGeneration
	Release     Release
}

type RemoveCLIRequest struct {
	OperationID   string
	Scope         OperationScope
	Generation    HostGeneration
	ConnectorKey  string
	ReleaseDigest string
}

type PrepareArtifactRequest struct {
	OperationID string
	Scope       OperationScope
	Generation  HostGeneration
	Release     Release
}

type RemoveArtifactRequest struct {
	OperationID   string
	Scope         OperationScope
	Generation    HostGeneration
	ConnectorKey  string
	Version       string
	ReleaseDigest string
}

type PhysicalRouteState string

const (
	PhysicalRouteStateReady    PhysicalRouteState = "ready"
	PhysicalRouteStateDegraded PhysicalRouteState = "degraded"
)

// PhysicalRoute is non-secret runtime truth for one currently owned logical
// route. CLI-only and remote routes are included even when they have no local
// long-lived process.
type PhysicalRoute struct {
	ConnectorKey  string
	ConnectionID  string
	ReleaseDigest string
	Generation    HostGeneration
	State         PhysicalRouteState
}

type PhysicalRouteSnapshot struct {
	// Revision and Routes describe the same linearization point. A producer
	// must not return a revision whose event is absent from Routes.
	Revision uint64
	Routes   []PhysicalRoute
}

type PhysicalRouteEventKind string

const (
	PhysicalRouteEventChanged        PhysicalRouteEventKind = "changed"
	PhysicalRouteEventUnexpectedExit PhysicalRouteEventKind = "unexpected_exit"
)

type PhysicalRouteEvent struct {
	Revision uint64
	Kind     PhysicalRouteEventKind
	Route    PhysicalRoute
}

// PhysicalRouteWatch starts at Revision. Every delivered event must advance it
// by exactly one. A closed Events channel is a watch failure and requires a
// fresh Snapshot before trusting cached physical state again.
type PhysicalRouteWatch struct {
	Revision uint64
	Events   <-chan PhysicalRouteEvent
}

type RuntimeReconcileRequest struct {
	OperationID  string
	Scope        OperationScope
	ConnectionID string
	Connector    Connector
	Enabled      bool
	Generation   HostGeneration
	// CredentialBrokerGrant is a one-shot authority passed directly to the
	// runtime adapter. Implementations must not log or persist it.
	CredentialBrokerGrant []byte
}

type RuntimeDeactivationRequest struct {
	Scope         OperationScope
	ConnectionID  string
	ConnectorKey  string
	ReleaseDigest string
	// AllConnections fences every local route for this Connector, including
	// routes for superseded releases. Device uninstall uses this because an
	// authorization provider may rotate the connection identity after a route was
	// published, and a failed earlier retirement may retain an older release.
	AllConnections bool
	Generation     HostGeneration
	Deadline       time.Time
}

type RuntimeBindingRequest struct {
	OperationID string
	Scope       OperationScope
	Purpose     RuntimeBindingPurpose
	Connector   Connector
	Release     Release
}

type RuntimeBindingPurpose string

const (
	RuntimeBindingPurposePlan       RuntimeBindingPurpose = "plan"
	RuntimeBindingPurposeReconcile  RuntimeBindingPurpose = "reconcile"
	RuntimeBindingPurposeDeactivate RuntimeBindingPurpose = "deactivate"
)

type RuntimeBinding struct {
	ConnectionID          string
	Enabled               bool
	AuthorizationState    AuthorizationState
	CredentialBrokerGrant []byte
}

type RuntimeIntent struct {
	ConnectionID       string
	Enabled            bool
	AuthorizationState AuthorizationState
}

type AuthorizationInspectRequest struct {
	Scope                   OperationScope
	Connector               Connector
	AccountGeneration       uint64
	VMAssignmentID          string
	AuthorizationSessionID  string
	AuthorizationGeneration uint64
	DesktopBootEpoch        string
	GuestBootID             string
	RuntimeEpoch            string
	StateRevision           uint64
}

type AuthorizationStartRequest struct {
	OperationID       string
	ClientRequestID   string
	ReplacementPolicy AuthorizationReplacementPolicy
	Scope             OperationScope
	Connector         Connector
	Release           Release
	Secret            []byte
}

type AuthorizationCancelRequest struct {
	OperationID string
	Scope       OperationScope
	Connector   Connector
	Release     Release
	Session     AuthorizationSession
}

type AuthorizationDisconnectRequest struct {
	OperationID string
	Scope       OperationScope
	Connector   Connector
	Release     Release
}

type AuthorizationObserveRequest struct {
	Scope     OperationScope
	Connector Connector
	Release   Release
	Session   AuthorizationSession
}

type ChangedEvent struct {
	ConnectorKey   string              `json:"connectorKey,omitempty"`
	OperationID    string              `json:"operationId,omitempty"`
	OwnerAccountID string              `json:"ownerAccountId,omitempty"`
	Visibility     OperationVisibility `json:"visibility,omitempty"`
	Revision       uint64              `json:"revision"`
	Cursor         int64               `json:"cursor,omitempty"`
}

type ChangedEventRecord struct {
	Sequence int64
	Event    ChangedEvent
}

type LifecycleCleanupRequest struct {
	TerminalOperationsUpdatedThrough time.Time
	PublishedEventsPublishedThrough  time.Time
	BatchSize                        int
}

type LifecycleCleanupResult struct {
	TerminalOperationsDeleted int64
	PublishedEventsDeleted    int64
}
