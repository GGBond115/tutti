package implementationhost

import connectorartifact "github.com/tutti-os/tutti/packages/connector/runtime/artifact"

type SkillSummary = connectorartifact.SkillSummary

type ConnectorSummary struct {
	Key         string                      `json:"key"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Skills      []SkillSummary              `json:"skills"`
	Interfaces  []ConnectorInterfaceSummary `json:"interfaces"`
}

type ConnectorInterfaceSummary struct {
	Kind       string `json:"kind"`
	ServerName string `json:"serverName,omitempty"`
	ToolPrefix string `json:"toolPrefix,omitempty"`
	Command    string `json:"command,omitempty"`
	Status     string `json:"status"`
}

// ConnectorRoutingHint is a bounded, non-secret projection of one active
// route for Agent runtime preparation.
type ConnectorRoutingHint struct {
	Key         string
	DisplayName string
	Aliases     []string
	SkillRoot   string
}

// ConnectorSummaries returns immutable discovery metadata already validated
// before each route was committed. It never rescans installed artifacts.
func (registry *RouteRegistry) ConnectorSummaries() []ConnectorSummary {
	routes := registry.Routes()
	connectors := make([]ConnectorSummary, 0, len(routes))
	for _, route := range routes {
		interfaces := make([]ConnectorInterfaceSummary, 0, 2)
		if route.HasMCP {
			interfaces = append(interfaces, ConnectorInterfaceSummary{Kind: "mcp", ServerName: "connector",
				ToolPrefix: route.ConnectorKey + "_", Status: "ready"})
		}
		if route.CLICommand != "" {
			interfaces = append(interfaces, ConnectorInterfaceSummary{Kind: "cli", Command: route.CLICommand, Status: "ready"})
		}
		connectors = append(connectors, ConnectorSummary{
			Key: route.ConnectorKey, Name: route.DisplayName, Description: route.Description,
			Skills: append([]SkillSummary(nil), route.Skills...), Interfaces: interfaces,
		})
	}
	return connectors
}

// RoutingHints returns a detached snapshot for Agent runtime preparation.
func (registry *RouteRegistry) RoutingHints() []ConnectorRoutingHint {
	routes := registry.Routes()
	hints := make([]ConnectorRoutingHint, 0, len(routes))
	for _, route := range routes {
		hints = append(hints, ConnectorRoutingHint{
			Key: route.ConnectorKey, DisplayName: route.DisplayName,
			Aliases: append([]string(nil), route.RoutingAliases...), SkillRoot: route.SkillRoot,
		})
	}
	return hints
}
