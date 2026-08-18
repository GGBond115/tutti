package application

import (
	"context"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

type connectorPresentationInputs struct {
	freshness        contracts.CatalogFreshness
	convergenceByKey map[string]contracts.RuntimeConvergence
	installedRelease map[contracts.InstalledReleaseRef]contracts.Release
}

type connectorPolicyFacts struct {
	supportState contracts.SupportedConnectorSetState
	grantState   contracts.SupportedConnectorSetState
	supported    bool
	granted      bool
}

func localConnectorPolicyFacts() connectorPolicyFacts {
	return connectorPolicyFacts{
		supportState: contracts.SupportedConnectorSetReady,
		grantState:   contracts.SupportedConnectorSetReady,
		supported:    true,
		granted:      true,
	}
}

func (application *service) connectorPresentationInputs(
	ctx context.Context,
	scope contracts.OperationScope,
	freshness contracts.CatalogFreshness,
	connectors []contracts.Connector,
) (connectorPresentationInputs, error) {
	convergences, err := application.config.Repository.RuntimeConvergences(ctx, scope, maxRuntimeConvergenceSnapshot+1)
	if err != nil {
		return connectorPresentationInputs{}, err
	}
	if len(convergences) > maxRuntimeConvergenceSnapshot {
		return connectorPresentationInputs{}, contracts.NewDomainError(
			contracts.ErrorCodeUnavailable, "connector presentation runtime snapshot exceeds limit", true, nil,
		)
	}
	convergenceByKey := make(map[string]contracts.RuntimeConvergence, len(convergences))
	for _, convergence := range convergences {
		convergenceByKey[strings.TrimSpace(convergence.Desired.ConnectorKey)] = convergence
	}
	refs := make([]contracts.InstalledReleaseRef, 0, len(connectors))
	for _, connector := range connectors {
		if connector.Installation.State == contracts.InstallationStateInstalled {
			refs = append(refs, contracts.InstalledReleaseRef{
				ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest,
			})
		}
	}
	installedReleases, err := application.config.Repository.InstalledReleases(ctx, refs)
	if err != nil {
		return connectorPresentationInputs{}, err
	}
	return connectorPresentationInputs{
		freshness: freshness, convergenceByKey: convergenceByKey, installedRelease: installedReleases,
	}, nil
}

func (application *service) projectConnectorPresentation(
	ctx context.Context,
	scope contracts.OperationScope,
	connector contracts.Connector,
	inputs connectorPresentationInputs,
	policy connectorPolicyFacts,
) contracts.ConnectorPresentation {
	state, reasonCode := application.projectConnectorState(ctx, scope, connector, inputs, policy)
	presentation := contracts.ConnectorPresentation{
		State: state, ReasonCode: nonEmptyPresentationReason(state, reasonCode),
		AllowedActions: connectorAllowedActions(state, connector, inputs.freshness, policy),
	}
	if err := presentation.Validate(); err != nil {
		return contracts.ConnectorPresentation{
			State: contracts.ConnectorStateUnsupported, ReasonCode: "invalid_connector_presentation",
			AllowedActions: []contracts.ConnectorAction{
				contracts.ConnectorActionDetails, contracts.ConnectorActionRemoveSelection,
			},
		}
	}
	return presentation
}

func nonEmptyPresentationReason(state contracts.ConnectorState, reason string) string {
	if state == contracts.ConnectorStateConnected {
		return ""
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		return reason
	}
	return "connector_state_unavailable"
}

func (application *service) projectConnectorState(
	ctx context.Context,
	scope contracts.OperationScope,
	connector contracts.Connector,
	inputs connectorPresentationInputs,
	policy connectorPolicyFacts,
) (contracts.ConnectorState, string) {
	if policy.supportState != contracts.SupportedConnectorSetReady {
		return contracts.ConnectorStateUnsupported, "agent_connector_support_" + connectorSetStateReason(policy.supportState)
	}
	if !policy.supported {
		return contracts.ConnectorStateUnsupported, "shared_agent_connector_not_declared"
	}
	if policy.grantState != contracts.SupportedConnectorSetReady {
		return contracts.ConnectorStateDisabled, "agent_connector_grant_" + connectorSetStateReason(policy.grantState)
	}
	if !policy.granted {
		return contracts.ConnectorStateDisabled, "agent_connector_grant_missing"
	}
	if inputs.freshness.SnapshotID == "" {
		if inputs.freshness.State == contracts.CatalogFreshnessRefreshing {
			return contracts.ConnectorStateLoading, "catalog_initial_refresh"
		}
		return contracts.ConnectorStateUnavailable, "catalog_unavailable"
	}
	switch inputs.freshness.State {
	case contracts.CatalogFreshnessFresh, contracts.CatalogFreshnessStale, contracts.CatalogFreshnessRefreshing:
	default:
		return contracts.ConnectorStateUnsupported, "unknown_catalog_freshness"
	}
	if connector.Compatibility.State != contracts.CompatibilityStateSupported {
		switch connector.Compatibility.State {
		case contracts.CompatibilityStateUnsupportedProduct, contracts.CompatibilityStateUnsupportedPlatform,
			contracts.CompatibilityStateUnsupportedVersion, contracts.CompatibilityStateUnsupportedImplementation:
			return contracts.ConnectorStateUnsupported, string(connector.Compatibility.State)
		default:
			return contracts.ConnectorStateUnsupported, "unknown_compatibility_state"
		}
	}
	switch connector.Installation.State {
	case contracts.InstallationStateNotInstalled:
		return contracts.ConnectorStateSetupRequired, "connector_not_installed"
	case contracts.InstallationStateInstalling, contracts.InstallationStateUpdating, contracts.InstallationStateUninstalling:
		return contracts.ConnectorStateConnecting, "installation_converging"
	case contracts.InstallationStateFailed:
		return contracts.ConnectorStateFailed, connector.Installation.FailureCode
	case contracts.InstallationStateInstalled:
	default:
		return contracts.ConnectorStateUnsupported, "unknown_installation_state"
	}
	switch connector.Authorization.State {
	case contracts.AuthorizationStateDisconnected, contracts.AuthorizationStateExpired:
		return contracts.ConnectorStateAuthorizationRequired, connector.Authorization.FailureCode
	case contracts.AuthorizationStatePending:
		return contracts.ConnectorStateConnecting, "authorization_pending"
	case contracts.AuthorizationStateFailed:
		return contracts.ConnectorStateFailed, connector.Authorization.FailureCode
	case contracts.AuthorizationStateNotRequired, contracts.AuthorizationStateConnected:
	default:
		return contracts.ConnectorStateUnsupported, "unknown_authorization_state"
	}
	convergence, found := inputs.convergenceByKey[connector.Key]
	if !found {
		return contracts.ConnectorStateConnecting, "runtime_not_observed"
	}
	if !convergence.Desired.Enabled {
		return contracts.ConnectorStateDisabled, "runtime_disabled"
	}
	releaseRef := contracts.InstalledReleaseRef{
		ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest,
	}
	installedRelease, found := inputs.installedRelease[releaseRef]
	if !found {
		return contracts.ConnectorStateDegraded, "installed_release_evidence_unavailable"
	}
	binding, err := application.config.RuntimeIntents.ResolveRuntimeIntent(ctx, contracts.RuntimeBindingRequest{
		OperationID: "connector-presentation/" + connector.Key,
		Scope:       scope, Purpose: contracts.RuntimeBindingPurposePlan, Connector: connector, Release: installedRelease,
	})
	if err != nil {
		return contracts.ConnectorStateDegraded, "runtime_binding_unavailable"
	}
	desiredCurrent := convergence.Desired.ConnectorKey == connector.Key &&
		strings.TrimSpace(convergence.Desired.Scope.AccountID) == strings.TrimSpace(scope.AccountID) &&
		convergence.Desired.Generation > 0 &&
		convergence.Desired.ReleaseDigest == installedRelease.ReleaseDigest &&
		convergence.Desired.Enabled == binding.Enabled &&
		convergence.Desired.ConnectionID == strings.TrimSpace(binding.ConnectionID) &&
		convergence.Desired.AuthorizationState == binding.AuthorizationState
	if !desiredCurrent {
		return contracts.ConnectorStateConnecting, "runtime_desired_stale"
	}
	exact := convergence.Observed.DesiredGeneration == convergence.Desired.Generation &&
		convergence.Observed.BootEpoch == application.config.BootEpoch &&
		convergence.Observed.Enabled == convergence.Desired.Enabled &&
		convergence.Observed.ConnectionID == convergence.Desired.ConnectionID &&
		convergence.Observed.ReleaseDigest == convergence.Desired.ReleaseDigest
	if !exact {
		if convergence.Attempt >= contracts.RuntimeFailureBudget {
			return contracts.ConnectorStateFailed, convergence.LastErrorCode
		}
		if convergence.Attempt > 0 || convergence.LastErrorCode != "" {
			return contracts.ConnectorStateDegraded, convergence.LastErrorCode
		}
		return contracts.ConnectorStateConnecting, "runtime_converging"
	}
	switch convergence.Observed.Readiness.State {
	case contracts.RuntimeReadinessReady:
		return contracts.ConnectorStateConnected, ""
	case contracts.RuntimeReadinessDegraded, contracts.RuntimeReadinessBlocked:
		return contracts.ConnectorStateDegraded, convergence.Observed.Readiness.ReasonCode
	case contracts.RuntimeReadinessFailed:
		return contracts.ConnectorStateFailed, convergence.Observed.Readiness.ReasonCode
	default:
		return contracts.ConnectorStateUnsupported, "unknown_runtime_readiness"
	}
}

func connectorSetStateReason(state contracts.SupportedConnectorSetState) string {
	switch state {
	case contracts.SupportedConnectorSetLoading:
		return "loading"
	case contracts.SupportedConnectorSetStale:
		return "stale"
	case contracts.SupportedConnectorSetUnavailable:
		return "unavailable"
	default:
		return "unsupported"
	}
}

func connectorAllowedActions(
	state contracts.ConnectorState,
	connector contracts.Connector,
	freshness contracts.CatalogFreshness,
	policy connectorPolicyFacts,
) []contracts.ConnectorAction {
	actions := []contracts.ConnectorAction{contracts.ConnectorActionDetails, contracts.ConnectorActionRemoveSelection}
	if policy.supportState != contracts.SupportedConnectorSetReady || !policy.supported ||
		policy.grantState != contracts.SupportedConnectorSetReady || !policy.granted {
		return actions
	}
	mutationFresh := freshness.SnapshotID != "" && (freshness.State == contracts.CatalogFreshnessFresh ||
		(freshness.State == contracts.CatalogFreshnessRefreshing && freshness.StaleSince == nil))
	installed := connectorHasInstalledArtifact(connector)
	switch state {
	case contracts.ConnectorStateLoading, contracts.ConnectorStateUnavailable:
		return []contracts.ConnectorAction{contracts.ConnectorActionRemoveSelection}
	case contracts.ConnectorStateSetupRequired:
		if mutationFresh {
			actions = append(actions, contracts.ConnectorActionInstall)
		}
	case contracts.ConnectorStateAuthorizationRequired:
		if mutationFresh {
			actions = append(actions, contracts.ConnectorActionAuthorize)
		}
	case contracts.ConnectorStateConnecting:
		if connector.Authorization.State == contracts.AuthorizationStatePending {
			actions = append(actions, contracts.ConnectorActionCancel)
		}
	case contracts.ConnectorStateConnected:
		actions = append(actions, contracts.ConnectorActionSelect)
		if connector.Authorization.State == contracts.AuthorizationStateConnected {
			actions = append(actions, contracts.ConnectorActionDisconnect)
		}
		if mutationFresh && connector.Installation.InstalledReleaseDigest != "" &&
			connector.Installation.InstalledReleaseDigest != connector.Release.ReleaseDigest {
			actions = append(actions, contracts.ConnectorActionUpdate)
		}
	case contracts.ConnectorStateDegraded:
		if connector.Authorization.State == contracts.AuthorizationStateConnected {
			actions = append(actions, contracts.ConnectorActionDisconnect)
		}
	case contracts.ConnectorStateDisabled:
		if connector.Authorization.State == contracts.AuthorizationStateConnected {
			actions = append(actions, contracts.ConnectorActionDisconnect)
		}
	case contracts.ConnectorStateFailed:
		switch {
		case connector.Installation.State == contracts.InstallationStateFailed:
			if mutationFresh {
				actions = append(actions, contracts.ConnectorActionInstall)
			}
		case connector.Authorization.State == contracts.AuthorizationStateFailed:
			if mutationFresh {
				actions = append(actions, contracts.ConnectorActionAuthorize)
			}
		case connector.Installation.State == contracts.InstallationStateInstalled:
			actions = append(actions, contracts.ConnectorActionRestartRuntime)
		}
	}
	if installed && state != contracts.ConnectorStateConnecting {
		actions = append(actions, contracts.ConnectorActionUninstall)
	}
	return actions
}

func (application *service) PresentConnectorForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	connector contracts.Connector,
) (contracts.ConnectorView, error) {
	projected, err := application.projectConnectorForScope(ctx, scope, connector)
	if err != nil {
		return contracts.ConnectorView{}, err
	}
	view, err := application.config.Repository.CatalogView(ctx)
	if err != nil {
		return contracts.ConnectorView{}, err
	}
	inputs, err := application.connectorPresentationInputs(ctx, scope, view.Freshness, []contracts.Connector{projected})
	if err != nil {
		return contracts.ConnectorView{}, err
	}
	return contracts.ConnectorView{
		Connector:    projected,
		Presentation: application.projectConnectorPresentation(ctx, scope, projected, inputs, localConnectorPolicyFacts()),
	}, nil
}

