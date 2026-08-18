package application

import (
	"context"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func TestConnectorPresentationProjectsClosedTenStateMachine(t *testing.T) {
	application, repository, connector := newAgentPolicyFixture(t)
	convergence := repository.runtimeConvergences[memoryRuntimeConvergenceKey(contracts.OperationScope{}, connector.Key)]
	baseInputs := connectorPresentationInputs{
		freshness:        contracts.CatalogFreshness{State: contracts.CatalogFreshnessFresh, SnapshotID: "catalog-1"},
		convergenceByKey: map[string]contracts.RuntimeConvergence{connector.Key: convergence},
		installedRelease: map[contracts.InstalledReleaseRef]contracts.Release{
			{ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest}: connector.Release,
		},
	}
	tests := []struct {
		name   string
		mutate func(*contracts.Connector, *connectorPresentationInputs, *connectorPolicyFacts)
		want   contracts.ConnectorState
	}{
		{name: "unavailable", want: contracts.ConnectorStateUnavailable, mutate: func(_ *contracts.Connector, inputs *connectorPresentationInputs, _ *connectorPolicyFacts) {
			inputs.freshness = contracts.CatalogFreshness{State: contracts.CatalogFreshnessUnavailable}
		}},
		{name: "loading", want: contracts.ConnectorStateLoading, mutate: func(_ *contracts.Connector, inputs *connectorPresentationInputs, _ *connectorPolicyFacts) {
			inputs.freshness = contracts.CatalogFreshness{State: contracts.CatalogFreshnessRefreshing}
		}},
		{name: "setup required", want: contracts.ConnectorStateSetupRequired, mutate: func(connector *contracts.Connector, _ *connectorPresentationInputs, _ *connectorPolicyFacts) {
			connector.Installation = contracts.Installation{State: contracts.InstallationStateNotInstalled}
		}},
		{name: "authorization required", want: contracts.ConnectorStateAuthorizationRequired, mutate: func(connector *contracts.Connector, _ *connectorPresentationInputs, _ *connectorPolicyFacts) {
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
		}},
		{name: "connecting", want: contracts.ConnectorStateConnecting, mutate: func(connector *contracts.Connector, _ *connectorPresentationInputs, _ *connectorPolicyFacts) {
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
		}},
		{name: "connected", want: contracts.ConnectorStateConnected},
		{name: "degraded", want: contracts.ConnectorStateDegraded, mutate: func(_ *contracts.Connector, inputs *connectorPresentationInputs, _ *connectorPolicyFacts) {
			value := inputs.convergenceByKey[connector.Key]
			value.Observed.Readiness = contracts.RuntimeReadiness{State: contracts.RuntimeReadinessDegraded, ReasonCode: "route_unhealthy"}
			inputs.convergenceByKey[connector.Key] = value
		}},
		{name: "disabled", want: contracts.ConnectorStateDisabled, mutate: func(_ *contracts.Connector, inputs *connectorPresentationInputs, _ *connectorPolicyFacts) {
			value := inputs.convergenceByKey[connector.Key]
			value.Desired.Enabled = false
			inputs.convergenceByKey[connector.Key] = value
		}},
		{name: "unsupported", want: contracts.ConnectorStateUnsupported, mutate: func(connector *contracts.Connector, _ *connectorPresentationInputs, _ *connectorPolicyFacts) {
			connector.Compatibility.State = "future"
		}},
		{name: "failed", want: contracts.ConnectorStateFailed, mutate: func(connector *contracts.Connector, _ *connectorPresentationInputs, _ *connectorPolicyFacts) {
			connector.Installation = contracts.Installation{State: contracts.InstallationStateFailed, FailureCode: "install_failed"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := connector
			inputs := clonePresentationInputs(baseInputs)
			policy := localConnectorPolicyFacts()
			if test.mutate != nil {
				test.mutate(&candidate, &inputs, &policy)
			}
			presentation := application.projectConnectorPresentation(context.Background(), contracts.OperationScope{}, candidate, inputs, policy)
			if presentation.State != test.want {
				t.Fatalf("presentation = %#v, want state %q", presentation, test.want)
			}
			if err := presentation.Validate(); err != nil {
				t.Fatalf("presentation validation: %v", err)
			}
			if (presentation.State == contracts.ConnectorStateConnected) != hasConnectorAction(presentation, contracts.ConnectorActionSelect) {
				t.Fatalf("select admission = %#v", presentation.AllowedActions)
			}
			if (presentation.State == contracts.ConnectorStateDegraded || presentation.State == contracts.ConnectorStateFailed) &&
				hasConnectorActionValue(presentation, "retry") {
				t.Fatalf("unimplemented retry action admitted: %#v", presentation.AllowedActions)
			}
			if hasConnectorActionValue(presentation, "manage") {
				t.Fatalf("removed management action admitted: %#v", presentation.AllowedActions)
			}
		})
	}
}

func hasConnectorActionValue(presentation contracts.ConnectorPresentation, action string) bool {
	for _, candidate := range presentation.AllowedActions {
		if string(candidate) == action {
			return true
		}
	}
	return false
}

func TestConnectorPresentationFailedRecoveryActionsComeFromApplicationFacts(t *testing.T) {
	application, repository, connector := newAgentPolicyFixture(t)
	convergence := repository.runtimeConvergences[memoryRuntimeConvergenceKey(contracts.OperationScope{}, connector.Key)]
	inputs := connectorPresentationInputs{
		freshness:        contracts.CatalogFreshness{State: contracts.CatalogFreshnessFresh, SnapshotID: "catalog-1"},
		convergenceByKey: map[string]contracts.RuntimeConvergence{connector.Key: convergence},
		installedRelease: map[contracts.InstalledReleaseRef]contracts.Release{
			{ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest}: connector.Release,
		},
	}
	tests := []struct {
		name      string
		connector contracts.Connector
		inputs    connectorPresentationInputs
		want      contracts.ConnectorAction
	}{
		{name: "installation failure", connector: func() contracts.Connector {
			value := connector
			value.Installation.State = contracts.InstallationStateFailed
			value.Installation.FailureCode = "artifact_invalid"
			return value
		}(), inputs: clonePresentationInputs(inputs), want: contracts.ConnectorActionInstall},
		{name: "authorization failure", connector: func() contracts.Connector {
			value := connector
			value.Authorization.State = contracts.AuthorizationStateFailed
			value.Authorization.FailureCode = "provider_rejected"
			return value
		}(), inputs: clonePresentationInputs(inputs), want: contracts.ConnectorActionAuthorize},
		{name: "runtime failure", connector: connector, inputs: func() connectorPresentationInputs {
			value := clonePresentationInputs(inputs)
			runtime := value.convergenceByKey[connector.Key]
			runtime.Observed.Readiness = contracts.RuntimeReadiness{State: contracts.RuntimeReadinessFailed, ReasonCode: "runtime_start_failed"}
			value.convergenceByKey[connector.Key] = runtime
			return value
		}(), want: contracts.ConnectorActionRestartRuntime},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			presentation := application.projectConnectorPresentation(
				context.Background(), contracts.OperationScope{}, test.connector, test.inputs, localConnectorPolicyFacts(),
			)
			if presentation.State != contracts.ConnectorStateFailed || !hasConnectorAction(presentation, test.want) {
				t.Fatalf("failed recovery projection = %#v, want action %q", presentation, test.want)
			}
		})
	}
}

