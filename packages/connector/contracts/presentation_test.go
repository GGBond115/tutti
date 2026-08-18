package contracts

import "testing"

func TestConnectorPresentationAcceptsEveryClosedState(t *testing.T) {
	states := []ConnectorState{
		ConnectorStateUnavailable, ConnectorStateLoading, ConnectorStateSetupRequired,
		ConnectorStateAuthorizationRequired, ConnectorStateConnecting, ConnectorStateConnected,
		ConnectorStateDegraded, ConnectorStateDisabled, ConnectorStateUnsupported, ConnectorStateFailed,
	}
	for _, state := range states {
		actions := []ConnectorAction{ConnectorActionDetails, ConnectorActionRemoveSelection}
		reason := "test_reason"
		if state == ConnectorStateConnected {
			actions = append(actions, ConnectorActionSelect)
			reason = ""
		}
		if err := (ConnectorPresentation{State: state, ReasonCode: reason, AllowedActions: actions}).Validate(); err != nil {
			t.Fatalf("state %q: %v", state, err)
		}
	}
}

func TestConnectorPresentationRejectsUnknownDuplicateAndUnsafeActions(t *testing.T) {
	tests := []ConnectorPresentation{
		{State: "future", ReasonCode: "future", AllowedActions: []ConnectorAction{ConnectorActionDetails}},
		{State: ConnectorStateUnsupported, ReasonCode: "unknown", AllowedActions: []ConnectorAction{"retry"}},
		{State: ConnectorStateUnsupported, ReasonCode: "unknown", AllowedActions: []ConnectorAction{ConnectorActionDetails, ConnectorActionDetails}},
		{State: ConnectorStateDegraded, ReasonCode: "route_lost", AllowedActions: []ConnectorAction{ConnectorActionSelect}},
		{State: ConnectorStateConnected, AllowedActions: []ConnectorAction{ConnectorActionDetails}},
	}
	for index, value := range tests {
		if err := value.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly passed", index)
		}
	}
}