func (application *service) GetConnectorViewForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) (contracts.ConnectorView, error) {
	connector, err := application.GetConnectorForScope(ctx, scope, connectorKey)
	if err != nil {
		return contracts.ConnectorView{}, err
	}
	return application.PresentConnectorForScope(ctx, scope, connector)
}

func (application *service) SnapshotViewForScope(
	ctx context.Context,
	scope contracts.OperationScope,
) (contracts.SnapshotView, error) {
	snapshot, err := application.SnapshotForScope(ctx, scope)
	if err != nil {
		return contracts.SnapshotView{}, err
	}
	inputs, err := application.connectorPresentationInputs(ctx, scope, snapshot.CatalogFreshness, snapshot.Connectors)
	if err != nil {
		return contracts.SnapshotView{}, err
	}
	result := contracts.SnapshotView{
		CatalogFreshness: snapshot.CatalogFreshness, Operations: snapshot.Operations,
		Revision: snapshot.Revision, EventCursor: snapshot.EventCursor,
		Connectors: make([]contracts.ConnectorView, 0, len(snapshot.Connectors)),
	}
	for _, connector := range snapshot.Connectors {
		result.Connectors = append(result.Connectors, contracts.ConnectorView{
			Connector:    connector,
			Presentation: application.projectConnectorPresentation(ctx, scope, connector, inputs, localConnectorPolicyFacts()),
		})
	}
	return result, nil
}

