// Package implementationhost owns the host-neutral managed Connector runtime.
// Products provide artifact, runtime, process, and credential ports and expose
// immutable route projections through their native CLI and Agent runtimes.
package implementationhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
	connectorruntime "github.com/tutti-os/tutti/packages/connector/runtime"
	connectorartifact "github.com/tutti-os/tutti/packages/connector/runtime/artifact"
	"github.com/tutti-os/tutti/packages/connector/runtime/command"
	"github.com/tutti-os/tutti/packages/connector/runtime/mcp"
	connectorprocess "github.com/tutti-os/tutti/packages/connector/runtime/process"
)

type PreparedArtifactResolver interface {
	ResolvePrepared(context.Context, contracts.Release) (contracts.PreparedArtifactReceipt, error)
}

type ReconcileRequest struct {
	Runtime contracts.RuntimeReconcileRequest
}

// RemoteMCPClient is the protocol client required by one remote Connector
// route. Products decide whether the client connects directly to their MCP
// Gateway or through a product-owned relay.
type RemoteMCPClient interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	RegisterTool(string, map[string]any) error
	ReplaceTools(map[string]map[string]any) error
	Close(context.Context) error
}

// RemoteMCPClientRequest carries the complete non-credential identity of one
// remote Connector route. A product adapter may use it to bind a relay request
// to the lifecycle operation without exposing account credentials to the
// runtime machine.
type RemoteMCPClientRequest struct {
	OperationID    string
	ConnectionID   string
	ConnectorKey   string
	AccountID      string
	ReleaseDigest  string
	Version        string
	Generation     contracts.HostGeneration
	Implementation contracts.RemoteStreamableHTTPImplementation
}

// RemoteMCPClientFactory is the product port for remote Connector MCP
// connectivity. ImplementationHost owns protocol bootstrap and route
// lifecycle; the factory owns the physical connection path and request
// authorization.
type RemoteMCPClientFactory interface {
	NewRemoteMCPClient(context.Context, RemoteMCPClientRequest) (RemoteMCPClient, error)
}

type Config struct {
	Artifacts              PreparedArtifactResolver
	CLIInstallations       application.CLIInstallationManager
	Runtimes               connectorruntime.ConnectorRuntimeResolver
	Processes              connectorprocess.Transport
	Authorization          AuthorizationObserver
	Registry               *RouteRegistry
	MCP                    *MCPRegistry
	StateRoot              string
	BinDir                 string
	UserHome               string
	MCPStartupTimeout      time.Duration
	RemoteMCPClientFactory RemoteMCPClientFactory
}

type Host struct {
	admission              sync.RWMutex
	laneMu                 sync.Mutex
	connectorLanes         map[string]*sync.Mutex
	artifacts              PreparedArtifactResolver
	planner                *connectorruntime.ManagedRoutePlanner
	processes              connectorprocess.Transport
	observations           *routeObservationHub
	routeObservationMu     sync.RWMutex
	authorizationObserver  AuthorizationObserver
	mcpStartupTimeout      time.Duration
	routes                 *connectorruntime.RouteTable
	snapshots              *connectorruntime.ExecutionSnapshotter
	authorizationProvider  *managedCredentialAuthorizationProvider
	authorizationMu        sync.Mutex
	authorizationRoutes    map[string]*connectorRoute
	remoteMCPClientFactory RemoteMCPClientFactory
	mcpRegistry            *MCPRegistry
	registry               *RouteRegistry
	binDir                 string
}

type connectorRoute struct {
	id                     string
	connectionID           string
	connectorKey           string
	connectorVersion       string
	releaseDigest          string
	generation             contracts.HostGeneration
	mcpTools               map[string]registeredMCPTool
	closeMu                sync.Mutex
	mcpClient              *mcp.StdioClient
	remoteMCP              RemoteMCPClient
	executionRoot          string
	installedRoot          string
	displayName            string
	description            string
	routingAliases         []string
	skillRoot              string
	skills                 []contracts.ConnectorSkillSummary
	processes              *connectorprocess.Group
	snapshots              *connectorruntime.ExecutionSnapshotter
	userHome               string
	cliLaunch              *managedCLILaunch
	cliCommand             string
	cliInvocationCommand   string
	cliContractHash        string
	cliCommands            []contracts.CLICommand
	cliShimPath            string
	cliShimContent         []byte
	credentialBrokerLaunch *managedCredentialBrokerLaunch
	readiness              contracts.RuntimeReadiness
}

