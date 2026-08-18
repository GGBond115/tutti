package daemon

import (
	"context"
	"sync"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
)

type catalogCommandFacet struct{ host *Host }

type commandGate struct {
	mu        sync.Mutex
	accepting bool
	active    int
	drained   chan struct{}
}

func newCommandGate() *commandGate {
	drained := make(chan struct{})
	close(drained)
	return &commandGate{drained: drained}
}

func (gate *commandGate) open() {
	gate.mu.Lock()
	gate.accepting = true
	gate.mu.Unlock()
}

func (gate *commandGate) begin() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if !gate.accepting {
		return false
	}
	if gate.active == 0 {
		gate.drained = make(chan struct{})
	}
	gate.active++
	return true
}

func (gate *commandGate) end() {
	gate.mu.Lock()
	gate.active--
	if gate.active == 0 {
		close(gate.drained)
	}
	gate.mu.Unlock()
}

func (gate *commandGate) close() <-chan struct{} {
	gate.mu.Lock()
	gate.accepting = false
	drained := gate.drained
	gate.mu.Unlock()
	return drained
}

func (host *Host) beginCommand() error {
	if host == nil {
		return errHostNotRunning
	}
	if !host.commandAdmission.begin() {
		return errHostNotRunning
	}
	host.lifecycleMu.Lock()
	if host.lifecycleState != LifecycleStateRunning {
		host.lifecycleMu.Unlock()
		host.commandAdmission.end()
		return errHostNotRunning
	}
	host.lifecycleMu.Unlock()
	return nil
}

func (host *Host) endCommand() { host.commandAdmission.end() }

func commandAdmissionRejected(err error) contracts.CommandResult {
	return contracts.CommandResult{Outcome: contracts.CommandRejected, Failure: &contracts.CommandFailure{
		Code: contracts.ErrorCodeUnavailable, Retryable: false, Message: err.Error(),
	}}
}

func (facet catalogCommandFacet) RefreshCatalog(ctx context.Context, mutation contracts.Mutation) contracts.CommandResult {
	if err := facet.host.beginCommand(); err != nil {
		return commandAdmissionRejected(err)
	}
	defer facet.host.endCommand()
	commandCtx, cancel := facet.host.commandContext(ctx)
	defer cancel()
	return facet.host.catalogCommands.RefreshCatalog(commandCtx, mutation)
}

type installationCommandFacet struct{ host *Host }

func (facet installationCommandFacet) Install(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	if err := facet.host.beginCommand(); err != nil {
		return commandAdmissionRejected(err)
	}
	defer facet.host.endCommand()
	commandCtx, cancel := facet.host.commandContext(ctx)
	defer cancel()
	return facet.host.installationCommands.Install(commandCtx, mutation)
}

func (facet installationCommandFacet) Uninstall(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	if err := facet.host.beginCommand(); err != nil {
		return commandAdmissionRejected(err)
	}
	defer facet.host.endCommand()
	commandCtx, cancel := facet.host.commandContext(ctx)
	defer cancel()
	return facet.host.installationCommands.Uninstall(commandCtx, mutation)
}

type authorizationCommandFacet struct{ host *Host }

type runtimeCommandFacet struct{ host *Host }

func (facet runtimeCommandFacet) RestartRuntime(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	if err := facet.host.beginCommand(); err != nil {
		return commandAdmissionRejected(err)
	}
	defer facet.host.endCommand()
	commandCtx, cancel := facet.host.commandContext(ctx)
	defer cancel()
	return facet.host.runtimeCommands.RestartRuntime(commandCtx, mutation)
}

func (facet authorizationCommandFacet) BeginAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	secret []byte,
) contracts.AuthorizationCommandResult {
	if err := facet.host.beginCommand(); err != nil {
		clear(secret)
		return contracts.AuthorizationCommandResult{CommandResult: commandAdmissionRejected(err)}
	}
	defer facet.host.endCommand()
	commandCtx, cancel := facet.host.commandContext(ctx)
	defer cancel()
	return facet.host.authorizationCommands.BeginAuthorization(commandCtx, mutation, secret)
}

func (facet authorizationCommandFacet) CancelAuthorization(
	ctx context.Context,
	command contracts.CancelAuthorizationCommand,
) contracts.CommandResult {
	if err := facet.host.beginCommand(); err != nil {
		return commandAdmissionRejected(err)
	}
	defer facet.host.endCommand()
	commandCtx, cancel := facet.host.commandContext(ctx)
	defer cancel()
	return facet.host.authorizationCommands.CancelAuthorization(commandCtx, command)
}

func (facet authorizationCommandFacet) DisconnectAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
) contracts.CommandResult {
	if err := facet.host.beginCommand(); err != nil {
		return commandAdmissionRejected(err)
	}
	defer facet.host.endCommand()
	commandCtx, cancel := facet.host.commandContext(ctx)
	defer cancel()
	return facet.host.authorizationCommands.DisconnectAuthorization(commandCtx, mutation)
}

var (
	_ application.CatalogCommands       = catalogCommandFacet{}
	_ application.InstallationCommands  = installationCommandFacet{}
	_ application.RuntimeCommands       = runtimeCommandFacet{}
	_ application.AuthorizationCommands = authorizationCommandFacet{}
)
