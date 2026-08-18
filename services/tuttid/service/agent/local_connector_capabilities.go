package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/composercatalog"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	preferencesbiz "github.com/tutti-os/tutti/services/tuttid/biz/preferences"
)

func (s *Service) connectorCatalogVisible(ctx context.Context) (bool, error) {
	if s == nil || s.DesktopPreferencesReader == nil {
		return false, nil
	}
	preferences, err := s.DesktopPreferencesReader.Get(ctx)
	if err != nil {
		return false, err
	}
	return preferencesbiz.IsLabFlagEnabled(
		preferences.FeatureFlags,
		preferencesbiz.LabFlagConnectors,
	), nil
}

func (s *Service) validatePromptConnectors(ctx context.Context, content []PromptContentBlock) error {
	requested := make(map[string]struct{})
	for _, block := range content {
		if block.Type == "connector" {
			requested[strings.TrimSpace(block.ConnectorKey)] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return nil
	}
	if s == nil || s.ConnectorMarketPolicy == nil {
		return fmt.Errorf("%w: local connector state is unavailable", ErrInvalidArgument)
	}
	target := localConnectorAgentTarget("local:agent", s.ConnectorMarketCurrentScope)
	snapshot, err := s.ConnectorMarketPolicy.Evaluate(ctx, target)
	if err != nil {
		return fmt.Errorf("read local connector state: %w", err)
	}
	for _, policy := range snapshot.Connectors {
		key := strings.TrimSpace(policy.Connector.Key)
		if _, ok := requested[key]; !ok {
			continue
		}
		if policy.Presentation.State != contracts.ConnectorStateConnected {
			return fmt.Errorf("%w: local connector %q is not ready", ErrInvalidArgument, key)
		}
		delete(requested, key)
	}
	for key := range requested {
		return fmt.Errorf("%w: local connector %q is not installed", ErrInvalidArgument, key)
	}
	return nil
}

func localConnectorCapabilityOptions(
	ctx context.Context,
	source application.AgentConnectorPolicyQueries,
	currentScope func() contracts.OperationScope,
	targetID string,
) ([]ComposerCapabilityOption, error) {
	snapshot, err := source.Evaluate(ctx, localConnectorAgentTarget(targetID, currentScope))
	if err != nil {
		return nil, err
	}
	return composercatalog.ProjectConnectorOptions(snapshot), nil
}

func localConnectorAgentTarget(
	targetID string,
	currentScope func() contracts.OperationScope,
) contracts.AgentTarget {
	target := contracts.AgentTarget{TargetID: strings.TrimSpace(targetID), Ownership: contracts.AgentOwnershipLocal}
	if target.TargetID == "" {
		target.TargetID = "local:agent"
	}
	if currentScope != nil {
		target.Scope = currentScope()
	}
	return target
}

func replaceComposerConnectorCapabilities(
	options []ComposerCapabilityOption,
	connectors []ComposerCapabilityOption,
) []ComposerCapabilityOption {
	result := make([]ComposerCapabilityOption, 0, len(options)+len(connectors))
	for _, option := range options {
		if option.Kind != "connector" {
			result = append(result, option)
		}
	}
	return mergeComposerCapabilityOptions(result, connectors)
}
