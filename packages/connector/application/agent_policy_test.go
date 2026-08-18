package application

import (
	"context"
	"testing"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

type sharedAgentSupportStub struct {
	set contracts.SupportedConnectorSet
}

func (stub sharedAgentSupportStub) SupportedConnectorKeys(context.Context, string) (contracts.SupportedConnectorSet, error) {
	return stub.set, nil
}

type agentConnectorGrantStub struct {
	set contracts.AgentConnectorGrantSet
}

func (stub agentConnectorGrantStub) GrantedConnectorKeys(context.Context, string, contracts.OperationScope) (contracts.AgentConnectorGrantSet, error) {
	return stub.set, nil
}

func TestAgentPolicyRequiresExactCurrentRuntimeObservation(t *testing.T) {
	application, repository, connector := newAgentPolicyFixture(t)
	target := contracts.AgentTarget{TargetID: "local:codex", Ownership: contracts.AgentOwnershipLocal}

	policy, err := application.Evaluate(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Connectors) != 1 || policy.Connectors[0].State != contracts.ConnectorStateConnected || !policy.Connectors[0].Selectable {
		t.Fatalf("policy = %#v", policy)
	}

	convergence := repository.runtimeConvergences[memoryRuntimeConvergenceKey(target.Scope, connector.Key)]
	convergence.Desired.ReleaseDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	repository.runtimeConvergences[memoryRuntimeConvergenceKey(target.Scope, connector.Key)] = convergence
	policy, err = application.Evaluate(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Connectors[0].State != contracts.ConnectorStateConnecting || policy.Connectors[0].Selectable {
		t.Fatalf("stale desired policy = %#v", policy.Connectors[0])
	}
}

func TestAgentPolicyBatchesRuntimeAndInstalledReleaseReads(t *testing.T) {
	application, repository, _ := newAgentPolicyFixture(t)
	_, err := application.Evaluate(context.Background(), contracts.AgentTarget{
		TargetID: "local:codex", Ownership: contracts.AgentOwnershipLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.runtimeConvergencesCalls != 1 || repository.installedReleasesCalls != 1 || repository.runtimeConvergenceCalls != 0 {
		t.Fatalf("repository reads = convergence batch %d, installed batch %d, per-connector %d",
			repository.runtimeConvergencesCalls, repository.installedReleasesCalls, repository.runtimeConvergenceCalls)
	}
}

func TestAgentPolicySeparatesSharedSupportFromGrant(t *testing.T) {
	application, _, connector := newAgentPolicyFixture(t)
	application.config.SharedAgentSupport = sharedAgentSupportStub{set: contracts.SupportedConnectorSet{
		State: contracts.SupportedConnectorSetReady, Keys: []string{connector.Key},
	}}
	application.config.AgentConnectorGrants = agentConnectorGrantStub{set: contracts.AgentConnectorGrantSet{
		State: contracts.SupportedConnectorSetReady,
	}}
	policy, err := application.Evaluate(context.Background(), contracts.AgentTarget{
		TargetID: "shared:agent-1", Ownership: contracts.AgentOwnershipShared,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Connectors) != 1 || !policy.Connectors[0].Supported || policy.Connectors[0].Granted ||
		policy.Connectors[0].State != contracts.ConnectorStateDisabled || policy.Connectors[0].Selectable {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestAgentPolicySharedDeclarationAndActiveCatalogFailClosed(t *testing.T) {
	application, repository, connector := newAgentPolicyFixture(t)
	application.config.SharedAgentSupport = sharedAgentSupportStub{set: contracts.SupportedConnectorSet{
		State: contracts.SupportedConnectorSetReady, Keys: []string{"another-connector"},
	}}
	application.config.AgentConnectorGrants = agentConnectorGrantStub{set: contracts.AgentConnectorGrantSet{
		State: contracts.SupportedConnectorSetReady, Keys: []string{connector.Key},
	}}
	policy, err := application.Evaluate(context.Background(), contracts.AgentTarget{
		TargetID: "shared:agent-1", Ownership: contracts.AgentOwnershipShared,
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Connectors[0].State != contracts.ConnectorStateUnsupported || policy.Connectors[0].Supported {
		t.Fatalf("undeclared policy = %#v", policy.Connectors[0])
	}

	repository.catalogView.ListingsBySection = map[string][]contracts.CatalogListing{}
	policy, err = application.Evaluate(context.Background(), contracts.AgentTarget{
		TargetID: "local:codex", Ownership: contracts.AgentOwnershipLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Connectors) != 0 {
		t.Fatalf("removed catalog connector leaked into Agent policy: %#v", policy.Connectors)
	}
}

func newAgentPolicyFixture(t *testing.T) (*service, *memoryRepository, contracts.Connector) {
	t.Helper()
	release := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	release.Manifest.AuthorizationKind = "none"
	connector := newCatalogConnector(release)
	connector.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: release.Version,
		InstalledReleaseID: release.ReleaseID, InstalledReleaseDigest: release.ReleaseDigest,
	}
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateNotRequired}
	connector.Compatibility = contracts.Compatibility{State: contracts.CompatibilityStateSupported}
	repository := newMemoryRepository(connector)
	repository.catalogView.ListingsBySection["all"] = []contracts.CatalogListing{{Connector: connector}}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	convergence := contracts.RuntimeConvergence{
		Desired: contracts.RuntimeDesired{ConnectorKey: connector.Key, Generation: 1, Enabled: true,
			ConnectionID: defaultConnectorConnectionID, ReleaseDigest: release.ReleaseDigest,
			AuthorizationState: contracts.AuthorizationStateNotRequired},
		Observed: contracts.RuntimeObserved{DesiredGeneration: 1, BootEpoch: application.config.BootEpoch, Enabled: true,
			ConnectionID: defaultConnectorConnectionID, ReleaseDigest: release.ReleaseDigest,
			Readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady}},
	}
	repository.runtimeConvergences[memoryRuntimeConvergenceKey(contracts.OperationScope{}, connector.Key)] = convergence
	return application, repository, connector
}
