package agent

import (
	"context"
	"errors"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"testing"
)

func TestValidatePromptConnectorsRequiresInstalledAuthorizedConnector(t *testing.T) {
	service := &Service{ConnectorMarketPolicy: connectorMarketSnapshotStub{
		snapshot: contracts.Snapshot{Connectors: []contracts.Connector{
			localConnectorFixture("lark-cli", contracts.InstallationStateInstalled, contracts.AuthorizationStateConnected, contracts.CompatibilityStateSupported),
			localConnectorFixture("notion", contracts.InstallationStateInstalled, contracts.AuthorizationStateDisconnected, contracts.CompatibilityStateSupported),
		}},
	}}
	if err := service.validatePromptConnectors(context.Background(), []PromptContentBlock{{
		Type: "connector", ConnectorKey: "lark-cli",
	}}); err != nil {
		t.Fatalf("validatePromptConnectors(lark-cli) error = %v", err)
	}
	if err := service.validatePromptConnectors(context.Background(), []PromptContentBlock{{
		Type: "connector", ConnectorKey: "notion",
	}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("validatePromptConnectors(notion) error = %v, want ErrInvalidArgument", err)
	}
	if err := service.validatePromptConnectors(context.Background(), []PromptContentBlock{{
		Type: "connector", ConnectorKey: "missing",
	}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("validatePromptConnectors(missing) error = %v, want ErrInvalidArgument", err)
	}
}

func TestValidatePromptConnectorsUsesCurrentAccountAuthorization(t *testing.T) {
	snapshots := &scopedConnectorMarketSnapshotStub{
		snapshot: contracts.Snapshot{Connectors: []contracts.Connector{
			localConnectorFixture("github", contracts.InstallationStateInstalled, contracts.AuthorizationStateDisconnected, contracts.CompatibilityStateSupported),
		}},
		scopedSnapshot: contracts.Snapshot{Connectors: []contracts.Connector{
			localConnectorFixture("github", contracts.InstallationStateInstalled, contracts.AuthorizationStateConnected, contracts.CompatibilityStateSupported),
		}},
	}
	service := &Service{
		ConnectorMarketPolicy: snapshots,
		ConnectorMarketCurrentScope: func() contracts.OperationScope {
			return contracts.OperationScope{AccountID: "account-1"}
		},
	}

	err := service.validatePromptConnectors(context.Background(), []PromptContentBlock{{
		Type: "connector", ConnectorKey: "github",
	}})
	if err != nil {
		t.Fatalf("validatePromptConnectors(github) error = %v", err)
	}
	if snapshots.requestedScope.AccountID != "account-1" {
		t.Fatalf("connector snapshot scope = %#v, want current account", snapshots.requestedScope)
	}
}

type connectorMarketSnapshotStub struct {
	snapshot contracts.Snapshot
}

func (stub connectorMarketSnapshotStub) Snapshot(context.Context) (contracts.Snapshot, error) {
	return stub.snapshot, nil
}

func (stub connectorMarketSnapshotStub) Evaluate(_ context.Context, target contracts.AgentTarget) (contracts.AgentConnectorPolicySnapshot, error) {
	return connectorPolicyTestSnapshot(target, stub.snapshot), nil
}

type scopedConnectorMarketSnapshotStub struct {
	snapshot       contracts.Snapshot
	scopedSnapshot contracts.Snapshot
	requestedScope contracts.OperationScope
}

func (stub *scopedConnectorMarketSnapshotStub) Snapshot(context.Context) (contracts.Snapshot, error) {
	return stub.snapshot, nil
}

func (stub *scopedConnectorMarketSnapshotStub) SnapshotForScope(_ context.Context, scope contracts.OperationScope) (contracts.Snapshot, error) {
	stub.requestedScope = scope
	return stub.scopedSnapshot, nil
}

func (stub *scopedConnectorMarketSnapshotStub) Evaluate(_ context.Context, target contracts.AgentTarget) (contracts.AgentConnectorPolicySnapshot, error) {
	stub.requestedScope = target.Scope
	return connectorPolicyTestSnapshot(target, stub.scopedSnapshot), nil
}

func connectorPolicyTestSnapshot(target contracts.AgentTarget, snapshot contracts.Snapshot) contracts.AgentConnectorPolicySnapshot {
	result := contracts.AgentConnectorPolicySnapshot{Target: target, CatalogFreshness: snapshot.CatalogFreshness, Revision: snapshot.Revision}
	for _, connector := range snapshot.Connectors {
		state := contracts.ConnectorStateAuthorizationRequired
		switch {
		case connector.Compatibility.State != contracts.CompatibilityStateSupported:
			state = contracts.ConnectorStateUnsupported
		case connector.Installation.State != contracts.InstallationStateInstalled:
			state = contracts.ConnectorStateSetupRequired
		case connector.Authorization.State == contracts.AuthorizationStateConnected || connector.Authorization.State == contracts.AuthorizationStateNotRequired:
			state = contracts.ConnectorStateConnected
		}
		result.Connectors = append(result.Connectors, contracts.AgentConnectorPolicy{
			Connector: connector, State: state, Supported: true, Granted: true, Selectable: state == contracts.ConnectorStateConnected,
		})
	}
	return result
}

func TestLocalConnectorCapabilityOptionsProjectsCatalogWithSetupState(t *testing.T) {
	options, err := localConnectorCapabilityOptions(context.Background(), connectorMarketSnapshotStub{
		snapshot: contracts.Snapshot{Connectors: []contracts.Connector{
			localConnectorFixture("github", contracts.InstallationStateInstalled, contracts.AuthorizationStateConnected, contracts.CompatibilityStateSupported),
			localConnectorFixture("notion", contracts.InstallationStateInstalled, contracts.AuthorizationStateDisconnected, contracts.CompatibilityStateSupported),
			localConnectorFixture("legacy", contracts.InstallationStateInstalled, contracts.AuthorizationStateConnected, contracts.CompatibilityStateUnsupportedVersion),
			localConnectorFixture("slack", contracts.InstallationStateNotInstalled, contracts.AuthorizationStateConnected, contracts.CompatibilityStateSupported),
			localConnectorFixture("lark-cli", contracts.InstallationStateFailed, contracts.AuthorizationStateDisconnected, contracts.CompatibilityStateSupported),
		}},
	}, nil, "local:codex")
	if err != nil {
		t.Fatalf("localConnectorCapabilityOptions() error = %v", err)
	}
	if len(options) != 5 {
		t.Fatalf("options = %#v, want all local catalog connectors", options)
	}
	if got := options[0]; got.ID != "connector:github" || got.Label != "GitHub" || got.IconURL != "data:image/png;base64,aWNvbg==" || got.Status != "available" || got.Trigger != "/github" || got.Invocation != "textTrigger" || got.Source != "local-db" {
		t.Fatalf("github option = %#v", got)
	}
	if got := options[1]; got.ID != "connector:notion" || got.Status != "authRequired" {
		t.Fatalf("notion option = %#v", got)
	}
	if got := options[2]; got.ID != "connector:legacy" || got.Status != "unsupported" {
		t.Fatalf("legacy option = %#v", got)
	}
	if got := options[3]; got.ID != "connector:slack" || got.Status != "setupRequired" {
		t.Fatalf("slack option = %#v", got)
	}
	if got := options[4]; got.ID != "connector:lark-cli" || got.Status != "setupRequired" {
		t.Fatalf("lark-cli option = %#v", got)
	}
}

func TestReplaceComposerConnectorCapabilitiesDropsProviderConnectors(t *testing.T) {
	result := replaceComposerConnectorCapabilities(
		[]ComposerCapabilityOption{
			{ID: "skill:review", Kind: "skill"},
			{ID: "connector:remote", Kind: "connector"},
		},
		[]ComposerCapabilityOption{{ID: "connector:local", Kind: "connector", Source: "local-db"}},
	)
	if len(result) != 2 || result[0].ID != "skill:review" || result[1].ID != "connector:local" {
		t.Fatalf("result = %#v, want non-connector capabilities plus local connector", result)
	}
}

func localConnectorFixture(
	key string,
	installation contracts.InstallationState,
	authorization contracts.AuthorizationState,
	compatibility contracts.CompatibilityState,
) contracts.Connector {
	label := key
	if key == "github" {
		label = "GitHub"
	}
	return contracts.Connector{
		Key: key,
		Release: contracts.Release{Manifest: contracts.Manifest{
			DisplayName: label,
			IconURL:     "data:image/png;base64,aWNvbg==",
			Description: key + " connector",
		}},
		Installation:  contracts.Installation{State: installation},
		Authorization: contracts.Authorization{State: authorization},
		Compatibility: contracts.Compatibility{State: compatibility},
	}
}
