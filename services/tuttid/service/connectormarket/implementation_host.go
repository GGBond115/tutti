package connectormarket

import (
	"context"
	"errors"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	connectorruntime "github.com/tutti-os/tutti/packages/connector/runtime"
	implementationhost "github.com/tutti-os/tutti/packages/connector/runtime/implementationhost"
	connectorprocess "github.com/tutti-os/tutti/packages/connector/runtime/process"
	"os"
	"runtime"
	"time"
)

type PreparedArtifactResolver = implementationhost.PreparedArtifactResolver
type ConnectorRuntimeResolver = connectorruntime.ConnectorRuntimeResolver

type ConnectorRuntimeRegistry struct {
	runtime *implementationhost.RouteRegistry
	mcp     *implementationhost.MCPRegistry
}

type ImplementationHostConfig struct {
	Artifacts              PreparedArtifactResolver
	CLIInstallations       application.CLIInstallationManager
	Runtimes               ConnectorRuntimeResolver
	Processes              connectorprocess.Transport
	Registry               *ConnectorRuntimeRegistry
	StateRoot              string
	BinDir                 string
	UserHome               string
	MCPStartupTimeout      time.Duration
	RemoteMCPClientFactory implementationhost.RemoteMCPClientFactory
}

// ImplementationHost adapts the host-neutral Connector runtime to tuttId.
type ImplementationHost struct {
	runtime   *implementationhost.Host
	artifacts PreparedArtifactResolver
}

var _ application.ImplementationCommands = (*ImplementationHost)(nil)
var _ application.RouteObservation = (*ImplementationHost)(nil)

func NewConnectorRuntimeRegistry() *ConnectorRuntimeRegistry {
	return &ConnectorRuntimeRegistry{runtime: implementationhost.NewRouteRegistry(), mcp: implementationhost.NewMCPRegistry()}
}

func (registry *ConnectorRuntimeRegistry) MCPRegistry() *implementationhost.MCPRegistry {
	if registry == nil {
		return nil
	}
	return registry.mcp
}

func (registry *ConnectorRuntimeRegistry) RouteRegistry() *implementationhost.RouteRegistry {
	if registry == nil {
		return nil
	}
	return registry.runtime
}

func NewImplementationHost(config ImplementationHostConfig) (*ImplementationHost, error) {
	if config.Registry == nil {
		return nil, errors.New("connector runtime registry is required")
	}
	if config.UserHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.New("connector implementation user home is unavailable")
		}
		config.UserHome = userHome
	}
	host, err := implementationhost.New(implementationhost.Config{
		Artifacts: config.Artifacts, CLIInstallations: config.CLIInstallations, Runtimes: config.Runtimes,
		Processes: config.Processes, Registry: config.Registry.runtime, MCP: config.Registry.mcp, StateRoot: config.StateRoot, BinDir: config.BinDir,
		UserHome: config.UserHome, MCPStartupTimeout: config.MCPStartupTimeout,
		RemoteMCPClientFactory: config.RemoteMCPClientFactory,
	})
	if err != nil {
		return nil, err
	}
	return &ImplementationHost{runtime: host, artifacts: config.Artifacts}, nil
}

func (host *ImplementationHost) Reconcile(ctx context.Context, request contracts.RuntimeReconcileRequest) (contracts.RuntimeReceipt, error) {
	if host == nil || host.runtime == nil {
		return contracts.RuntimeReceipt{}, errors.New("connector implementation host is unavailable")
	}
	return host.runtime.Reconcile(ctx, implementationhost.ReconcileRequest{Runtime: request})
}

func (host *ImplementationHost) Snapshot(ctx context.Context) (contracts.PhysicalRouteSnapshot, error) {
	if host == nil || host.runtime == nil {
		return contracts.PhysicalRouteSnapshot{}, errors.New("connector physical route observation is unavailable")
	}
	return host.runtime.Snapshot(ctx)
}

func (host *ImplementationHost) Watch(ctx context.Context) (contracts.PhysicalRouteWatch, error) {
	if host == nil || host.runtime == nil {
		return contracts.PhysicalRouteWatch{}, errors.New("connector physical route observation is unavailable")
	}
	return host.runtime.Watch(ctx)
}

