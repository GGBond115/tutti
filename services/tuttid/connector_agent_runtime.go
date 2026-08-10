package main

import (
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	connectorimplementation "github.com/tutti-os/tutti/packages/connector/runtime/implementationhost"
	connectormcpservice "github.com/tutti-os/tutti/services/tuttid/service/connectormcp"
)

// connectorAgentRuntime is the stable composition port shared by the Agent
// Service and Host. The route registry may change after construction, but the
// port itself is ready before Agent Host recovery begins.
type connectorAgentRuntime struct {
	routes *connectorimplementation.RouteRegistry
	server *connectormcpservice.Server
}

func (runtime *connectorAgentRuntime) RoutingHints() []runtimeprep.ConnectorRoutingHint {
	if runtime == nil || runtime.routes == nil {
		return nil
	}
	routes := runtime.routes.RoutingHints()
	hints := make([]runtimeprep.ConnectorRoutingHint, 0, len(routes))
	for _, route := range routes {
		hints = append(hints, runtimeprep.ConnectorRoutingHint{
			ConnectorKey: route.Key,
			DisplayName:  route.DisplayName,
			Aliases:      append([]string(nil), route.Aliases...),
			SkillRoot:    route.SkillRoot,
		})
	}
	return hints
}

func (runtime *connectorAgentRuntime) BindSession(workspaceID, agentSessionID string) (runtimeprep.MCPServerBinding, error) {
	binding, err := runtime.server.Binding(workspaceID, agentSessionID)
	if err != nil {
		return runtimeprep.MCPServerBinding{}, err
	}
	return runtimeprep.MCPServerBinding{
		Name: binding.Name, Type: binding.Type, URL: binding.URL, Headers: binding.Headers,
	}, nil
}

func (runtime *connectorAgentRuntime) RevokeSession(workspaceID, agentSessionID string) {
	if runtime != nil && runtime.server != nil {
		runtime.server.Revoke(workspaceID, agentSessionID)
	}
}

func (runtime *connectorAgentRuntime) RevokeAll() {
	if runtime != nil && runtime.server != nil {
		runtime.server.RevokeAll()
	}
}