func New(config Config) (*Host, error) {
	if config.Artifacts == nil || config.Runtimes == nil || config.Processes == nil || config.Registry == nil || config.MCP == nil {
		return nil, errors.New("connector implementation host dependencies are required")
	}
	if !filepath.IsAbs(strings.TrimSpace(config.StateRoot)) {
		return nil, errors.New("connector implementation state root must be absolute")
	}
	if !filepath.IsAbs(strings.TrimSpace(config.UserHome)) {
		return nil, errors.New("connector implementation user home must be absolute")
	}
	if strings.TrimSpace(config.BinDir) == "" {
		config.BinDir = filepath.Join(config.StateRoot, "bin")
	}
	if !filepath.IsAbs(config.BinDir) {
		return nil, errors.New("connector CLI bin directory must be absolute")
	}
	if config.MCPStartupTimeout <= 0 {
		config.MCPStartupTimeout = 15 * time.Second
	}
	snapshots, err := connectorruntime.NewExecutionSnapshotter(config.StateRoot)
	if err != nil {
		return nil, err
	}
	if err := snapshots.CleanupOrphans(); err != nil {
		return nil, fmt.Errorf("clean orphaned connector execution snapshots: %w", err)
	}
	routes := connectorruntime.NewRouteTable()
	planner, err := connectorruntime.NewManagedRoutePlanner(connectorruntime.ManagedRoutePlannerConfig{
		StateRoot: config.StateRoot, UserHome: config.UserHome, Runtimes: config.Runtimes, CLIInstallations: config.CLIInstallations,
	})
	if err != nil {
		return nil, err
	}
	config.Registry.attach(routes)
	config.MCP.attach(routes)
	host := &Host{
		artifacts:              config.Artifacts,
		planner:                planner,
		processes:              config.Processes,
		observations:           newRouteObservationHub(),
		authorizationObserver:  config.Authorization,
		mcpStartupTimeout:      config.MCPStartupTimeout,
		routes:                 routes,
		snapshots:              snapshots,
		authorizationRoutes:    make(map[string]*connectorRoute),
		connectorLanes:         make(map[string]*sync.Mutex),
		remoteMCPClientFactory: config.RemoteMCPClientFactory,
		mcpRegistry:            config.MCP,
		registry:               config.Registry,
		binDir:                 config.BinDir,
	}
	host.authorizationProvider = newManagedCredentialAuthorizationProvider(host)
	return host, nil
}

