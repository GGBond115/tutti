package contracts

import "time"

type AgentOwnership string

const (
	AgentOwnershipLocal  AgentOwnership = "local"
	AgentOwnershipShared AgentOwnership = "shared"
)

type AgentTarget struct {
	TargetID  string         `json:"targetId"`
	Ownership AgentOwnership `json:"ownership"`
	Scope     OperationScope `json:"scope,omitempty"`
}

type SupportedConnectorSetState string

const (
	SupportedConnectorSetReady       SupportedConnectorSetState = "ready"
	SupportedConnectorSetLoading     SupportedConnectorSetState = "loading"
	SupportedConnectorSetUnavailable SupportedConnectorSetState = "unavailable"
	SupportedConnectorSetStale       SupportedConnectorSetState = "stale"
)

type SupportedConnectorSet struct {
	State     SupportedConnectorSetState `json:"state"`
	Keys      []string                   `json:"keys"`
	UpdatedAt *time.Time                 `json:"updatedAt,omitempty"`
}

type AgentConnectorGrantSet struct {
	State     SupportedConnectorSetState `json:"state"`
	Keys      []string                   `json:"keys"`
	UpdatedAt *time.Time                 `json:"updatedAt,omitempty"`
}

type ConnectorState string

const (
	ConnectorStateUnavailable           ConnectorState = "unavailable"
	ConnectorStateLoading               ConnectorState = "loading"
	ConnectorStateSetupRequired         ConnectorState = "setup_required"
	ConnectorStateAuthorizationRequired ConnectorState = "authorization_required"
	ConnectorStateConnecting            ConnectorState = "connecting"
	ConnectorStateConnected             ConnectorState = "connected"
	ConnectorStateDegraded              ConnectorState = "degraded"
	ConnectorStateDisabled              ConnectorState = "disabled"
	ConnectorStateUnsupported           ConnectorState = "unsupported"
	ConnectorStateFailed                ConnectorState = "failed"
)

// ConnectorAction is a closed semantic action understood by Connector-owned
// renderers. It describes application admission, not a host navigation target.
type ConnectorAction string

const (
	ConnectorActionDetails         ConnectorAction = "details"
	ConnectorActionInstall         ConnectorAction = "install"
	ConnectorActionUpdate          ConnectorAction = "update"
	ConnectorActionAuthorize       ConnectorAction = "authorize"
	ConnectorActionCancel          ConnectorAction = "cancel"
	ConnectorActionSelect          ConnectorAction = "select"
	ConnectorActionRemoveSelection ConnectorAction = "remove_selection"
	ConnectorActionManage          ConnectorAction = "manage"
	ConnectorActionDisconnect      ConnectorAction = "disconnect"
	ConnectorActionUninstall       ConnectorAction = "uninstall"
	ConnectorActionRetry           ConnectorAction = "retry"
)

// ConnectorPresentation is the application-owned display and interaction
// projection. It is intentionally separate from Connector because it depends
// on account, Agent policy, runtime observation, and catalog freshness and
// therefore must never be persisted as an entity fact.
type ConnectorPresentation struct {
	State          ConnectorState    `json:"state"`
	ReasonCode     string            `json:"reasonCode,omitempty"`
	AllowedActions []ConnectorAction `json:"allowedActions"`
}

// ConnectorView combines a durable Connector entity with its ephemeral,
// scope-aware application projection. The anonymous embedding keeps the HTTP
// representation flat without adding presentation to the persisted entity.
type ConnectorView struct {
	Connector
	Presentation ConnectorPresentation `json:"presentation"`
}

type AgentConnectorPolicy struct {
	Connector    Connector             `json:"connector"`
	Presentation ConnectorPresentation `json:"presentation"`
	Supported    bool                  `json:"supported"`
	Granted      bool                  `json:"granted"`
}

type AgentConnectorPolicySnapshot struct {
	Target           AgentTarget                `json:"target"`
	SupportState     SupportedConnectorSetState `json:"supportState"`
	GrantState       SupportedConnectorSetState `json:"grantState"`
	CatalogFreshness CatalogFreshness           `json:"catalogFreshness"`
	Connectors       []AgentConnectorPolicy     `json:"connectors"`
	Revision         uint64                     `json:"revision"`
}
