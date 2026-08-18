package daemon

import (
	"context"
	"errors"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"sync"
	"time"
)

// activationGateHost is the global runtime publication boundary. It rejects
// operations issued for an inactive account and serializes every delegate
// mutation with FailClosed so no route can commit after an account fence.
type activationGateHost struct {
	delegate   application.ImplementationCommands
	mu         sync.Mutex
	admission  sync.RWMutex
	open       bool
	failClosed bool
	scope      contracts.OperationScope
	staged     map[string]contracts.RuntimeReconcileRequest
}

func newActivationGateHost(delegate application.ImplementationCommands) *activationGateHost {
	return &activationGateHost{delegate: delegate, staged: make(map[string]contracts.RuntimeReconcileRequest)}
}

func (gate *activationGateHost) Reconcile(ctx context.Context, request contracts.RuntimeReconcileRequest) (contracts.RuntimeReceipt, error) {
	key := request.ConnectionID + "\x00" + request.Connector.Key
	gate.admission.RLock()
	defer gate.admission.RUnlock()
	gate.mu.Lock()
	if request.Scope != gate.scope {
		gate.mu.Unlock()
		return contracts.RuntimeReceipt{}, errors.New("connector runtime request belongs to an inactive account scope")
	}
	if !request.Enabled {
		delete(gate.staged, key)
		gate.mu.Unlock()
		return gate.delegate.Reconcile(ctx, request)
	}
	if gate.open {
		gate.mu.Unlock()
		return gate.delegate.Reconcile(ctx, request)
	}
	current, exists := gate.staged[key]
	if !exists || current.Generation.BootEpoch != request.Generation.BootEpoch || request.Generation.Generation >= current.Generation.Generation {
		gate.staged[key] = request
	}
	gate.mu.Unlock()
	return contracts.RuntimeReceipt{OperationID: request.OperationID, ConnectionID: request.ConnectionID,
		ConnectorKey: request.Connector.Key, ReleaseDigest: request.Connector.Release.ReleaseDigest, Generation: request.Generation,
		Readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessBlocked, ReasonCode: "publication_gate_closed"}}, nil
}

func (gate *activationGateHost) DeactivateRuntime(ctx context.Context, request contracts.RuntimeDeactivationRequest) error {
	gate.admission.RLock()
	defer gate.admission.RUnlock()
	gate.mu.Lock()
	if request.Scope != gate.scope {
		gate.mu.Unlock()
		return errors.New("connector runtime deactivation belongs to an inactive account scope")
	}
	delete(gate.staged, request.ConnectionID+"\x00"+request.ConnectorKey)
	gate.mu.Unlock()
	return gate.delegate.DeactivateRuntime(ctx, request)
}

func (gate *activationGateHost) FailClosed(ctx context.Context, deadline time.Time) error {
	gate.admission.Lock()
	defer gate.admission.Unlock()
	gate.mu.Lock()
	gate.failClosed = true
	gate.open = false
	gate.staged = make(map[string]contracts.RuntimeReconcileRequest)
	gate.mu.Unlock()
	return gate.delegate.FailClosed(ctx, deadline)
}

func (gate *activationGateHost) Close(ctx context.Context) error {
	return gate.delegate.Close(ctx)
}

func (gate *activationGateHost) requiresRecovery() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.failClosed
}

func (gate *activationGateHost) markRecovered() {
	gate.mu.Lock()
	gate.failClosed = false
	gate.mu.Unlock()
}

func (gate *activationGateHost) setOpen(scope contracts.OperationScope, open bool) {
	gate.mu.Lock()
	if gate.scope != scope {
		gate.staged = make(map[string]contracts.RuntimeReconcileRequest)
	}
	gate.scope = scope
	gate.open = open
	if !open {
		gate.staged = make(map[string]contracts.RuntimeReconcileRequest)
	}
	gate.mu.Unlock()
}