func (host *Host) Reconcile(ctx context.Context, request ReconcileRequest) (contracts.RuntimeReceipt, error) {
	runtimeRequest := request.Runtime
	if host == nil || !hostIdentityPattern.MatchString(runtimeRequest.ConnectionID) ||
		!hostIdentityPattern.MatchString(runtimeRequest.Connector.Key) || runtimeRequest.Generation.BootEpoch == "" || runtimeRequest.Generation.Generation == 0 {
		return contracts.RuntimeReceipt{}, errors.New("connector runtime reconcile identity is invalid")
	}
	releaseLane := host.enterConnectorLane(runtimeRequest.Connector.Key)
	defer releaseLane()
	if !runtimeRequest.Enabled {
		// A connection id can rotate while an older account route is still
		// present. Disabled is connector-level desired state, so convergence must
		// fence every route and authorization session for the connector instead
		// of removing only the latest route key.
		if err := host.deactivateConnector(contracts.RuntimeDeactivationRequest{
			ConnectionID:   runtimeRequest.ConnectionID,
			ConnectorKey:   runtimeRequest.Connector.Key,
			AllConnections: true,
			Generation:     runtimeRequest.Generation,
			Deadline:       time.Now().Add(3 * time.Second),
		}); err != nil {
			return contracts.RuntimeReceipt{}, err
		}
		return contracts.RuntimeReceipt{OperationID: runtimeRequest.OperationID, ConnectionID: runtimeRequest.ConnectionID,
			ConnectorKey: runtimeRequest.Connector.Key, ReleaseDigest: runtimeRequest.Connector.Release.ReleaseDigest,
			Generation: runtimeRequest.Generation,
			Readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessBlocked,
				ReasonCode: contracts.RuntimeReadinessReasonRuntimeDisabled}}, nil
	}
	if err := contracts.ValidateRuntimeReleaseShape(runtimeRequest.Connector.Release); err != nil {
		return contracts.RuntimeReceipt{}, err
	}
	key := connectorRouteKey(runtimeRequest.ConnectionID, runtimeRequest.Connector.Key)
	if !installationTargetsRelease(runtimeRequest.Connector.Installation, runtimeRequest.Connector.Release.ReleaseDigest) {
		return contracts.RuntimeReceipt{}, errors.New("connector installed release is not active")
	}
	if err := host.validateAuthorization(runtimeRequest); err != nil {
		return contracts.RuntimeReceipt{}, err
	}
	var route *connectorRoute
	var err error
	if runtimeRequest.Connector.Release.Manifest.Implementation.Kind == contracts.ImplementationKindRemoteStreamableHTTP {
		route, err = host.buildRemoteRoute(ctx, runtimeRequest)
		if err == nil {
			prepared, resolveErr := host.artifacts.ResolvePrepared(ctx, runtimeRequest.Connector.Release)
			if resolveErr != nil {
				_ = route.Close(time.Now().Add(3 * time.Second))
				return contracts.RuntimeReceipt{}, fmt.Errorf("resolve remote connector artifact: %w", resolveErr)
			}
			route.installedRoot = prepared.PreparedPath
		}
	} else {
		prepared, resolveErr := host.artifacts.ResolvePrepared(ctx, runtimeRequest.Connector.Release)
		if resolveErr != nil {
			return contracts.RuntimeReceipt{}, fmt.Errorf("resolve prepared connector artifact: %w", resolveErr)
		}
		installedRoot := prepared.PreparedPath
		executionRoot, snapshotErr := host.snapshots.Create(prepared, artifactNativeEntrypoints(runtimeRequest.Connector.Release)...)
		if snapshotErr != nil {
			return contracts.RuntimeReceipt{}, fmt.Errorf("create connector execution snapshot: %w", snapshotErr)
		}
		prepared.PreparedPath = executionRoot
		route, err = host.buildManagedRoute(ctx, runtimeRequest, prepared)
		if err != nil {
			_ = host.snapshots.Remove(executionRoot)
			return contracts.RuntimeReceipt{}, err
		}
		route.executionRoot, route.installedRoot = executionRoot, installedRoot
	}
	if err != nil {
		return contracts.RuntimeReceipt{}, err
	}
	route.displayName = runtimeRequest.Connector.Release.Manifest.DisplayName
	route.description = runtimeRequest.Connector.Release.Manifest.Description
	if routing := runtimeRequest.Connector.Release.Manifest.AgentRouting; routing != nil {
		route.routingAliases = append([]string(nil), routing.Aliases...)
	}
	route.snapshots = host.snapshots
	skillProjection, err := connectorartifact.InspectSkills(route.installedRoot)
	if err != nil {
		_ = route.Close(time.Now().Add(3 * time.Second))
		return contracts.RuntimeReceipt{}, fmt.Errorf("inspect connector Skills: %w", err)
	}
	route.skillRoot = skillProjection.Root
	route.skills = append([]contracts.ConnectorSkillSummary(nil), skillProjection.Skills...)
	if route.readiness.State == "" {
		route.readiness = readyRuntimeReadiness(route)
	}
	previous, _ := host.routes.Route(key).(*connectorRoute)
	if err := route.activateCLIShim(); err != nil {
		_ = route.Close(time.Now().Add(3 * time.Second))
		return contracts.RuntimeReceipt{}, err
	}
	host.routeObservationMu.Lock()
	commitErr := host.routes.Commit(route)
	committed := host.routes.IsCurrent(route)
	if committed {
		host.observations.publish(contracts.PhysicalRouteEventChanged, physicalRoute(route))
	}
	host.routeObservationMu.Unlock()
	if commitErr != nil {
		if committed {
			host.notifyRouteChanged()
		}
		if !committed {
			if previous != nil {
				_ = previous.activateCLIShim()
			} else {
				route.removeCLIShimIfCurrent()
			}
			_ = route.Close(time.Now().Add(3 * time.Second))
		}
		return contracts.RuntimeReceipt{}, commitErr
	}
	host.releaseAuthorizationRouteByKey(key)
	host.notifyRouteChanged()
	if route.mcpClient != nil {
		go host.monitorMCPRoute(route, route.mcpClient)
	}
	summary := connectorSummaryFromDescriptor(routeDescriptor(route))
	return contracts.RuntimeReceipt{OperationID: runtimeRequest.OperationID, ConnectionID: runtimeRequest.ConnectionID,
		ConnectorKey: runtimeRequest.Connector.Key, ReleaseDigest: route.releaseDigest,
		Generation: runtimeRequest.Generation, Readiness: cloneRuntimeReadiness(route.readiness), Summary: &summary}, nil
}

func installationTargetsRelease(installation contracts.Installation, releaseDigest string) bool {
	switch installation.State {
	case contracts.InstallationStateInstalled:
		return installation.InstalledReleaseDigest == releaseDigest
	case contracts.InstallationStateInstalling, contracts.InstallationStateUpdating:
		return installation.CandidateReleaseDigest == releaseDigest
	default:
		return false
	}
}