func TestConnectorPresentationRequiresEveryExactRuntimeObservationField(t *testing.T) {
	application, repository, connector := newAgentPolicyFixture(t)
	base := repository.runtimeConvergences[memoryRuntimeConvergenceKey(contracts.OperationScope{}, connector.Key)]
	mutations := map[string]func(*contracts.RuntimeConvergence){
		"desired connector":  func(value *contracts.RuntimeConvergence) { value.Desired.ConnectorKey = "other" },
		"desired scope":      func(value *contracts.RuntimeConvergence) { value.Desired.Scope.AccountID = "other" },
		"desired generation": func(value *contracts.RuntimeConvergence) { value.Desired.Generation = 0 },
		"desired release":    func(value *contracts.RuntimeConvergence) { value.Desired.ReleaseDigest = "other" },
		"desired enabled":    func(value *contracts.RuntimeConvergence) { value.Desired.Enabled = false },
		"desired connection": func(value *contracts.RuntimeConvergence) { value.Desired.ConnectionID = "other" },
		"desired authorization": func(value *contracts.RuntimeConvergence) {
			value.Desired.AuthorizationState = contracts.AuthorizationStateDisconnected
		},
		"observed generation": func(value *contracts.RuntimeConvergence) { value.Observed.DesiredGeneration++ },
		"observed boot":       func(value *contracts.RuntimeConvergence) { value.Observed.BootEpoch = "old-boot" },
		"observed enabled":    func(value *contracts.RuntimeConvergence) { value.Observed.Enabled = false },
		"observed connection": func(value *contracts.RuntimeConvergence) { value.Observed.ConnectionID = "other" },
		"observed release":    func(value *contracts.RuntimeConvergence) { value.Observed.ReleaseDigest = "other" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			inputs := connectorPresentationInputs{
				freshness:        contracts.CatalogFreshness{State: contracts.CatalogFreshnessFresh, SnapshotID: "catalog-1"},
				convergenceByKey: map[string]contracts.RuntimeConvergence{connector.Key: candidate},
				installedRelease: map[contracts.InstalledReleaseRef]contracts.Release{
					{ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest}: connector.Release,
				},
			}
			presentation := application.projectConnectorPresentation(context.Background(), contracts.OperationScope{}, connector, inputs, localConnectorPolicyFacts())
			if presentation.State == contracts.ConnectorStateConnected || hasConnectorAction(presentation, contracts.ConnectorActionSelect) {
				t.Fatalf("mismatched runtime became selectable: %#v", presentation)
			}
		})
	}
}