func (host *ImplementationHost) Begin(ctx context.Context, request contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	if host == nil || host.runtime == nil {
		return contracts.AuthorizationSession{}, errors.New("connector authorization provider is unavailable")
	}
	return host.runtime.BeginAuthorization(ctx, request)
}

func (host *ImplementationHost) Disconnect(ctx context.Context, request contracts.AuthorizationDisconnectRequest) error {
	if host == nil || host.runtime == nil {
		return errors.New("connector authorization provider is unavailable")
	}
	return host.runtime.DisconnectAuthorization(ctx, request)
}

func (host *ImplementationHost) Cancel(ctx context.Context, request contracts.AuthorizationCancelRequest) error {
	if host == nil || host.runtime == nil {
		return errors.New("connector authorization provider is unavailable")
	}
	return host.runtime.CancelAuthorization(ctx, request)
}

func (host *ImplementationHost) InspectAuthorization(ctx context.Context, request contracts.AuthorizationInspectRequest) (contracts.AuthorizationObservation, error) {
	if host == nil || host.runtime == nil {
		return contracts.AuthorizationObservation{}, errors.New("connector authorization inspector is unavailable")
	}
	return host.runtime.InspectAuthorization(ctx, request)
}

func (host *ImplementationHost) DeactivateRuntime(ctx context.Context, request contracts.RuntimeDeactivationRequest) error {
	if host == nil || host.runtime == nil {
		return errors.New("connector implementation host is unavailable")
	}
	return host.runtime.DeactivateRuntime(ctx, request)
}

func (host *ImplementationHost) FailClosed(ctx context.Context, deadline time.Time) error {
	if host == nil || host.runtime == nil {
		return nil
	}
	return host.runtime.FailClosed(ctx, deadline)
}

func (host *ImplementationHost) FenceAll(ctx context.Context, deadline time.Time) error {
	if host == nil || host.runtime == nil {
		return nil
	}
	return host.runtime.FenceAll(ctx, deadline)
}

func (host *ImplementationHost) SetCapabilityPublication(enabled bool) {
	if host != nil && host.runtime != nil {
		host.runtime.SetCapabilityPublication(enabled)
	}
}

func (host *ImplementationHost) Close(ctx context.Context) error {
	if host == nil || host.runtime == nil {
		return nil
	}
	return host.runtime.Close(ctx)
}

func ProductionPorts(host *ImplementationHost, external application.AuthorizationProvider) (application.ImplementationCommands, application.AuthorizationProvider, application.CompatibilityEvaluator, application.ImplementationRegistry) {
	return host, application.NewImplementationAuthorizationRouter(host, external), productionCompatibility{}, application.NewImplementationRegistry(map[string]application.ImplementationValidator{
		contracts.ImplementationKindManagedStdio:         nil,
		contracts.ImplementationKindRemoteStreamableHTTP: nil,
	})
}

type productionCompatibility struct{}

func (productionCompatibility) Evaluate(manifest contracts.Manifest) contracts.Compatibility {
	switch manifest.Implementation.Kind {
	case contracts.ImplementationKindRemoteStreamableHTTP:
		return contracts.Compatibility{State: contracts.CompatibilityStateSupported}
	case contracts.ImplementationKindManagedStdio:
	default:
		return contracts.Compatibility{State: contracts.CompatibilityStateUnsupportedImplementation, Reason: "implementation is unavailable"}
	}
	if manifest.AuthorizationKind != "none" && (manifest.Implementation.ManagedStdio == nil || manifest.Implementation.ManagedStdio.CredentialBroker == nil) {
		return contracts.Compatibility{State: contracts.CompatibilityStateUnsupportedImplementation, Reason: "authorization broker is unavailable"}
	}
	for _, platform := range manifest.Compatibility.Platforms {
		if platform == runtime.GOOS+"-"+runtime.GOARCH {
			return contracts.Compatibility{State: contracts.CompatibilityStateSupported}
		}
	}
	if len(manifest.Compatibility.Platforms) != 0 {
		return contracts.Compatibility{State: contracts.CompatibilityStateUnsupportedPlatform, Reason: "platform is not supported"}
	}
	return contracts.Compatibility{State: contracts.CompatibilityStateSupported}
}