func (*Host) validateAuthorization(request contracts.RuntimeReconcileRequest) error {
	authKind := request.Connector.Release.Manifest.AuthorizationKind
	if request.Connector.Release.Manifest.Implementation.Kind == contracts.ImplementationKindRemoteStreamableHTTP {
		if authKind == "none" {
			if request.Connector.Authorization.State != contracts.AuthorizationStateNotRequired {
				return errors.New("authorization-free connector has an invalid credential binding")
			}
			return nil
		}
		if request.Connector.Authorization.State != contracts.AuthorizationStateConnected {
			return errors.New("authorized remote connector is not connected")
		}
		return nil
	}
	managed := request.Connector.Release.Manifest.Implementation.ManagedStdio
	if authKind == "none" {
		if request.Connector.Authorization.State != contracts.AuthorizationStateNotRequired {
			return errors.New("authorization-free connector has an invalid credential binding")
		}
		return nil
	}
	if managed == nil || managed.CredentialBroker == nil {
		return errors.New("authorized connector credential broker binding is unavailable")
	}
	if request.Connector.Authorization.State != contracts.AuthorizationStateConnected {
		return errors.New("authorized managed connector is not connected")
	}
	return nil
}

func newConnectorRoute(request contracts.RuntimeReconcileRequest) *connectorRoute {
	return &connectorRoute{id: connectorRouteKey(request.ConnectionID, request.Connector.Key), connectionID: request.ConnectionID,
		connectorKey: request.Connector.Key, connectorVersion: request.Connector.Release.Version,
		releaseDigest: request.Connector.Release.ReleaseDigest,
		generation:    request.Generation, mcpTools: make(map[string]registeredMCPTool),
		processes: connectorprocess.NewGroup()}
}

