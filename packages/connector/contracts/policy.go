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

type AgentConnectorPolicy struct {
	Connector  Connector      `json:"connector"`
	State      ConnectorState `json:"state"`
	Supported  bool           `json:"supported"`
	Granted    bool           `json:"granted"`
	Selectable bool           `json:"selectable"`
	ReasonCode string         `json:"reasonCode,omitempty"`
}

type AgentConnectorPolicySnapshot struct {
	Target           AgentTarget                `json:"target"`
	SupportState     SupportedConnectorSetState `json:"supportState"`
	GrantState       SupportedConnectorSetState `json:"grantState"`
	CatalogFreshness CatalogFreshness           `json:"catalogFreshness"`
	Connectors       []AgentConnectorPolicy     `json:"connectors"`
	Revision         uint64                     `json:"revision"`
}
