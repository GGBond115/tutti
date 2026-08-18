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

func (facet catalogCommandFacet) RefreshCatalog(ctx context.Context, mutation contracts.Mutation) (contracts.MutationResult, error) {
	if err := facet.host.beginCommand(); err != nil {
		return contracts.MutationResult{}, err
	}
	defer facet.host.endCommand()
	return facet.host.catalogCommands.RefreshCatalog(ctx, mutation)
}

type installationCommandFacet struct{ host *Host }

func (facet installationCommandFacet) Install(ctx context.Context, mutation contracts.ConnectorMutation) (contracts.MutationResult, error) {
	if err := facet.host.beginCommand(); err != nil {
		return contracts.MutationResult{}, err
	}
	defer facet.host.endCommand()
	return facet.host.installationCommands.Install(ctx, mutation)
}

func (facet installationCommandFacet) Uninstall(ctx context.Context, mutation contracts.ConnectorMutation) (contracts.MutationResult, error) {
	if err := facet.host.beginCommand(); err != nil {
		return contracts.MutationResult{}, err
	}
	defer facet.host.endCommand()
	return facet.host.installationCommands.Uninstall(ctx, mutation)
}

type authorizationCommandFacet struct{ host *Host }

func (facet authorizationCommandFacet) BeginAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	secret []byte,
) (contracts.AuthorizationResult, error) {
	if err := facet.host.beginCommand(); err != nil {
		clear(secret)
		return contracts.AuthorizationResult{}, err
	}
	defer facet.host.endCommand()
	return facet.host.authorizationCommands.BeginAuthorization(ctx, mutation, secret)
}

func (facet authorizationCommandFacet) CancelAuthorization(
	ctx context.Context,
	scope contracts.OperationScope,
	operationID string,
) error {
	if err := facet.host.beginCommand(); err != nil {
		return err
	}
	defer facet.host.endCommand()
	return facet.host.authorizationCommands.CancelAuthorization(ctx, scope, operationID)
}

func (facet authorizationCommandFacet) DisconnectAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
) (contracts.MutationResult, error) {
	if err := facet.host.beginCommand(); err != nil {
		return contracts.MutationResult{}, err
	}
	defer facet.host.endCommand()
	return facet.host.authorizationCommands.DisconnectAuthorization(ctx, mutation)
}

var (
	_ application.CatalogCommands       = catalogCommandFacet{}
	_ application.InstallationCommands  = installationCommandFacet{}
	_ application.AuthorizationCommands = authorizationCommandFacet{}
)