// Snapshot returns bounded, level-triggered physical route truth. It is
// independent from capability publication and excludes fenced retiring routes.
func (host *Host) Snapshot(ctx context.Context) (contracts.PhysicalRouteSnapshot, error) {
	if host == nil || host.routes == nil || host.observations == nil {
		return contracts.PhysicalRouteSnapshot{}, errors.New("connector physical route observation is unavailable")
	}
	if ctx == nil {
		return contracts.PhysicalRouteSnapshot{}, errors.New("connector physical route snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return contracts.PhysicalRouteSnapshot{}, err
	}
	host.routeObservationMu.RLock()
	defer host.routeObservationMu.RUnlock()
	active := host.routes.ActiveRoutes()
	if len(active) > maxPhysicalRouteSnapshot {
		return contracts.PhysicalRouteSnapshot{}, errors.New("connector physical route snapshot exceeds limit")
	}
	routes := make([]contracts.PhysicalRoute, 0, len(active))
	for _, route := range active {
		candidate, ok := route.(*connectorRoute)
		if !ok {
			return contracts.PhysicalRouteSnapshot{}, errors.New("connector physical route has an unsupported implementation")
		}
		routes = append(routes, physicalRoute(candidate))
	}
	sortPhysicalRoutes(routes)
	return contracts.PhysicalRouteSnapshot{Revision: host.observations.currentRevision(), Routes: routes}, nil
}

func (host *Host) Watch(ctx context.Context) (contracts.PhysicalRouteWatch, error) {
	if host == nil || host.observations == nil {
		return contracts.PhysicalRouteWatch{}, errors.New("connector physical route observation is unavailable")
	}
	return host.observations.watch(ctx)
}

func (host *Host) Close() error {
	if host == nil {
		return nil
	}
	deadline := time.Now().Add(3 * time.Second)
	host.admission.Lock()
	defer host.admission.Unlock()
	host.authorizationMu.Lock()
	authorizationRoutes := make([]*connectorRoute, 0, len(host.authorizationRoutes))
	for key, route := range host.authorizationRoutes {
		delete(host.authorizationRoutes, key)
		authorizationRoutes = append(authorizationRoutes, route)
	}
	host.authorizationMu.Unlock()
	host.routeObservationMu.Lock()
	physicalRoutes := host.routes.ActiveRoutes()
	routeCloseErr := host.routes.Close(deadline)
	for _, route := range physicalRoutes {
		if candidate, ok := route.(*connectorRoute); ok {
			host.observations.publish(contracts.PhysicalRouteEventChanged, physicalRoute(candidate))
		}
	}
	host.observations.close()
	host.routeObservationMu.Unlock()
	errs := []error{routeCloseErr}
	for _, route := range authorizationRoutes {
		route.Fence()
		errs = append(errs, route.Close(deadline))
	}
	return errors.Join(errs...)
}

func (host *Host) SetCapabilityPublication(enabled bool) {
	if host != nil {
		host.routes.SetPublished(enabled)
		host.notifyRouteChanged()
	}
}

func (host *Host) FenceAll(_ context.Context, deadline time.Time) error {
	if host == nil {
		return nil
	}
	host.admission.Lock()
	defer host.admission.Unlock()
	host.routeObservationMu.Lock()
	routes := host.routes.ActiveRoutes()
	err := host.routes.FenceAll(deadline)
	host.notifyRouteChanged()
	for _, route := range routes {
		if candidate, ok := route.(*connectorRoute); ok {
			host.observations.publish(contracts.PhysicalRouteEventChanged, physicalRoute(candidate))
		}
	}
	host.routeObservationMu.Unlock()
	return err
}

func (host *Host) FailClosed(ctx context.Context, deadline time.Time) error {
	if host == nil {
		return nil
	}
	host.SetCapabilityPublication(false)
	return host.FenceAll(ctx, deadline)
}

func (host *Host) DeactivateRuntime(ctx context.Context, request contracts.RuntimeDeactivationRequest) error {
	if host == nil {
		return errors.New("connector implementation host is unavailable")
	}
	if !request.Deadline.IsZero() && time.Now().After(request.Deadline) {
		return context.DeadlineExceeded
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	releaseLane := host.enterConnectorLane(request.ConnectorKey)
	defer releaseLane()
	if request.AllConnections {
		return host.deactivateConnector(request)
	}
	key := connectorRouteKey(request.ConnectionID, request.ConnectorKey)
	host.routeObservationMu.Lock()
	removed, _ := host.routes.Route(key).(*connectorRoute)
	err := host.routes.Remove(key, request.Generation, request.ReleaseDigest, request.Deadline)
	host.notifyRouteChanged()
	if removed != nil && !host.routes.IsCurrent(removed) {
		host.observations.publish(contracts.PhysicalRouteEventChanged, physicalRoute(removed))
	}
	host.routeObservationMu.Unlock()
	return err
}

func (host *Host) enterConnectorLane(connectorKey string) func() {
	host.admission.RLock()
	host.laneMu.Lock()
	if host.connectorLanes == nil {
		host.connectorLanes = make(map[string]*sync.Mutex)
	}
	lane := host.connectorLanes[connectorKey]
	if lane == nil {
		lane = &sync.Mutex{}
		host.connectorLanes[connectorKey] = lane
	}
	host.laneMu.Unlock()
	lane.Lock()
	return func() {
		lane.Unlock()
		host.admission.RUnlock()
	}
}

func (host *Host) buildManagedRoute(ctx context.Context, request contracts.RuntimeReconcileRequest,
	prepared contracts.PreparedArtifactReceipt) (*connectorRoute, error) {
	plan, err := host.planner.Build(ctx, request, prepared)
	if err != nil {
		return nil, err
	}
	route := newConnectorRoute(request)
	route.userHome = plan.UserHome
	if plan.Managed.MCP != nil {
		if err := host.attachMCP(ctx, route, plan.Managed, prepared, plan.Executable, plan.StateDir, plan.UserHome, plan.ArtifactTrees); err != nil {
			_ = route.close(time.Now().Add(3 * time.Second))
			return nil, err
		}
	}
	if plan.Managed.CLI != nil {
		if err := host.attachCLI(route, plan.Managed, prepared, plan.InstalledCLI, plan.Executable, plan.StateDir, plan.ArtifactTrees); err != nil {
			_ = route.close(time.Now().Add(3 * time.Second))
			return nil, err
		}
		if err := route.prepareCLIShim(host.binDir); err != nil {
			_ = route.close(time.Now().Add(3 * time.Second))
			return nil, err
		}
		if err := host.checkCLIReadiness(ctx, route, plan.Managed.CLI.ReadinessProbe); err != nil {
			_ = route.close(time.Now().Add(3 * time.Second))
			return nil, fmt.Errorf("check connector CLI readiness: %w", err)
		}
	}
	if plan.Managed.CredentialBroker != nil {
		if err := host.attachCredentialBroker(route, plan.Managed.CredentialBroker, prepared, plan.Executable, plan.StateDir, plan.ArtifactTrees); err != nil {
			_ = route.close(time.Now().Add(3 * time.Second))
			return nil, err
		}
	}
	if route.cliLaunch == nil && len(route.mcpTools) == 0 {
		_ = route.close(time.Now().Add(3 * time.Second))
		return nil, errors.New("connector implementation exposed no MCP tools or CLI commands")
	}
	route.readiness = readyRuntimeReadiness(route)
	return route, nil
}

var mcpLocalToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]*$`)
var hostIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,190}$`)

