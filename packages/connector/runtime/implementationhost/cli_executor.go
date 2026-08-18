package implementationhost

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
	connectorruntime "github.com/tutti-os/tutti/packages/connector/runtime"
	connectorprocess "github.com/tutti-os/tutti/packages/connector/runtime/process"
)

var cliContractHashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

const (
	maxCLIExecutionArguments = 256
	maxCLIExecutionArgBytes  = 256 << 10
)

var (
	ErrCLIExecutionInvalid          = errors.New("connector CLI execution request is invalid")
	ErrCLIExecutionUnavailable      = errors.New("connector CLI execution is unavailable")
	ErrCLIExecutionIdentityMismatch = errors.New("connector CLI execution identity does not match the current route")
)

// CLIExecutionRequest binds one raw CLI invocation to the exact immutable
// route selected and authorized by a product-owned Connector broker.
type CLIExecutionRequest struct {
	ConnectionID     string
	ConnectorKey     string
	ConnectorVersion string
	ReleaseDigest    string
	Generation       contracts.HostGeneration
	CLIContractHash  string
	Arguments        []string
}

// StartCLI launches one current managed CLI route. It never accepts an
// executable, working directory, environment, or state path from the caller.
func (host *Host) StartCLI(ctx context.Context, request CLIExecutionRequest) (connectorprocess.Connection, error) {
	if err := validateCLIExecutionRequest(ctx, request); err != nil {
		return nil, err
	}
	if host == nil || host.routes == nil || host.processes == nil {
		return nil, ErrCLIExecutionUnavailable
	}
	route, _ := host.routes.Route(connectorRouteKey(request.ConnectionID, request.ConnectorKey)).(*connectorRoute)
	if route == nil || !host.routeCurrent(route) || route.cliLaunch == nil || route.cliContractHash == "" {
		return nil, ErrCLIExecutionUnavailable
	}
	if route.connectorVersion != request.ConnectorVersion || route.releaseDigest != request.ReleaseDigest ||
		route.generation != request.Generation || route.cliContractHash != request.CLIContractHash {
		return nil, ErrCLIExecutionIdentityMismatch
	}
	launch := route.cliLaunch
	arguments := append(append([]string(nil), launch.arguments...), request.Arguments...)
	spec := connectorruntime.ConnectorProcessSpec(route.connectionID, route.connectorKey, launch.language,
		launch.executable, launch.cwd, arguments, launch.stateDir, route.userHome, launch.artifactTrees)
	startCtx := ctx
	var executionDeadline time.Time
	var cancel context.CancelFunc
	if launch.timeout > 0 {
		executionDeadline = time.Now().Add(launch.timeout)
		startCtx, cancel = context.WithDeadline(ctx, executionDeadline)
		defer cancel()
	}
	connection, processID, err := host.startProcess(startCtx, route, spec, true)
	if err != nil {
		return nil, errors.Join(ErrCLIExecutionUnavailable, err)
	}
	return wrapCLIExecutionConnection(route, processID, connection, executionDeadline), nil
}

func validateCLIExecutionRequest(ctx context.Context, request CLIExecutionRequest) error {
	if ctx == nil || !hostIdentityPattern.MatchString(request.ConnectionID) || !hostIdentityPattern.MatchString(request.ConnectorKey) ||
		strings.TrimSpace(request.ConnectorVersion) == "" || strings.TrimSpace(request.ReleaseDigest) == "" ||
		request.Generation.BootEpoch == "" || request.Generation.Generation == 0 ||
		!cliContractHashPattern.MatchString(request.CLIContractHash) || len(request.Arguments) > maxCLIExecutionArguments {
		return ErrCLIExecutionInvalid
	}
	totalBytes := 0
	for _, argument := range request.Arguments {
		totalBytes += len(argument)
		if strings.ContainsRune(argument, '\x00') || totalBytes > maxCLIExecutionArgBytes {
			return ErrCLIExecutionInvalid
		}
	}
	return nil
}

