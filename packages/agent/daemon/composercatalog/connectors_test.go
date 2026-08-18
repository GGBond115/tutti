package composercatalog

import (
	"context"
	"errors"
	"testing"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func TestConnectorOptionsProjectsEveryCatalogState(t *testing.T) {
	source := snapshotStub{snapshot: contracts.AgentConnectorPolicySnapshot{Connectors: []contracts.AgentConnectorPolicy{
		{Connector: connectorFixture("github", "GitHub"), Presentation: contracts.ConnectorPresentation{State: contracts.ConnectorStateConnected}},
		{Connector: connectorFixture("notion", ""), Presentation: contracts.ConnectorPresentation{State: contracts.ConnectorStateAuthorizationRequired}},
		{Connector: connectorFixture("legacy", "Legacy"), Presentation: contracts.ConnectorPresentation{State: contracts.ConnectorStateUnsupported}},
		{Connector: connectorFixture("slack", "Slack"), Presentation: contracts.ConnectorPresentation{State: contracts.ConnectorStateSetupRequired}},
	}}}

	options, err := ConnectorOptions(context.Background(), source, contracts.AgentTarget{TargetID: "local:codex", Ownership: contracts.AgentOwnershipLocal})
	if err != nil {
		t.Fatalf("ConnectorOptions() error = %v", err)
	}
	if len(options) != 4 {
		t.Fatalf("options = %#v, want every catalog connector", options)
	}
	if got := options[0]; got.ID != "connector:github" || got.Label != "GitHub" || got.IconURL != "data:image/png;base64,aWNvbg==" || got.Status != CapabilityStatusAvailable || got.Trigger != "/github" || got.Invocation != CapabilityInvocationTextTrigger || got.Source != CapabilitySourceLocalDB {
		t.Fatalf("github option = %#v", got)
	}
	if got := options[1]; got.Label != "notion" || got.Status != CapabilityStatusAuthRequired {
		t.Fatalf("notion option = %#v", got)
	}
	if got := options[2]; got.Status != CapabilityStatusUnsupported {
		t.Fatalf("legacy option = %#v", got)
	}
	if got := options[3]; got.Status != CapabilityStatusSetupRequired {
		t.Fatalf("slack option = %#v", got)
	}
}

func TestConnectorOptionsPreservesSnapshotReadError(t *testing.T) {
	want := errors.New("snapshot unavailable")
	_, err := ConnectorOptions(context.Background(), snapshotStub{err: want}, contracts.AgentTarget{TargetID: "local:codex", Ownership: contracts.AgentOwnershipLocal})
	if !errors.Is(err, want) {
		t.Fatalf("ConnectorOptions() error = %v, want %v", err, want)
	}
}

type snapshotStub struct {
	snapshot contracts.AgentConnectorPolicySnapshot
	err      error
}

func (stub snapshotStub) Evaluate(context.Context, contracts.AgentTarget) (contracts.AgentConnectorPolicySnapshot, error) {
	return stub.snapshot, stub.err
}

func connectorFixture(
	key string,
	label string,
) contracts.Connector {
	return contracts.Connector{
		Key: key,
		Release: contracts.Release{Manifest: contracts.Manifest{
			DisplayName: label,
			IconURL:     "data:image/png;base64,aWNvbg==",
			Description: key + " connector",
		}},
	}
}