func (*Host) attachCredentialBroker(route *connectorRoute, broker *contracts.ManagedCredentialBroker,
	prepared contracts.PreparedArtifactReceipt, executable connectorruntime.ConnectorExecutable,
	stateDir string, artifactTrees []connectorprocess.ArtifactTreeIdentity) error {
	if route.cliLaunch == nil {
		return errors.New("connector credential broker requires a managed CLI")
	}
	entrypoint, err := connectorruntime.PreparedEntrypoint(prepared.PreparedPath, broker.Entrypoint)
	if err != nil {
		return fmt.Errorf("resolve connector credential broker entrypoint: %w", err)
	}
	allowedHosts := make(map[string]struct{}, len(broker.AllowedHosts))
	for _, allowedHost := range broker.AllowedHosts {
		allowedHosts[strings.ToLower(strings.TrimSpace(allowedHost))] = struct{}{}
	}
	route.credentialBrokerLaunch = &managedCredentialBrokerLaunch{
		entrypoint: entrypoint, timeout: time.Duration(broker.TimeoutMS) * time.Millisecond, allowedHosts: allowedHosts,
		cliLaunch: credentialBrokerCLILaunch{Executable: route.cliLaunch.executable.Path,
			// Native CLIs legitimately have no argv; preserve [] instead of encoding null for the broker protocol.
			Arguments: append([]string{}, route.cliLaunch.arguments...), CWD: route.cliLaunch.cwd},
		executable: executable, language: route.cliLaunch.language, cwd: prepared.PreparedPath, stateDir: stateDir,
		artifactTrees: append([]connectorprocess.ArtifactTreeIdentity(nil), artifactTrees...),
	}
	return nil
}

func (host *Host) attachMCP(ctx context.Context, route *connectorRoute, managed *contracts.ManagedStdioImplementation,
	prepared contracts.PreparedArtifactReceipt, executable connectorruntime.ConnectorExecutable,
	stateDir, userHome string, artifactTrees []connectorprocess.ArtifactTreeIdentity) error {
	entrypoint, err := connectorruntime.PreparedEntrypoint(prepared.PreparedPath, managed.MCP.Entrypoint)
	if err != nil {
		return err
	}
	startupContext, cancelStartup := context.WithTimeout(ctx, host.mcpStartupTimeout)
	defer cancelStartup()
	spec := connectorruntime.ConnectorProcessSpec(route.connectionID, route.connectorKey, managed.Runtime.Language,
		executable, prepared.PreparedPath, append([]string{entrypoint}, managed.MCP.Arguments...), stateDir, userHome, artifactTrees)
	connection, processID, err := host.startProcess(startupContext, route, spec, false)
	if err != nil {
		return fmt.Errorf("start connector MCP process: %w", err)
	}
	release := func() { _ = route.releaseProcess(processID, connection) }
	client, err := mcp.NewStdioClient(mcp.StdioClientConfig{Connection: connection, ProcessName: route.connectorKey + " MCP"})
	if err != nil {
		release()
		return err
	}
	if _, err := client.Call(startupContext, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "tutti-connector-host", "version": "1"}}); err != nil {
		release()
		return fmt.Errorf("initialize connector MCP process: %w", err)
	}
	if err := client.Notify("notifications/initialized", map[string]any{}); err != nil {
		release()
		return err
	}
	tools, err := listMCPTools(startupContext, client)
	if err != nil {
		release()
		return err
	}
	if len(tools) == 0 {
		release()
		return errors.New("connector MCP tools/list response is invalid")
	}
	if err := host.registerMCPTools(route, client, tools); err != nil {
		release()
		return err
	}
	route.mcpClient = client
	return nil
}

func readyRuntimeReadiness(route *connectorRoute) contracts.RuntimeReadiness {
	interfaces := make([]contracts.InterfaceReadiness, 0, 2)
	if len(route.mcpTools) > 0 {
		routeIDs := make([]string, 0, len(route.mcpTools))
		for _, tool := range route.mcpTools {
			routeIDs = append(routeIDs, tool.routeID)
		}
		sort.Strings(routeIDs)
		interfaces = append(interfaces, contracts.InterfaceReadiness{Kind: "mcp", State: contracts.RuntimeReadinessReady, RouteIDs: routeIDs})
	}
	if route.cliLaunch != nil {
		interfaces = append(interfaces, contracts.InterfaceReadiness{Kind: "cli", State: contracts.RuntimeReadinessReady})
	}
	return contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady, Interfaces: interfaces}
}

func cloneRuntimeReadiness(readiness contracts.RuntimeReadiness) contracts.RuntimeReadiness {
	cloned := readiness
	cloned.Interfaces = append([]contracts.InterfaceReadiness(nil), readiness.Interfaces...)
	for index := range cloned.Interfaces {
		cloned.Interfaces[index].RouteIDs = append([]string(nil), readiness.Interfaces[index].RouteIDs...)
	}
	return cloned
}

func (host *Host) notifyRouteChanged() {
	if host == nil {
		return
	}
	host.registry.notifyChanged()
	host.mcpRegistry.notifyChanged()
}

func (host *Host) monitorMCPRoute(route *connectorRoute, client *mcp.StdioClient) {
	<-client.Done()
	host.routeObservationMu.Lock()
	_ = host.routes.RetireExact(route, time.Now().Add(3*time.Second), func() {
		host.observations.publish(contracts.PhysicalRouteEventUnexpectedExit, physicalRoute(route))
	})
	host.routeObservationMu.Unlock()
	host.notifyRouteChanged()
}