type cliExecutionConnection struct {
	route      *connectorRoute
	processID  uint64
	connection connectorprocess.Connection
	closeOnce  sync.Once
	closeErr   error
	execution  context.Context
	cancel     context.CancelFunc
}

func (connection *cliExecutionConnection) Send(payload []byte) error {
	return connection.connection.Send(payload)
}
func (connection *cliExecutionConnection) Recv() (connectorprocess.Frame, error) {
	return connection.connection.Recv()
}
func (connection *cliExecutionConnection) Close() error {
	connection.closeOnce.Do(func() {
		if connection.cancel != nil {
			connection.cancel()
		}
		connection.closeErr = connection.route.releaseProcess(connection.processID, connection.connection)
	})
	return connection.closeErr
}

type contextualCLIExecutionConnection struct {
	*cliExecutionConnection
	contextual connectorprocess.ContextConnection
}

func (connection *contextualCLIExecutionConnection) RecvContext(ctx context.Context) (connectorprocess.Frame, error) {
	merged, cancel := mergeCLIExecutionContext(ctx, connection.execution)
	defer cancel()
	frame, err := connection.contextual.RecvContext(merged)
	if connection.execution != nil && errors.Is(connection.execution.Err(), context.DeadlineExceeded) {
		return connectorprocess.Frame{}, context.DeadlineExceeded
	}
	return frame, err
}

type gracefulCLIExecutionConnection struct {
	*cliExecutionConnection
	graceful connectorprocess.GracefulConnection
}

func (connection *gracefulCLIExecutionConnection) CloseInput() error {
	return connection.graceful.CloseInput()
}
func (connection *gracefulCLIExecutionConnection) Terminate() error {
	return connection.graceful.Terminate()
}
func (connection *gracefulCLIExecutionConnection) Kill() error { return connection.graceful.Kill() }

type contextualGracefulCLIExecutionConnection struct {
	*gracefulCLIExecutionConnection
	contextual connectorprocess.ContextConnection
}

func (connection *contextualGracefulCLIExecutionConnection) RecvContext(ctx context.Context) (connectorprocess.Frame, error) {
	merged, cancel := mergeCLIExecutionContext(ctx, connection.execution)
	defer cancel()
	frame, err := connection.contextual.RecvContext(merged)
	if connection.execution != nil && errors.Is(connection.execution.Err(), context.DeadlineExceeded) {
		return connectorprocess.Frame{}, context.DeadlineExceeded
	}
	return frame, err
}

func wrapCLIExecutionConnection(route *connectorRoute, processID uint64,
	connection connectorprocess.Connection, executionDeadline time.Time) connectorprocess.Connection {
	base := &cliExecutionConnection{route: route, processID: processID, connection: connection}
	if !executionDeadline.IsZero() {
		base.execution, base.cancel = context.WithDeadline(context.Background(), executionDeadline)
	}
	contextual, hasContext := connection.(connectorprocess.ContextConnection)
	graceful, hasGraceful := connection.(connectorprocess.GracefulConnection)
	switch {
	case hasContext && hasGraceful:
		return &contextualGracefulCLIExecutionConnection{
			gracefulCLIExecutionConnection: &gracefulCLIExecutionConnection{cliExecutionConnection: base, graceful: graceful},
			contextual:                     contextual,
		}
	case hasContext:
		return &contextualCLIExecutionConnection{cliExecutionConnection: base, contextual: contextual}
	case hasGraceful:
		return &gracefulCLIExecutionConnection{cliExecutionConnection: base, graceful: graceful}
	default:
		return base
	}
}

func mergeCLIExecutionContext(caller, execution context.Context) (context.Context, context.CancelFunc) {
	if execution == nil {
		return context.WithCancel(caller)
	}
	merged, cancel := context.WithCancel(caller)
	stop := context.AfterFunc(execution, cancel)
	return merged, func() {
		stop()
		cancel()
	}
}
