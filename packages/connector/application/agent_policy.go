package application

import (
	"context"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

// Evaluate returns the application-owned Connector readiness projection for
// one Agent target. Callers map this result to their DTOs; they do not
// reinterpret installation, authorization, compatibility, or runtime state.
func (application *service) Evaluate(
	ctx context.Context,
	target contracts.AgentTarget,
) (contracts.AgentConnectorPolicySnapshot, error) {
	target.TargetID = strings.TrimSpace(target.TargetID)
	if target.TargetID == "" {
		return contracts.AgentConnectorPolicySnapshot{}, invalidRequest("agent target id is required")
	}
	if target.Ownership != contracts.AgentOwnershipLocal && target.Ownership != contracts.AgentOwnershipShared {
		return contracts.AgentConnectorPolicySnapshot{}, invalidRequest("agent target ownership is invalid")
	}
	var snapshot contracts.Snapshot
	var view contracts.CatalogView
	for attempt := 0; attempt < 2; attempt++ {
		var err error
		snapshot, err = application.SnapshotForScope(ctx, target.Scope)
		if err != nil {
			return contracts.AgentConnectorPolicySnapshot{}, err
		}
		view, err = application.config.Repository.CatalogView(ctx)
		if err != nil {
			return contracts.AgentConnectorPolicySnapshot{}, err
		}
		if snapshot.Revision == view.Revision {
			break
		}
		if attempt == 1 {
			return contracts.AgentConnectorPolicySnapshot{}, contracts.NewDomainError(
				contracts.ErrorCodeRevisionConflict, "connector policy inputs changed during evaluation", true, nil,
			)
		}
	}
	result := contracts.AgentConnectorPolicySnapshot{
		Target: target, CatalogFreshness: snapshot.CatalogFreshness, Revision: snapshot.Revision,
	}
	supported, supportState, explicitlyEmptySupport, err := application.agentSupportedConnectorKeys(ctx, target, view)
	if err != nil {
		return contracts.AgentConnectorPolicySnapshot{}, err
	}
	result.SupportState = supportState
	granted, grantState, err := application.agentGrantedConnectorKeys(ctx, target, view)
	if err != nil {
		return contracts.AgentConnectorPolicySnapshot{}, err
	}
	result.GrantState = grantState
	active := activeCatalogConnectorKeys(view)
	convergences, err := application.config.Repository.RuntimeConvergences(ctx, target.Scope, maxRuntimeConvergenceSnapshot+1)
	if err != nil {
		return contracts.AgentConnectorPolicySnapshot{}, err
	}
	if len(convergences) > maxRuntimeConvergenceSnapshot {
		return contracts.AgentConnectorPolicySnapshot{}, contracts.NewDomainError(
			contracts.ErrorCodeUnavailable, "connector policy runtime snapshot exceeds limit", true, nil,
		)
	}
	convergenceByKey := make(map[string]contracts.RuntimeConvergence, len(convergences))
	for _, convergence := range convergences {
		convergenceByKey[convergence.Desired.ConnectorKey] = convergence
	}
	refs := make([]contracts.InstalledReleaseRef, 0, len(active))
	for _, connector := range snapshot.Connectors {
		if _, inActiveCatalog := active[connector.Key]; inActiveCatalog &&
			connector.Installation.State == contracts.InstallationStateInstalled {
			refs = append(refs, contracts.InstalledReleaseRef{
				ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest,
			})
		}
	}
	installedReleases, err := application.config.Repository.InstalledReleases(ctx, refs)
	if err != nil {
		return contracts.AgentConnectorPolicySnapshot{}, err
	}
	result.Connectors = make([]contracts.AgentConnectorPolicy, 0, len(active))
	for _, connector := range snapshot.Connectors {
		if _, inActiveCatalog := active[connector.Key]; !inActiveCatalog {
			continue
		}
		_, declared := supported[connector.Key]
		_, hasGrant := granted[connector.Key]
		policy := contracts.AgentConnectorPolicy{Connector: connector, Supported: declared, Granted: hasGrant}
		if !declared {
			if target.Ownership == contracts.AgentOwnershipShared && supportState == contracts.SupportedConnectorSetReady && explicitlyEmptySupport {
				policy.State = contracts.ConnectorStateDisabled
				policy.ReasonCode = "shared_agent_connector_disabled"
			} else if target.Ownership == contracts.AgentOwnershipShared && supportState == contracts.SupportedConnectorSetReady {
				policy.State = contracts.ConnectorStateUnsupported
				policy.ReasonCode = "shared_agent_connector_not_declared"
			} else {
				policy.State = contracts.ConnectorStateUnsupported
				policy.ReasonCode = "agent_connector_policy_unavailable"
			}
		} else if !hasGrant {
			policy.State = contracts.ConnectorStateDisabled
			policy.ReasonCode = "agent_connector_grant_missing"
		} else {
			policy.State, policy.ReasonCode = application.agentConnectorState(
				ctx, target.Scope, snapshot.CatalogFreshness, connector, convergenceByKey, installedReleases,
			)
		}
		policy.Selectable = policy.State == contracts.ConnectorStateConnected
		result.Connectors = append(result.Connectors, policy)
	}
	return result, nil
}

func activeCatalogConnectorKeys(view contracts.CatalogView) map[string]struct{} {
	active := make(map[string]struct{})
	for _, listings := range view.ListingsBySection {
		for _, listing := range listings {
			if key := strings.TrimSpace(listing.Connector.Key); key != "" {
				active[key] = struct{}{}
			}
		}
	}
	return active
}

func (application *service) agentGrantedConnectorKeys(
	ctx context.Context,
	target contracts.AgentTarget,
	view contracts.CatalogView,
) (map[string]struct{}, contracts.SupportedConnectorSetState, error) {
	active := activeCatalogConnectorKeys(view)
	result := make(map[string]struct{}, len(active))
	if target.Ownership == contracts.AgentOwnershipLocal {
		for key := range active {
			result[key] = struct{}{}
		}
		return result, contracts.SupportedConnectorSetReady, nil
	}
	if application.config.AgentConnectorGrants == nil {
		return result, contracts.SupportedConnectorSetUnavailable, nil
	}
	set, err := application.config.AgentConnectorGrants.GrantedConnectorKeys(ctx, target.TargetID, target.Scope)
	if err != nil {
		return nil, "", err
	}
	set.State = normalizeConnectorSetState(set.State)
	if set.State != contracts.SupportedConnectorSetReady {
		return result, set.State, nil
	}
	for _, key := range set.Keys {
		if key = strings.TrimSpace(key); key != "" {
			if _, exists := active[key]; !exists {
				continue
			}
			result[key] = struct{}{}
		}
	}
	return result, set.State, nil
}

func (application *service) agentSupportedConnectorKeys(
	ctx context.Context,
	target contracts.AgentTarget,
	view contracts.CatalogView,
) (map[string]struct{}, contracts.SupportedConnectorSetState, bool, error) {
	active := activeCatalogConnectorKeys(view)
	result := make(map[string]struct{}, len(active))
	if target.Ownership == contracts.AgentOwnershipLocal {
		for key := range active {
			result[key] = struct{}{}
		}
		return result, contracts.SupportedConnectorSetReady, false, nil
	}
	if application.config.SharedAgentSupport == nil {
		return result, contracts.SupportedConnectorSetUnavailable, false, nil
	}
	set, err := application.config.SharedAgentSupport.SupportedConnectorKeys(ctx, target.TargetID)
	if err != nil {
		return nil, "", false, err
	}
	set.State = normalizeConnectorSetState(set.State)
	if set.State != contracts.SupportedConnectorSetReady {
		return result, set.State, false, nil
	}
	for _, key := range set.Keys {
		if key = strings.TrimSpace(key); key != "" {
			if _, exists := active[key]; !exists {
				continue
			}
			result[key] = struct{}{}
		}
	}
	return result, set.State, len(set.Keys) == 0, nil
}

func normalizeConnectorSetState(state contracts.SupportedConnectorSetState) contracts.SupportedConnectorSetState {
	switch state {
	case contracts.SupportedConnectorSetReady, contracts.SupportedConnectorSetLoading,
		contracts.SupportedConnectorSetUnavailable, contracts.SupportedConnectorSetStale:
		return state
	default:
		return contracts.SupportedConnectorSetUnavailable
	}
}

func (application *service) agentConnectorState(
	ctx context.Context,
	scope contracts.OperationScope,
	freshness contracts.CatalogFreshness,
	connector contracts.Connector,
	convergences map[string]contracts.RuntimeConvergence,
	installedReleases map[contracts.InstalledReleaseRef]contracts.Release,
) (contracts.ConnectorState, string) {
	if freshness.SnapshotID == "" {
		if freshness.State == contracts.CatalogFreshnessRefreshing {
			return contracts.ConnectorStateLoading, "catalog_initial_refresh"
		}
		return contracts.ConnectorStateUnavailable, "catalog_unavailable"
	}
	if freshness.State != contracts.CatalogFreshnessFresh && freshness.State != contracts.CatalogFreshnessStale &&
		freshness.State != contracts.CatalogFreshnessRefreshing {
		return contracts.ConnectorStateUnsupported, "unknown_catalog_freshness"
	}
	if connector.Compatibility.State != contracts.CompatibilityStateSupported {
		return contracts.ConnectorStateUnsupported, string(connector.Compatibility.State)
	}
	switch connector.Installation.State {
	case contracts.InstallationStateNotInstalled:
		return contracts.ConnectorStateSetupRequired, "connector_not_installed"
	case contracts.InstallationStateInstalling, contracts.InstallationStateUpdating, contracts.InstallationStateUninstalling:
		return contracts.ConnectorStateConnecting, "installation_converging"
	case contracts.InstallationStateFailed:
		return contracts.ConnectorStateFailed, connector.Installation.FailureCode
	case contracts.InstallationStateInstalled:
		// Continue into account and physical runtime readiness.
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
		// Continue into exact runtime observation.
	default:
		return contracts.ConnectorStateUnsupported, "unknown_authorization_state"
	}
	convergence, found := convergences[connector.Key]
	if !found {
		return contracts.ConnectorStateConnecting, "runtime_not_observed"
	}
	if !convergence.Desired.Enabled {
		return contracts.ConnectorStateDisabled, "runtime_disabled"
	}
	releaseRef := contracts.InstalledReleaseRef{
		ConnectorKey: connector.Key, ReleaseDigest: connector.Installation.InstalledReleaseDigest,
	}
	installedRelease, found := installedReleases[releaseRef]
	if !found {
		return contracts.ConnectorStateDegraded, "installed_release_evidence_unavailable"
	}
	binding, err := application.config.RuntimeIntents.ResolveRuntimeIntent(ctx, contracts.RuntimeBindingRequest{
		OperationID: "agent-policy/" + connector.Key,
		Scope:       scope, Purpose: contracts.RuntimeBindingPurposePlan, Connector: connector, Release: installedRelease,
	})
	if err != nil {
		return contracts.ConnectorStateDegraded, "runtime_binding_unavailable"
	}
	desiredCurrent := convergence.Desired.ReleaseDigest == installedRelease.ReleaseDigest &&
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
