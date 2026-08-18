package contracts

import (
	"fmt"
	"strings"
)

func IsConnectorState(value ConnectorState) bool {
	switch value {
	case ConnectorStateUnavailable, ConnectorStateLoading, ConnectorStateSetupRequired,
		ConnectorStateAuthorizationRequired, ConnectorStateConnecting, ConnectorStateConnected,
		ConnectorStateDegraded, ConnectorStateDisabled, ConnectorStateUnsupported, ConnectorStateFailed:
		return true
	default:
		return false
	}
}

func IsConnectorAction(value ConnectorAction) bool {
	switch value {
	case ConnectorActionDetails, ConnectorActionInstall, ConnectorActionUpdate,
		ConnectorActionAuthorize, ConnectorActionCancel, ConnectorActionSelect,
		ConnectorActionRemoveSelection, ConnectorActionManage, ConnectorActionDisconnect,
		ConnectorActionUninstall, ConnectorActionRetry:
		return true
	default:
		return false
	}
}

func (value ConnectorPresentation) Validate() error {
	if !IsConnectorState(value.State) {
		return fmt.Errorf("connector presentation state %q is invalid", value.State)
	}
	seen := make(map[ConnectorAction]struct{}, len(value.AllowedActions))
	for _, action := range value.AllowedActions {
		if !IsConnectorAction(action) {
			return fmt.Errorf("connector presentation action %q is invalid", action)
		}
		if _, exists := seen[action]; exists {
			return fmt.Errorf("connector presentation action %q is duplicated", action)
		}
		seen[action] = struct{}{}
		if action == ConnectorActionSelect && value.State != ConnectorStateConnected {
			return fmt.Errorf("connector presentation select action requires connected state")
		}
	}
	if value.State == ConnectorStateConnected {
		if _, selectable := seen[ConnectorActionSelect]; !selectable {
			return fmt.Errorf("connected connector presentation requires select action")
		}
	}
	if value.State != ConnectorStateConnected && strings.TrimSpace(value.ReasonCode) == "" {
		return fmt.Errorf("connector presentation reason code is required outside connected state")
	}
	return nil
}
