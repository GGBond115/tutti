package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
)

type HostConfig struct {
	Repository                  application.Repository
	CatalogSource               application.CatalogSource
	ReleaseInstallations        application.ReleaseInstallationManager
	ImplementationCommands      application.ImplementationCommands
	PhysicalRoutes              application.RouteObservation
	PhysicalAntiEntropyInterval time.Duration
	Authorization               application.AuthorizationProvider
	AuthorizationProjections    application.AuthorizationProjectionStore
	AuthorizationSnapshots      application.AuthorizationSnapshotSource
	AuthorizationEvents         application.AuthorizationEventSource
	AuthorizationReadiness      *application.AuthorizationReadinessGate
	SharedAgentSupport          application.SharedAgentSupportSource
	AgentConnectorGrants        application.AgentConnectorGrantSource
	RuntimeBindings             application.RuntimeBindingResolver
	RuntimeIntents              application.RuntimeIntentResolver
	Compatibility               application.CompatibilityEvaluator
	ImplementationRegistry      application.ImplementationRegistry
	Outbox                      application.ChangedEventOutbox
	Lifecycle                   application.LifecycleCleanupStore
	LifecyclePolicy             LifecycleCleanupPolicy
	Publisher                   ChangedEventPublisher
	Publication                 CapabilityPublicationController
}

// CapabilityPublicationController is the daemon-level publication boundary
// for runtimes owned by another process or machine.
type CapabilityPublicationController interface {
	ApplyCapabilityPublication(context.Context, contracts.OperationScope, bool) error
}

type Host struct {
	state                    application.StateQueries
	catalogQueries           application.CatalogQueries
	catalogCommands          application.CatalogCommands
	installationCommands     application.InstallationCommands
	runtimeCommands          application.RuntimeCommands
	authorizationCommands    application.AuthorizationCommands
	operationQueries         application.OperationQueries
	agentPolicy              application.AgentConnectorPolicyQueries
	recoveryControl          application.RecoveryControl
	operationRecovery        application.OperationRecoveryControl
	catalogMaintenance       application.CatalogMaintenance
	installationMaintenance  application.InstallationMaintenance
	authorizationMaintenance application.AuthorizationMaintenance
	runtimeMaintenance       application.RuntimeMaintenance

	scheduler                   *OperationScheduler
	authorizationSyncWake       chan struct{}
	authorizationScopeWake      chan struct{}
	runtimeRecoveryWake         chan struct{}
	runtimePhysicalWake         chan struct{}
	lifecycleMu                 sync.Mutex
	lifecycleCtx                context.Context
	lifecycleCancel             context.CancelFunc
	lifecycleEpoch              uint64
	transitionEpoch             uint64
	commandAdmission            *commandGate
	lifecycleState              LifecycleState
	workers                     *workerGroup
	closeDone                   chan struct{}
	closeResult                 error
	closeOnce                   sync.Once
	bootstrapMu                 sync.Mutex
	scopeTransition             chan struct{}
	bootstrapped                bool
	bootstrapReady              chan struct{}
	bootstrapReadyOnce          sync.Once
	bootstrapScope              contracts.OperationScope
	publicationScopeMu          sync.Mutex
	publicationScope            contracts.OperationScope
	publicationMu               sync.Mutex
	shutdownTimeout             time.Duration
	catalogRefreshInitialDelay  time.Duration
	catalogRetryJitter          func(time.Duration) time.Duration
	catalogRetryWait            func(context.Context, time.Duration) bool
	implementationCommands      application.ImplementationCommands
	physicalRoutes              application.RouteObservation
	physicalAntiEntropyInterval time.Duration
	physicalAntiEntropyJitter   func(time.Duration) time.Duration
	activationGate              *activationGateHost
	publicationGate             capabilityPublicationGate
	publication                 CapabilityPublicationController
	authorizationSnapshots      application.AuthorizationSnapshotSource
	authorizationSnapshotStore  application.AuthorizationSnapshotStore
	authorizationEvents         application.AuthorizationEventSource
	authorizationReadiness      *application.AuthorizationReadinessGate
	authorizationDirty          map[string]map[string]struct{}
	runtimeRecoveryPending      map[string]struct{}
	outbox                      application.ChangedEventOutbox
	lifecycle                   application.LifecycleCleanupStore
	lifecyclePolicy             LifecycleCleanupPolicy
	publisher                   ChangedEventPublisher
	hasAuthorizationObserver    bool
}

