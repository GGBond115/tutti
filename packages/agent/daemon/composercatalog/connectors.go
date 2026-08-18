// Package composercatalog projects provider-neutral capability data for Agent
// composer surfaces shared by daemon hosts.
package composercatalog

import (
	"context"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
)

const (
	CapabilityKindConnector         = "connector"
	CapabilityInvocationTextTrigger = "textTrigger"
	CapabilitySourceLocalDB         = "local-db"

	CapabilityStatusAuthRequired  = "authRequired"
	CapabilityStatusAvailable     = "available"
	CapabilityStatusSetupRequired = "setupRequired"
	CapabilityStatusUnsupported   = "unsupported"
)

// Option is the host-neutral capability entry consumed by Agent composer
// projections. Provider-specific discovery may populate the optional identity
// fields while connector projection uses ConnectorKey-compatible Name values.
type Option struct {
	ID          string
	Kind        string
	Name        string
	Label       string
	IconURL     string
	Description string
	Status      string
	Source      string
	PluginName  string
	ServerName  string
	ToolName    string
	Trigger     string
	Path        string
	Invocation  string
}

// ConnectorOptions reads the application-owned Agent policy and only maps it
// into the host-neutral composer contract.
func ConnectorOptions(ctx context.Context, source application.AgentConnectorPolicyQueries, target contracts.AgentTarget) ([]Option, error) {
	if source == nil {
		return nil, nil
	}
	snapshot, err := source.Evaluate(ctx, target)
	if err != nil {
		return nil, err
	}
	return ProjectConnectorOptions(snapshot), nil
}

// ProjectConnectorOptions performs no business-state derivation.
func ProjectConnectorOptions(snapshot contracts.AgentConnectorPolicySnapshot) []Option {
	options := make([]Option, 0, len(snapshot.Connectors))
	for _, policy := range snapshot.Connectors {
		connector := policy.Connector
		key := strings.TrimSpace(connector.Key)
		if key == "" {
			continue
		}
		label := strings.TrimSpace(connector.Release.Manifest.DisplayName)
		if label == "" {
			label = key
		}
		options = append(options, Option{
			ID:          "connector:" + key,
			Kind:        CapabilityKindConnector,
			Name:        key,
			Label:       label,
			IconURL:     strings.TrimSpace(connector.Release.Manifest.IconURL),
			Description: strings.TrimSpace(connector.Release.Manifest.Description),
			Status:      composerStatus(policy.Presentation.State),
			Source:      CapabilitySourceLocalDB,
			Trigger:     "/" + key,
			Invocation:  CapabilityInvocationTextTrigger,
		})
	}
	return options
}

// ConnectorStatus maps the already-evaluated application state to the closed
// Agent composer vocabulary.
func composerStatus(state contracts.ConnectorState) string {
	switch state {
	case contracts.ConnectorStateConnected:
		return CapabilityStatusAvailable
	case contracts.ConnectorStateAuthorizationRequired:
		return CapabilityStatusAuthRequired
	case contracts.ConnectorStateSetupRequired:
		return CapabilityStatusSetupRequired
	default:
		return CapabilityStatusUnsupported
	}
}
