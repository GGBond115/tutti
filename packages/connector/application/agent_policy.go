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
				contracts.ErrorCodeRevisionConflict, "connector policy inputs changed during evaluation", false, nil,
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
	activeConnectors := make([]contracts.Connector, 0, len(active))
	for _, connector := range snapshot.Connectors {
		if _, inActiveCatalog := active[connector.Key]; inActiveCatalog {
			activeConnectors = append(activeConnectors, connector)
		}
	}
	inputs, err := application.connectorPresentationInputs(ctx, target.Scope, snapshot.CatalogFreshness, activeConnectors)
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
		if explicitlyEmptySupport {
			declared = false
		}
		policy := contracts.AgentConnectorPolicy{
			Connector: connector, Supported: declared, Granted: hasGrant,
			Presentation: application.projectConnectorPresentation(ctx, target.Scope, connector, inputs, connectorPolicyFacts{
				supportState: supportState, grantState: grantState, supported: declared, granted: hasGrant,
			}),
		}
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