type capabilityPublicationGate interface {
	SetCapabilityPublication(bool)
}

func NewHost(config HostConfig) (*Host, error) {
	if config.Outbox == nil || config.Lifecycle == nil || config.Publisher == nil {
		return nil, errors.New("connector market outbox, lifecycle cleanup, and publisher are required")
	}
	physicalRoutes := config.PhysicalRoutes
	physicalAntiEntropyInterval := config.PhysicalAntiEntropyInterval
	if physicalAntiEntropyInterval <= 0 {
		physicalAntiEntropyInterval = defaultPhysicalAntiEntropyInterval
	}
	if physicalRoutes == nil {
		return nil, errors.New("connector physical route observation is required")
	}
	scheduler := newOperationScheduler()
	activationGate := newActivationGateHost(config.ImplementationCommands)
	composition, err := application.New(application.Config{
		Repository:               config.Repository,
		CatalogSource:            config.CatalogSource,
		ReleaseInstallations:     config.ReleaseInstallations,
		ImplementationCommands:   activationGate,
		Authorization:            config.Authorization,
		AuthorizationProjections: config.AuthorizationProjections,
		AuthorizationSnapshots:   config.AuthorizationSnapshots,
		AuthorizationReadiness:   config.AuthorizationReadiness,
		SharedAgentSupport:       config.SharedAgentSupport,
		AgentConnectorGrants:     config.AgentConnectorGrants,
		RuntimeBindings:          config.RuntimeBindings,
		RuntimeIntents:           config.RuntimeIntents,
		Compatibility:            config.Compatibility,
		Scheduler:                scheduler,
		ImplementationRegistry:   config.ImplementationRegistry,
	})
	if err != nil {
		return nil, err
	}
	if err := scheduler.Bind(composition.Daemon.Recovery); err != nil {
		return nil, err
	}
	host := &Host{
		commandAdmission:            newCommandGate(),
		state:                       composition.Root.State(),
		catalogQueries:              composition.Root.Catalog(),
		catalogCommands:             composition.Root.CatalogCommands(),
		installationCommands:        composition.Root.Installations(),
		runtimeCommands:             composition.Root.RuntimeCommands(),
		authorizationCommands:       composition.Root.Authorizations(),
		operationQueries:            composition.Root.Operations(),
		agentPolicy:                 composition.Root.AgentPolicy(),
		recoveryControl:             composition.Daemon.Recovery,
		operationRecovery:           composition.Daemon.Operations,
		catalogMaintenance:          composition.Daemon.Catalog,
		installationMaintenance:     composition.Daemon.Installation,
		authorizationMaintenance:    composition.Daemon.Authorization,
		runtimeMaintenance:          composition.Daemon.Runtime,
		scheduler:                   scheduler,
		authorizationSyncWake:       make(chan struct{}, 1),
		authorizationScopeWake:      make(chan struct{}, 1),
		runtimeRecoveryWake:         make(chan struct{}, 1),
		runtimePhysicalWake:         make(chan struct{}, 1),
		lifecycleState:              LifecycleStateCreated,
		closeDone:                   make(chan struct{}),
		bootstrapReady:              make(chan struct{}),
		scopeTransition:             make(chan struct{}, 1),
		implementationCommands:      config.ImplementationCommands,
		physicalRoutes:              physicalRoutes,
		physicalAntiEntropyInterval: physicalAntiEntropyInterval,
		physicalAntiEntropyJitter:   fullJitterDuration,
		catalogRetryJitter:          fullJitterDuration,
		activationGate:              activationGate,
		publication:                 config.Publication,
		authorizationSnapshots:      config.AuthorizationSnapshots,
		authorizationEvents:         config.AuthorizationEvents,
		authorizationReadiness:      config.AuthorizationReadiness,
		authorizationDirty:          make(map[string]map[string]struct{}),
		runtimeRecoveryPending:      make(map[string]struct{}),
		outbox:                      config.Outbox,
		lifecycle:                   config.Lifecycle,
		lifecyclePolicy:             config.LifecyclePolicy,
		publisher:                   config.Publisher,
	}
	if snapshotStore, ok := config.AuthorizationProjections.(application.AuthorizationSnapshotStore); ok {
		host.authorizationSnapshotStore = snapshotStore
	}
	if publicationGate, ok := config.ImplementationCommands.(capabilityPublicationGate); ok {
		host.publicationGate = publicationGate
	}
	if _, ok := config.Authorization.(application.AuthorizationObserver); ok {
		host.hasAuthorizationObserver = true
	}
	host.scopeTransition <- struct{}{}
	return host, nil
}