func (application *service) ListCatalogPageViewForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	query contracts.CatalogPageQuery,
) (contracts.CatalogPageView, error) {
	page, err := application.ListCatalogPageForScope(ctx, scope, query)
	if err != nil {
		return contracts.CatalogPageView{}, err
	}
	connectors := make([]contracts.Connector, 0, len(page.Items))
	for _, item := range page.Items {
		connectors = append(connectors, item.Connector)
	}
	view, err := application.config.Repository.CatalogView(ctx)
	if err != nil {
		return contracts.CatalogPageView{}, err
	}
	if view.Revision != page.Revision {
		return contracts.CatalogPageView{}, contracts.NewDomainError(
			contracts.ErrorCodeRevisionConflict, "connector catalog changed during presentation", false, nil,
		)
	}
	inputs, err := application.connectorPresentationInputs(ctx, scope, view.Freshness, connectors)
	if err != nil {
		return contracts.CatalogPageView{}, err
	}
	result := contracts.CatalogPageView{
		SectionID: page.SectionID, NextPageToken: page.NextPageToken, Revision: page.Revision,
		Items: make([]contracts.CatalogListingView, 0, len(page.Items)),
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, contracts.CatalogListingView{
			CategoryID: item.CategoryID, Featured: item.Featured,
			Connector: contracts.ConnectorView{
				Connector:    item.Connector,
				Presentation: application.projectConnectorPresentation(ctx, scope, item.Connector, inputs, localConnectorPolicyFacts()),
			},
		})
	}
	return result, nil
}