func (*Host) attachCLI(route *connectorRoute, managed *contracts.ManagedStdioImplementation,
	prepared contracts.PreparedArtifactReceipt, installed *contracts.CLIInstallationReceipt,
	executable connectorruntime.ConnectorExecutable, stateDir string, artifactTrees []connectorprocess.ArtifactTreeIdentity) error {
	entrypointRoot, entrypointRelative := prepared.PreparedPath, managed.CLI.Entrypoint
	if installed != nil {
		entrypointRoot, entrypointRelative = installed.InstallRoot, installed.Entrypoint
	}
	entrypoint, err := connectorruntime.PreparedEntrypoint(entrypointRoot, entrypointRelative)
	if err != nil {
		return err
	}
	contractHash, err := contracts.ManagedCLIContractHash(*managed.CLI)
	if err != nil {
		return fmt.Errorf("hash connector CLI contract: %w", err)
	}
	launchArguments := []string{entrypoint}
	launchExecutable := executable
	if managed.CLI.Launch != nil && managed.CLI.Launch.Kind == contracts.CLIArtifactLaunchKindNative {
		launchArguments = nil
		launchExecutable = connectorruntime.ConnectorExecutable{Path: entrypoint, SHA256: managed.CLI.Launch.SHA256,
			SizeBytes: managed.CLI.Launch.SizeBytes}
	} else if installed != nil && installed.LaunchKind == "native" {
		launchArguments = nil
		launchExecutable = connectorruntime.ConnectorExecutable{Path: entrypoint, SHA256: installed.EntrypointSHA256,
			SizeBytes: installed.EntrypointSize}
	}
	route.cliLaunch = &managedCLILaunch{arguments: append(append([]string{}, launchArguments...), managed.CLI.Arguments...),
		artifactTrees: append([]connectorprocess.ArtifactTreeIdentity(nil), artifactTrees...), cwd: prepared.PreparedPath,
		executable: launchExecutable, language: managed.Runtime.Language, stateDir: stateDir,
		timeout: time.Duration(managed.CLI.TimeoutMS) * time.Millisecond}
	route.cliContractHash = contractHash
	route.cliInvocationCommand = contracts.ManagedCLICommandName(*managed.CLI)
	route.cliCommands = cloneCLICommands(managed.CLI.Commands)
	return nil
}

func artifactNativeEntrypoints(release contracts.Release) []string {
	managed := release.Manifest.Implementation.ManagedStdio
	if managed == nil || managed.CLI == nil || managed.CLI.Launch == nil ||
		managed.CLI.Launch.Kind != contracts.CLIArtifactLaunchKindNative {
		return nil
	}
	return []string{managed.CLI.Entrypoint}
}

func (host *Host) startProcess(ctx context.Context, route *connectorRoute, spec connectorprocess.Spec,
	requireCurrent bool) (connectorprocess.Connection, uint64, error) {
	if requireCurrent && !host.routes.IsCurrent(route) {
		return nil, 0, command.ErrServiceUnavailable
	}
	startContext, processID, accepted := route.processes.Begin(context.Background())
	if !accepted {
		return nil, 0, command.ErrServiceUnavailable
	}
	type startResult struct {
		connection connectorprocess.Connection
		err        error
	}
	result := make(chan startResult, 1)
	go func() {
		connection, startErr := host.processes.Start(startContext, spec)
		result <- startResult{connection: connection, err: startErr}
	}()
	select {
	case started := <-result:
		if started.err != nil {
			route.processes.FailStart(processID)
			return nil, 0, started.err
		}
		if !route.processes.CommitStart(processID, started.connection) {
			_ = started.connection.Close()
			return nil, 0, command.ErrServiceUnavailable
		}
		return started.connection, processID, nil
	case <-ctx.Done():
		route.processes.FailStart(processID)
		go func() {
			started := <-result
			if started.connection != nil {
				_ = started.connection.Close()
			}
		}()
		return nil, 0, ctx.Err()
	}
}

func connectorRouteKey(connectionID, connectorKey string) string {
	return connectionID + "\x00" + connectorKey
}

func (host *Host) routeCurrent(route *connectorRoute) bool {
	return host.routes.IsCurrent(route) && !route.processes.IsFenced()
}

func (route *connectorRoute) RouteID() string                           { return route.id }
func (route *connectorRoute) RouteGeneration() contracts.HostGeneration { return route.generation }
func (route *connectorRoute) RouteReleaseDigest() string                { return route.releaseDigest }
func (route *connectorRoute) Fence()                                    { route.processes.Fence() }
func (route *connectorRoute) close(deadline time.Time) error            { return route.Close(deadline) }
func (route *connectorRoute) releaseProcess(id uint64, connection connectorprocess.Connection) error {
	if route != nil && route.processes != nil {
		return route.processes.ReleaseWithError(id, connection)
	}
	return nil
}