func (host *Host) acquireScopeTransition(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-host.scopeTransition:
		host.lifecycleMu.Lock()
		host.transitionEpoch++
		host.lifecycleMu.Unlock()
		return nil
	}
}

func (host *Host) releaseScopeTransition() { host.scopeTransition <- struct{}{} }

// StateQueries exposes only the account-aware read facet.
func (host *Host) StateQueries() application.StateQueries {
	if host == nil {
		return nil
	}
	return host.state
}

func (host *Host) CatalogQueries() application.CatalogQueries {
	if host == nil {
		return nil
	}
	return host.catalogQueries
}

func (host *Host) CatalogCommands() application.CatalogCommands {
	if host == nil {
		return nil
	}
	return catalogCommandFacet{host: host}
}

func (host *Host) InstallationCommands() application.InstallationCommands {
	if host == nil {
		return nil
	}
	return installationCommandFacet{host: host}
}

func (host *Host) RuntimeCommands() application.RuntimeCommands {
	if host == nil {
		return nil
	}
	return runtimeCommandFacet{host: host}
}

func (host *Host) AuthorizationCommands() application.AuthorizationCommands {
	if host == nil {
		return nil
	}
	return authorizationCommandFacet{host: host}
}

func (host *Host) OperationQueries() application.OperationQueries {
	if host == nil {
		return nil
	}
	return host.operationQueries
}

func (host *Host) AgentPolicy() application.AgentConnectorPolicyQueries {
	if host == nil {
		return nil
	}
	return host.agentPolicy
}

func (host *Host) State() LifecycleState {
	if host == nil {
		return LifecycleStateStopped
	}
	host.lifecycleMu.Lock()
	defer host.lifecycleMu.Unlock()
	return host.lifecycleState
}

func (host *Host) Health() WorkerHealthSnapshot {
	if host == nil {
		return WorkerHealthSnapshot{Lifecycle: LifecycleStateStopped}
	}
	host.lifecycleMu.Lock()
	state := host.lifecycleState
	workers := host.workers
	host.lifecycleMu.Unlock()
	return WorkerHealthSnapshot{
		Lifecycle: state, UnexpectedExits: workers.unexpectedNames(), Workers: workers.healthSnapshot(),
	}
}

// RuntimeRetryHealth returns the application-owned, per-Connector durable
// retry projection for the active scope. It intentionally remains separate
// from WorkerHealth: independent Connector failure budgets must never be
// collapsed into the runtime-convergence scanner's process health.
func (host *Host) RuntimeRetryHealth(ctx context.Context) ([]contracts.RuntimeRetryHealth, error) {
	if host == nil || host.runtimeMaintenance == nil {
		return nil, errHostNotRunning
	}
	host.bootstrapMu.Lock()
	bootstrapped, scope := host.bootstrapped, host.bootstrapScope
	host.bootstrapMu.Unlock()
	if !bootstrapped {
		return nil, errHostNotRunning
	}
	return host.runtimeMaintenance.RuntimeRetryHealth(ctx, scope)
}

func (host *Host) handleUnexpectedWorkerExit(name string) {
	drained := host.commandAdmission.close()
	host.lifecycleMu.Lock()
	if host.lifecycleState == LifecycleStateStopping || host.lifecycleState == LifecycleStateStopped {
		host.lifecycleMu.Unlock()
		return
	}
	host.lifecycleState = LifecycleStateFailed
	host.lifecycleEpoch++
	if host.lifecycleCancel != nil {
		host.lifecycleCancel()
	}
	workers := host.workers
	host.lifecycleMu.Unlock()
	if workers != nil {
		workers.Stop()
	}
	scope := host.currentPublicationScope()
	host.activationGate.setOpen(scope, false)
	publicationResult := host.beginBestEffortPublicationDisable(scope)
	go func() {
		shutdownCtx, cancel := host.newShutdownContext()
		defer cancel()
		var failCloseErr error
		select {
		case <-drained:
		case <-shutdownCtx.Done():
			failCloseErr = shutdownCtx.Err()
		}
		failCloseErr = errors.Join(failCloseErr, <-publicationResult)
		if err := host.acquireScopeTransition(shutdownCtx); err != nil {
			failCloseErr = errors.Join(failCloseErr, err)
		} else {
			failCloseErr = errors.Join(failCloseErr,
				host.runBounded(shutdownCtx, func(callCtx context.Context) error {
					return host.applyCapabilityPublication(callCtx, scope, false)
				}),
				host.runBounded(shutdownCtx, func(callCtx context.Context) error {
					return host.activationGate.FailClosed(callCtx, host.shutdownDeadline(callCtx))
				}),
			)
			host.releaseScopeTransition()
		}
		if failCloseErr != nil {
			slog.Error("connector daemon worker exit fail-close failed", "worker", name, "error", failCloseErr)
		}
	}()
}