func TestConnectorPresentationCatalogAndPolicyActionsFailClosed(t *testing.T) {
	application, repository, connector := newAgentPolicyFixture(t)
	convergence := repository.runtimeConvergences[memoryRuntimeConvergenceKey(contracts.OperationScope{}, connector.Key)]
	inputs := connectorPresentationInputs{
		freshness:        contracts.CatalogFreshness{State: contracts.CatalogFreshnessStale, SnapshotID: "catalog-1", StaleSince: timePointer(time.Now())},
		convergenceByKey: map[string]contracts.RuntimeConvergence{connector.Key: convergence},
		installedRelease: map[contracts.InstalledReleaseRef]contracts.Release{
			{ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest}: connector.Release,
		},
	}

	connected := application.projectConnectorPresentation(context.Background(), contracts.OperationScope{}, connector, inputs, localConnectorPolicyFacts())
	if connected.State != contracts.ConnectorStateConnected || !hasConnectorAction(connected, contracts.ConnectorActionSelect) ||
		hasConnectorAction(connected, contracts.ConnectorActionInstall) || hasConnectorAction(connected, contracts.ConnectorActionUpdate) {
		t.Fatalf("stale exact-ready projection = %#v", connected)
	}

	authorizationRequired := connector
	authorizationRequired.Authorization.State = contracts.AuthorizationStateDisconnected
	staleAuthorization := application.projectConnectorPresentation(context.Background(), contracts.OperationScope{}, authorizationRequired, inputs, localConnectorPolicyFacts())
	if hasConnectorAction(staleAuthorization, contracts.ConnectorActionAuthorize) {
		t.Fatalf("stale authorization admitted a new mutation: %#v", staleAuthorization)
	}

	for _, policy := range []connectorPolicyFacts{
		{supportState: contracts.SupportedConnectorSetReady, grantState: contracts.SupportedConnectorSetReady, granted: true},
		{supportState: contracts.SupportedConnectorSetUnavailable, grantState: contracts.SupportedConnectorSetReady, supported: true, granted: true},
		{supportState: contracts.SupportedConnectorSetReady, grantState: contracts.SupportedConnectorSetStale, supported: true, granted: true},
		{supportState: contracts.SupportedConnectorSetReady, grantState: contracts.SupportedConnectorSetReady, supported: true},
	} {
		presentation := application.projectConnectorPresentation(context.Background(), contracts.OperationScope{}, connector, inputs, policy)
		for _, action := range presentation.AllowedActions {
			if action != contracts.ConnectorActionDetails && action != contracts.ConnectorActionRemoveSelection {
				t.Fatalf("policy failure admitted %q: %#v", action, presentation)
			}
		}
	}
}

func TestAgentPolicySharedReadyEmptyAllowlistIsUnsupported(t *testing.T) {
	application, _, connector := newAgentPolicyFixture(t)
	application.config.SharedAgentSupport = sharedAgentSupportStub{set: contracts.SupportedConnectorSet{State: contracts.SupportedConnectorSetReady}}
	application.config.AgentConnectorGrants = agentConnectorGrantStub{set: contracts.AgentConnectorGrantSet{
		State: contracts.SupportedConnectorSetReady, Keys: []string{connector.Key},
	}}
	policy, err := application.Evaluate(context.Background(), contracts.AgentTarget{
		TargetID: "shared:empty", Ownership: contracts.AgentOwnershipShared,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Connectors) != 1 || policy.Connectors[0].Presentation.State != contracts.ConnectorStateUnsupported ||
		policy.Connectors[0].Supported || hasConnectorAction(policy.Connectors[0].Presentation, contracts.ConnectorActionUninstall) {
		t.Fatalf("empty allowlist projection = %#v", policy)
	}
}

func clonePresentationInputs(value connectorPresentationInputs) connectorPresentationInputs {
	result := connectorPresentationInputs{
		freshness:        value.freshness,
		convergenceByKey: make(map[string]contracts.RuntimeConvergence, len(value.convergenceByKey)),
		installedRelease: make(map[contracts.InstalledReleaseRef]contracts.Release, len(value.installedRelease)),
	}
	for key, convergence := range value.convergenceByKey {
		result.convergenceByKey[key] = convergence
	}
	for key, release := range value.installedRelease {
		result.installedRelease[key] = release
	}
	return result
}