func (route *connectorRoute) Close(deadline time.Time) error {
	if route == nil {
		return nil
	}
	route.closeMu.Lock()
	defer route.closeMu.Unlock()
	route.removeCLIShimIfCurrent()
	var remoteErr error
	if route.remoteMCP != nil {
		closeCtx, cancel := context.WithDeadline(context.Background(), deadline)
		remoteErr = route.remoteMCP.Close(closeCtx)
		cancel()
	}
	closeErr := route.processes.Close(deadline)
	if closeErr == nil {
		closeErr = remoteErr
	}
	if closeErr == nil && route.executionRoot != "" {
		if err := route.snapshots.Remove(route.executionRoot); err != nil {
			closeErr = err
		} else {
			route.executionRoot = ""
		}
	}
	return closeErr
}

func (route *connectorRoute) prepareCLIShim(binDir string) error {
	if route == nil || route.cliLaunch == nil {
		return nil
	}
	command := "tutti-connector-" + route.connectorKey
	if runtime.GOOS == "windows" {
		command += ".cmd"
	}
	path := filepath.Join(binDir, command)
	content, err := connectorCLIShimContent(route)
	if err != nil {
		return err
	}
	route.cliCommand, route.cliShimPath, route.cliShimContent = command, path, content
	return nil
}

func (route *connectorRoute) activateCLIShim() error {
	if route == nil || route.cliShimPath == "" || len(route.cliShimContent) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(route.cliShimPath), 0o700); err != nil {
		return fmt.Errorf("create connector CLI bin directory: %w", err)
	}
	temporary := route.cliShimPath + ".tmp-" + fmt.Sprintf("%d", route.generation.Generation)
	if err := os.WriteFile(temporary, route.cliShimContent, 0o700); err != nil {
		return fmt.Errorf("write connector CLI shim: %w", err)
	}
	if err := os.Rename(temporary, route.cliShimPath); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("activate connector CLI shim: %w", err)
	}
	return nil
}

func (route *connectorRoute) removeCLIShimIfCurrent() {
	if route == nil || route.cliShimPath == "" {
		return
	}
	current, err := os.ReadFile(route.cliShimPath)
	if err == nil && string(current) == string(route.cliShimContent) {
		_ = os.Remove(route.cliShimPath)
	}
}

func connectorCLIShimContent(route *connectorRoute) ([]byte, error) {
	launch := route.cliLaunch
	if launch == nil || strings.TrimSpace(launch.executable.Path) == "" {
		return nil, errors.New("connector CLI launch is unavailable")
	}
	arguments := append([]string(nil), launch.arguments...)
	if runtime.GOOS == "windows" {
		values := append([]string{launch.executable.Path}, arguments...)
		for _, value := range append(values, launch.cwd, launch.stateDir, route.userHome) {
			if strings.ContainsAny(value, "\r\n\"") {
				return nil, errors.New("connector CLI path cannot be represented by Windows shim")
			}
		}
		quoted := make([]string, 0, len(values))
		for _, value := range values {
			quoted = append(quoted, `"`+value+`"`)
		}
		content := "@echo off\r\n" +
			"set \"TUTTI_CONNECTOR_CONNECTION_ID=" + route.connectionID + "\"\r\n" +
			"set \"TUTTI_CONNECTOR_KEY=" + route.connectorKey + "\"\r\n" +
			"set \"TUTTI_CONNECTOR_LANGUAGE=" + launch.language + "\"\r\n" +
			"set \"TUTTI_CONNECTOR_STATE_DIR=" + launch.stateDir + "\"\r\n" +
			"set \"HOME=" + route.userHome + "\"\r\n" +
			"set \"USERPROFILE=" + route.userHome + "\"\r\n" +
			"cd /d \"" + launch.cwd + "\"\r\n" + strings.Join(quoted, " ") + " %*\r\n"
		return []byte(content), nil
	}
	values := append([]string{launch.executable.Path}, arguments...)
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	content := "#!/bin/sh\n" +
		"export TUTTI_CONNECTOR_CONNECTION_ID=" + shellQuote(route.connectionID) + "\n" +
		"export TUTTI_CONNECTOR_KEY=" + shellQuote(route.connectorKey) + "\n" +
		"export TUTTI_CONNECTOR_LANGUAGE=" + shellQuote(launch.language) + "\n" +
		"export TUTTI_CONNECTOR_STATE_DIR=" + shellQuote(launch.stateDir) + "\n" +
		"export HOME=" + shellQuote(route.userHome) + "\n" +
		"export USERPROFILE=" + shellQuote(route.userHome) + "\n" +
		"cd " + shellQuote(launch.cwd) + " || exit 1\n" +
		"exec " + strings.Join(quoted, " ") + " \"$@\"\n"
	return []byte(content), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

var _ connectorruntime.ManagedRoute = (*connectorRoute)(nil)