// Start owns every Connector daemon worker. NewHost deliberately performs no
// publication, scheduling, cleanup, polling, or goroutine startup.
func (host *Host) Start(ctx context.Context, initialScope contracts.OperationScope) error {
	if host == nil || host.recoveryControl == nil || ctx == nil {
		return errors.New("connector market host start context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	host.lifecycleMu.Lock()
	if host.lifecycleState != LifecycleStateCreated {
		state := host.lifecycleState
		host.lifecycleMu.Unlock()
		return fmt.Errorf("connector market host cannot start from %s", state)
	}
	host.lifecycleState = LifecycleStateStarting
	host.lifecycleEpoch++
	lifecycleCtx, lifecycleCancel := context.WithCancel(ctx)
	host.lifecycleCtx = lifecycleCtx
	host.lifecycleCancel = lifecycleCancel
	workers := newWorkerGroup(lifecycleCtx, host.handleUnexpectedWorkerExit)
	host.workers = workers
	host.lifecycleMu.Unlock()
	rollback := func(cause error) error {
		workers.Stop()
		lifecycleCancel()
		host.lifecycleMu.Lock()
		if host.lifecycleState == LifecycleStateStarting {
			host.lifecycleState = LifecycleStateFailed
			host.lifecycleEpoch++
		}
		host.lifecycleMu.Unlock()
		cleanupErr := host.cleanupFailedStart(workers)
		host.lifecycleMu.Lock()
		if host.workers == workers {
			host.workers = nil
		}
		host.lifecycleMu.Unlock()
		return errors.Join(cause, cleanupErr)
	}

	host.activationGate.setOpen(contracts.OperationScope{}, false)
	if err := host.applyCapabilityPublication(ctx, contracts.OperationScope{}, false); err != nil {
		return rollback(fmt.Errorf("fail-close connector capability publication: %w", err))
	}
	if err := host.requireBootstrapLifecycle(); err != nil {
		return rollback(err)
	}
	if err := host.scheduler.Start(workers.ctx); err != nil {
		return rollback(err)
	}
	if err := host.requireBootstrapLifecycle(); err != nil {
		return rollback(err)
	}
	dispatcher := OutboxDispatcher{Outbox: host.outbox, Publisher: host.publisher}
	cleanupWorker := LifecycleCleanupWorker{Store: host.lifecycle, Policy: host.lifecyclePolicy}
	registrations := []struct {
		name string
		run  func(context.Context)
	}{
		{name: "outbox", run: dispatcher.Run},
		{name: "lifecycle-cleanup", run: cleanupWorker.Run},
		{name: "runtime-recovery", run: host.runRuntimeRecoveryWorker},
		{name: "runtime-convergence", run: host.runRuntimeConvergenceWorker},
		{name: "runtime-route-watch", run: host.runPhysicalRouteWatchWorker},
		{name: "operation-recovery", run: host.runOperationRecoveryWorker},
		{name: "catalog-refresh", run: host.runCatalogRefreshWorker},
	}
	if host.authorizationSnapshots != nil && host.authorizationSnapshotStore != nil {
		registrations = append(registrations, struct {
			name string
			run  func(context.Context)
		}{name: "authorization-snapshot", run: host.runAuthorizationSnapshotWorker})
	}
	if host.authorizationEvents != nil {
		registrations = append(registrations, struct {
			name string
			run  func(context.Context)
		}{name: "authorization-events", run: host.runAuthorizationEventWorker})
	}
	if host.hasAuthorizationObserver {
		registrations = append(registrations, struct {
			name string
			run  func(context.Context)
		}{name: "authorization-reconcile", run: host.runAuthorizationReconcileWorker})
	}
	for _, registration := range registrations {
		run := registration.run
		guarded := func(workerContext context.Context) {
			select {
			case <-host.bootstrapReady:
				run(workerContext)
			case <-workerContext.Done():
			}
		}
		if err := workers.Go(registration.name, guarded); err != nil {
			return rollback(err)
		}
	}
	workers.Seal()
	if err := host.requireBootstrapLifecycle(); err != nil {
		return rollback(err)
	}
	if err := host.acquireScopeTransition(ctx); err != nil {
		return rollback(err)
	}
	if err := host.requireBootstrapLifecycle(); err != nil {
		host.releaseScopeTransition()
		return rollback(err)
	}
	if err := host.bootstrapForScope(ctx, initialScope); err != nil {
		host.releaseScopeTransition()
		return rollback(fmt.Errorf("bootstrap connector market initial scope: %w", err))
	}
	host.releaseScopeTransition()
	host.bootstrapReadyOnce.Do(func() { close(host.bootstrapReady) })
	host.lifecycleMu.Lock()
	if host.lifecycleState != LifecycleStateStarting {
		host.lifecycleMu.Unlock()
		return rollback(errors.New("connector market lifecycle changed during initial bootstrap"))
	}
	host.lifecycleState = LifecycleStateRunning
	host.commandAdmission.open()
	host.lifecycleMu.Unlock()
	return nil
}

func (host *Host) requireRunning() error {
	if host == nil {
		return errHostNotRunning
	}
	host.lifecycleMu.Lock()
	defer host.lifecycleMu.Unlock()
	if host.lifecycleState != LifecycleStateRunning {
		return errHostNotRunning
	}
	return nil
}

// commandContext joins the request lifetime to the daemon lifecycle without
// holding lifecycleMu while command I/O runs. Close cancels lifecycleCtx before
// it starts draining admitted commands.
func (host *Host) commandContext(requestCtx context.Context) (context.Context, context.CancelFunc) {
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	host.lifecycleMu.Lock()
	lifecycleCtx := host.lifecycleCtx
	host.lifecycleMu.Unlock()
	ctx, cancel := context.WithCancel(requestCtx)
	if lifecycleCtx == nil {
		cancel()
		return ctx, cancel
	}
	stopLifecycleCancel := context.AfterFunc(lifecycleCtx, cancel)
	return ctx, func() {
		stopLifecycleCancel()
		cancel()
	}
}

func (host *Host) FenceForScope(ctx context.Context, scope contracts.OperationScope) error {
	if host == nil || host.runtimeMaintenance == nil {
		return errors.New("connector market host is unavailable")
	}
	if err := host.acquireScopeTransition(ctx); err != nil {
		return err
	}
	defer host.releaseScopeTransition()
	drained := host.commandAdmission.close()
	select {
	case <-drained:
	case <-ctx.Done():
		return ctx.Err()
	}
	host.bootstrapMu.Lock()
	defer host.bootstrapMu.Unlock()
	previousScope := host.bootstrapScope
	if host.authorizationReadiness != nil && strings.TrimSpace(previousScope.AccountID) != "" {
		host.authorizationReadiness.SetReady(previousScope.AccountID, false)
	}
	if host.authorizationReadiness != nil && strings.TrimSpace(scope.AccountID) != "" {
		host.authorizationReadiness.SetReady(scope.AccountID, false)
	}
	host.bootstrapped = false
	host.runtimeRecoveryPending = make(map[string]struct{})
	host.notifyAuthorizationScopeChanged()
	publicationErr := host.applyCapabilityPublication(ctx, previousScope, false)
	fenceErr := host.activationGate.FailClosed(ctx, time.Now().Add(10*time.Second))
	host.bootstrapScope = scope
	host.activationGate.setOpen(scope, false)
	return errors.Join(publicationErr, fenceErr)
}

// ReconcileRuntimeForScope repairs one observed runtime route under the same
// lifecycle gate as bootstrap and fencing. The operation is awaited while the
// gate is held so a concurrent runtime replacement cannot fence its generation
// after acceptance but before the VM receipt is committed.
func (host *Host) ReconcileRuntimeForScope(ctx context.Context, scope contracts.OperationScope, connectorKey string) error {
	if host == nil || host.runtimeMaintenance == nil || host.catalogQueries == nil {
		return errors.New("connector market host is unavailable")
	}
	if !host.bootstrapMu.TryLock() {
		// A bootstrap, fence, or earlier repair already owns convergence. The
		// observer will verify a fresh VM snapshot after that operation finishes.
		return nil
	}
	defer host.bootstrapMu.Unlock()
	if !host.bootstrapped || host.bootstrapScope != scope || host.activationGate.requiresRecovery() {
		// Bootstrap owns convergence while the lifecycle gate is closed. Enqueuing
		// a second per-Connector operation here would race its generation fence.
		return nil
	}
	return host.reconcileRuntimeForScopeLockedMode(ctx, scope, connectorKey, true)
}

func (host *Host) reconcileRuntimeForScopeLocked(ctx context.Context, scope contracts.OperationScope, connectorKey string) error {
	return host.reconcileRuntimeForScopeLockedMode(ctx, scope, connectorKey, false)
}

func (host *Host) reconcileRuntimeForScopeLockedMode(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
	force bool,
) error {
	connector, err := host.catalogQueries.GetConnectorForScope(ctx, scope, connectorKey)
	if errors.Is(err, contracts.ErrNotFound) || err == nil && connector.Installation.State != contracts.InstallationStateInstalled {
		return nil
	}
	if err != nil {
		return err
	}
	if force {
		return host.runtimeMaintenance.ReconcileRuntimeAfterInvalidation(ctx, scope, connectorKey)
	}
	return host.runtimeMaintenance.ReconcileRuntimeDesired(ctx, scope, connectorKey)
}

// ObserveAuthorizationForScope commits account authorization and its runtime
// reconcile under the lifecycle gate. This prevents authorization callbacks
// from publishing a generation concurrently with bootstrap recovery.
func (host *Host) ObserveAuthorizationForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	projection contracts.AuthorizationProjection,
) error {
	if host == nil || host.authorizationMaintenance == nil {
		return errors.New("connector market host is unavailable")
	}
	host.bootstrapMu.Lock()
	defer host.bootstrapMu.Unlock()
	if err := host.authorizationMaintenance.ProjectAuthorization(ctx, scope, projection); err != nil {
		return err
	}
	return host.reconcileRuntimeForScopeLocked(ctx, scope, projection.ConnectorKey)
}

func (host *Host) recoverAndWait(ctx context.Context) error {
	operations, err := host.operationRecovery.RecoverableOperations(ctx)
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}
	if err := host.recoveryControl.Recover(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		pending := false
		for _, candidate := range operations {
			// Remote refresh and authorization operations may legitimately wait on
			// the network. Recover them, but do not make local route restoration
			// wait for their terminal state.
			if candidate.Kind != contracts.OperationKindInstall && candidate.Kind != contracts.OperationKindUninstall &&
				candidate.Kind != contracts.OperationKindReconcileRuntime {
				continue
			}
			operation, err := host.recoveryControl.GetOperation(ctx, candidate.OperationID)
			if err != nil {
				return err
			}
			if operation.State == contracts.OperationStateAccepted || operation.State == contracts.OperationStateRunning {
				pending = true
			}
		}
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (host *Host) refreshAndWait(ctx context.Context) error {
	snapshot, err := host.recoveryControl.Snapshot(ctx)
	if err != nil {
		return err
	}
	result, err := host.catalogMaintenance.RefreshCatalog(ctx, contracts.Mutation{
		ClientRequestID: "daemon-refresh-" + uuid.NewString(), ExpectedRevision: snapshot.Revision,
	})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		operation, err := host.recoveryControl.GetOperation(ctx, result.Operation.OperationID)
		if err != nil {
			return err
		}
		switch operation.State {
		case contracts.OperationStateCompleted:
			return nil
		case contracts.OperationStateFailed:
			return fmt.Errorf("connector market refresh failed: %s", operation.FailureCode)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (host *Host) runCatalogRefreshWorker(ctx context.Context) {
	delay := host.catalogRefreshInitialDelay
	backoff := time.Minute
	for {
		if delay > 0 {
			wait := host.catalogRetryWait
			if wait == nil {
				wait = waitPhysicalRouteWatchRetry
			}
			if !wait(ctx, delay) {
				return
			}
		}
		refreshContext, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := host.refreshAndWait(refreshContext)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !errors.Is(err, context.Canceled) {
				slog.Warn("connector market scheduled refresh failed", "error", err)
			}
			delay = host.catalogRetryDelay(backoff)
			reportWorkerFailure(ctx, workerFailureCatalogRefresh, delay)
			backoff = nextBoundedRetry(backoff, 5*time.Minute)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		reportWorkerSuccess(ctx)
		backoff = time.Minute
		delay = host.catalogRetryDelay(time.Minute)
	}
}

func (host *Host) catalogRetryDelay(base time.Duration) time.Duration {
	delay := host.catalogRetryJitter(base)
	if delay < base/2 {
		return base / 2
	}
	if delay > base {
		return base
	}
	return delay
}

// CatalogOnlyPorts deliberately advertise no installable implementation. The
// host can safely expose remote browsing before a concrete runtime activator,
// artifact resolver, and authorization provider are registered.
func CatalogOnlyPorts() (
	application.ReleaseInstallationManager,
	application.ImplementationCommands,
	application.AuthorizationProvider,
	application.CompatibilityEvaluator,
	application.ImplementationRegistry,
) {
	return unavailableReleaseInstaller{}, unavailableRuntime{}, unavailableAuthorization{},
		rejectingCompatibility{}, application.NewImplementationRegistry(nil)
}

type unavailableReleaseInstaller struct{}

func (unavailableReleaseInstaller) InstallRelease(context.Context, contracts.InstallReleaseRequest) (contracts.ReleaseInstallationReceipt, error) {
	return contracts.ReleaseInstallationReceipt{}, errors.New("connector release installation is not registered")
}

func (unavailableReleaseInstaller) InspectReleaseInstallation(_ context.Context, request contracts.InspectReleaseInstallationRequest) (contracts.ReleaseInstallationObservation, error) {
	return contracts.ReleaseInstallationObservation{State: contracts.ReleaseInstallationIndeterminate,
		ConnectorKey: request.Release.ConnectorKey, ReleaseDigest: request.Release.ReleaseDigest,
		ReasonCode: "release_installation_manager_unavailable"}, nil
}

func (unavailableReleaseInstaller) CommitReleaseInstallation(context.Context, contracts.CommitReleaseInstallationRequest) error {
	return errors.New("connector release installation is not registered")
}

func (unavailableReleaseInstaller) UninstallRelease(context.Context, contracts.UninstallReleaseRequest) error {
	return errors.New("connector release installation is not registered")
}

type unavailableRuntime struct{}

func (unavailableRuntime) Snapshot(context.Context) (contracts.PhysicalRouteSnapshot, error) {
	return contracts.PhysicalRouteSnapshot{}, nil
}

func (unavailableRuntime) Watch(ctx context.Context) (contracts.PhysicalRouteWatch, error) {
	events := make(chan contracts.PhysicalRouteEvent)
	go func() {
		<-ctx.Done()
		close(events)
	}()
	return contracts.PhysicalRouteWatch{Events: events}, nil
}

func (unavailableRuntime) Reconcile(context.Context, contracts.RuntimeReconcileRequest) (contracts.RuntimeReceipt, error) {
	return contracts.RuntimeReceipt{}, errors.New("connector implementation host is not registered")
}

func (unavailableRuntime) DeactivateRuntime(context.Context, contracts.RuntimeDeactivationRequest) error {
	return errors.New("connector runtime is not registered")
}

func (unavailableRuntime) FailClosed(context.Context, time.Time) error {
	return errors.New("connector runtime is not registered")
}

func (unavailableRuntime) Close(context.Context) error { return nil }

type unavailableAuthorization struct{}

func (unavailableAuthorization) Begin(context.Context, contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	return contracts.AuthorizationSession{}, errors.New("connector authorization is not registered")
}

func (unavailableAuthorization) Disconnect(context.Context, contracts.AuthorizationDisconnectRequest) error {
	return errors.New("connector authorization is not registered")
}

type rejectingCompatibility struct{}

func (rejectingCompatibility) Evaluate(contracts.Manifest) contracts.Compatibility {
	return contracts.Compatibility{
		State:  contracts.CompatibilityStateUnsupportedVersion,
		Reason: "connector_runtime_not_registered",
	}
}
