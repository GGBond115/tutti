package implementationhost

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/tutti-os/tutti/packages/connector/contracts"
	connectorruntime "github.com/tutti-os/tutti/packages/connector/runtime"
)

type RouteRegistry struct {
	mu       sync.RWMutex
	routes   *connectorruntime.RouteTable
	revision atomic.Uint64
}

type RouteDescriptor struct {
	ConnectionID         string
	ConnectorKey         string
	ConnectorVersion     string
	ReleaseDigest        string
	Generation           contracts.HostGeneration
	DisplayName          string
	Description          string
	RoutingAliases       []string
	SkillRoot            string
	Skills               []contracts.ConnectorSkillSummary
	HasMCP               bool
	CLICommand           string
	CLIInvocationCommand string
	CLIContractHash      string
	CLICommands          []contracts.CLICommand
	Readiness            contracts.RuntimeReadiness
}

func (descriptor RouteDescriptor) InterfaceState(kind string) contracts.RuntimeReadinessState {
	for _, readiness := range descriptor.Readiness.Interfaces {
		if readiness.Kind == kind {
			return readiness.State
		}
	}
	return contracts.RuntimeReadinessFailed
}

func NewRouteRegistry() *RouteRegistry { return &RouteRegistry{} }

func (registry *RouteRegistry) Revision() uint64 {
	if registry == nil {
		return 0
	}
	return registry.revision.Load()
}

func (registry *RouteRegistry) notifyChanged() {
	if registry != nil {
		registry.revision.Add(1)
	}
}

func (registry *RouteRegistry) attach(routes *connectorruntime.RouteTable) {
	registry.mu.Lock()
	registry.routes = routes
	registry.mu.Unlock()
}

func (registry *RouteRegistry) activeRoutes() []*connectorRoute {
	registry.mu.RLock()
	table := registry.routes
	registry.mu.RUnlock()
	if table == nil {
		return nil
	}
	portable := table.PublishedRoutes()
	routes := make([]*connectorRoute, 0, len(portable))
	for _, candidate := range portable {
		if route, ok := candidate.(*connectorRoute); ok {
			routes = append(routes, route)
		}
	}
	return routes
}

func (registry *RouteRegistry) Routes() []RouteDescriptor {
	routes := registry.activeRoutes()
	result := make([]RouteDescriptor, 0, len(routes))
	for _, route := range routes {
		result = append(result, routeDescriptor(route))
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].ConnectorKey != result[right].ConnectorKey {
			return result[left].ConnectorKey < result[right].ConnectorKey
		}
		return result[left].ConnectionID < result[right].ConnectionID
	})
	return result
}

func routeDescriptor(route *connectorRoute) RouteDescriptor {
	return RouteDescriptor{ConnectionID: route.connectionID, ConnectorKey: route.connectorKey,
		ConnectorVersion: route.connectorVersion, ReleaseDigest: route.releaseDigest, Generation: route.generation,
		DisplayName: route.displayName,
		Description: route.description, RoutingAliases: append([]string(nil), route.routingAliases...),
		SkillRoot: route.skillRoot, Skills: append([]contracts.ConnectorSkillSummary(nil), route.skills...),
		HasMCP: len(route.mcpTools) > 0, CLICommand: route.cliCommand, CLIInvocationCommand: route.cliInvocationCommand,
		CLIContractHash: route.cliContractHash,
		CLICommands:     cloneCLICommands(route.cliCommands),
		Readiness:       cloneRuntimeReadiness(route.readiness)}
}

func cloneCLICommands(commands []contracts.CLICommand) []contracts.CLICommand {
	result := make([]contracts.CLICommand, 0, len(commands))
	for _, command := range commands {
		cloned := command
		cloned.Arguments = append([]string(nil), command.Arguments...)
		cloned.InputSchema = cloneJSONMap(command.InputSchema)
		result = append(result, cloned)
	}
	return result
}
