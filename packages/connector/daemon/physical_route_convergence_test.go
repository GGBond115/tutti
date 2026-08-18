package daemon

import (
	"context"
	"errors"
	"fmt"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	marketdata "github.com/tutti-os/tutti/packages/connector/store-sqlite"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type convergencePhysicalRuntime struct {
	activationGateDelegate

	mu             sync.Mutex
	revision       uint64
	route          *contracts.PhysicalRoute
	nextWatcherID  uint64
	watchers       map[uint64]chan contracts.PhysicalRouteEvent
	reconciles     int
	reconciled     chan int
	suppressEvents bool
	snapshotCalls  int
	deactivations  []contracts.RuntimeDeactivationRequest
}

func newConvergencePhysicalRuntime() *convergencePhysicalRuntime {
	return &convergencePhysicalRuntime{watchers: make(map[uint64]chan contracts.PhysicalRouteEvent), reconciled: make(chan int, 16)}
}

func (runtime *convergencePhysicalRuntime) Reconcile(
	_ context.Context,
	request contracts.RuntimeReconcileRequest,
) (contracts.RuntimeReceipt, error) {
	runtime.mu.Lock()
	runtime.reconciles++
	count := runtime.reconciles
	runtime.revision++
	route := contracts.PhysicalRoute{ConnectorKey: request.Connector.Key, ConnectionID: request.ConnectionID,
		ReleaseDigest: request.Connector.Release.ReleaseDigest, Generation: request.Generation,
		State: contracts.PhysicalRouteStateReady}
	if request.Enabled {
		runtime.route = &route
	} else {
		runtime.route = nil
	}
	runtime.publishLocked(contracts.PhysicalRouteEvent{Revision: runtime.revision, Kind: contracts.PhysicalRouteEventChanged, Route: route})
	runtime.mu.Unlock()
	runtime.reconciled <- count
	return contracts.RuntimeReceipt{OperationID: request.OperationID, ConnectionID: request.ConnectionID,
		ConnectorKey: request.Connector.Key, ReleaseDigest: request.Connector.Release.ReleaseDigest,
		Generation: request.Generation, Readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady,
			Interfaces: []contracts.InterfaceReadiness{{Kind: "mcp", State: contracts.RuntimeReadinessReady}}},
		Summary: &contracts.ConnectorSummary{Key: request.Connector.Key, Name: request.Connector.Key,
			Interfaces: []contracts.ConnectorInterfaceSummary{{Kind: "mcp", ServerName: "connector",
				Status: string(contracts.RuntimeReadinessReady)}}}}, nil
}

func (runtime *convergencePhysicalRuntime) DeactivateRuntime(
	_ context.Context,
	request contracts.RuntimeDeactivationRequest,
) error {
	runtime.mu.Lock()
	runtime.deactivations = append(runtime.deactivations, request)
	runtime.mu.Unlock()
	runtime.clearRoute(false)
	return nil
}

func (runtime *convergencePhysicalRuntime) FailClosed(context.Context, time.Time) error {
	runtime.clearRoute(false)
	return nil
}

func (runtime *convergencePhysicalRuntime) Close(context.Context) error { return nil }

func (runtime *convergencePhysicalRuntime) clearRoute(dropEvent bool) {
	runtime.mu.Lock()
	runtime.revision++
	previous := contracts.PhysicalRoute{}
	if runtime.route != nil {
		previous = *runtime.route
	}
	runtime.route = nil
	if !dropEvent {
		runtime.publishLocked(contracts.PhysicalRouteEvent{Revision: runtime.revision,
			Kind: contracts.PhysicalRouteEventChanged, Route: previous})
	}
	runtime.mu.Unlock()
}

func (runtime *convergencePhysicalRuntime) unexpectedExit(dropEvent bool) {
	runtime.mu.Lock()
	runtime.revision++
	previous := contracts.PhysicalRoute{}
	if runtime.route != nil {
		previous = *runtime.route
	}
	runtime.route = nil
	if !dropEvent {
		runtime.publishLocked(contracts.PhysicalRouteEvent{Revision: runtime.revision,
			Kind: contracts.PhysicalRouteEventUnexpectedExit, Route: previous})
	}
	runtime.mu.Unlock()
}

func (runtime *convergencePhysicalRuntime) Snapshot(ctx context.Context) (contracts.PhysicalRouteSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return contracts.PhysicalRouteSnapshot{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.snapshotCalls++
	snapshot := contracts.PhysicalRouteSnapshot{Revision: runtime.revision}
	if runtime.route != nil {
		snapshot.Routes = []contracts.PhysicalRoute{*runtime.route}
	}
	return snapshot, nil
}

func (runtime *convergencePhysicalRuntime) snapshotCount() int {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.snapshotCalls
}

func (runtime *convergencePhysicalRuntime) Watch(ctx context.Context) (contracts.PhysicalRouteWatch, error) {
	runtime.mu.Lock()
	runtime.nextWatcherID++
	watcherID := runtime.nextWatcherID
	events := make(chan contracts.PhysicalRouteEvent, 8)
	runtime.watchers[watcherID] = events
	revision := runtime.revision
	runtime.mu.Unlock()
	go func() {
		<-ctx.Done()
		runtime.mu.Lock()
		if runtime.watchers[watcherID] == events {
			delete(runtime.watchers, watcherID)
			close(events)
		}
		runtime.mu.Unlock()
	}()
	return contracts.PhysicalRouteWatch{Revision: revision, Events: events}, nil
}

func (runtime *convergencePhysicalRuntime) publishLocked(event contracts.PhysicalRouteEvent) {
	if runtime.suppressEvents {
		return
	}
	for _, watcher := range runtime.watchers {
		select {
		case watcher <- event:
		default:
			// A full test watcher models a production overflow: the consumer
			// must recover from the next bounded Snapshot.
		}
	}
}

func TestUnexpectedManagedRouteExitAutomaticallyReconcilesPhysicalDrift(t *testing.T) {
	testPhysicalRouteRecovery(t, false)
}

func TestPeriodicSnapshotRecoversManagedRouteWhenExitEventIsLost(t *testing.T) {
	testPhysicalRouteRecovery(t, true)
}

func TestRepeatedEarlyExitStopsAutomaticStartsAtFailureBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime := newConvergencePhysicalRuntime()
	host, store, connector, _ := newPhysicalRouteRecoveryHost(t, runtime)
	host.physicalAntiEntropyInterval = 50 * time.Millisecond
	if err := host.Start(ctx, contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = host.Close(closeContext)
	})
	if err := host.ActivateScope(ctx, contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	reconcileCount := waitForRuntimeReconcile(t, ctx, runtime.reconciled, 1)
	observed := waitForObservedRuntimeConvergence(t, ctx, store, connector.Key, 0)
	for failure := uint32(1); failure <= contracts.RuntimeFailureBudget; failure++ {
		runtime.unexpectedExit(false)
		if failure < contracts.RuntimeFailureBudget {
			reconcileCount = waitForRuntimeReconcile(t, ctx, runtime.reconciled, reconcileCount+1)
			observed = waitForObservedRuntimeConvergence(t, ctx, store, connector.Key, observed.Desired.Generation)
			if failure == contracts.RuntimeFailureDegradedThreshold &&
				(observed.Observed.Readiness.State != contracts.RuntimeReadinessDegraded ||
					observed.Observed.Readiness.ReasonCode != contracts.RuntimeReadinessReasonFailureBudgetDegraded) {
				t.Fatalf("successful restart erased degraded failure budget = %#v", observed)
			}
		}
	}
	time.Sleep(2*runtimeConvergenceScanInterval + 100*time.Millisecond)
	convergence, err := store.RuntimeConvergence(ctx, contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if convergence.Attempt != contracts.RuntimeFailureBudget ||
		convergence.Observed.Readiness.State != contracts.RuntimeReadinessFailed ||
		convergence.Observed.Readiness.ReasonCode != contracts.RuntimeReadinessReasonFailureBudgetExhausted {
		t.Fatalf("early-exit failure budget = %#v", convergence)
	}
	runtime.mu.Lock()
	runtimeStarts := runtime.reconciles
	runtime.mu.Unlock()
	if runtimeStarts != int(contracts.RuntimeFailureBudget) {
		// One initial start plus five automatic restarts; the sixth exit is
		// suppressed for this desired generation.
		t.Fatalf("runtime starts = %d, want %d", runtimeStarts, contracts.RuntimeFailureBudget)
	}
}

type scriptedRouteObservation struct {
	watch    contracts.PhysicalRouteWatch
	watchErr error
}

func (observation scriptedRouteObservation) Snapshot(context.Context) (contracts.PhysicalRouteSnapshot, error) {
	return contracts.PhysicalRouteSnapshot{}, nil
}

func (observation scriptedRouteObservation) Watch(context.Context) (contracts.PhysicalRouteWatch, error) {
	return observation.watch, observation.watchErr
}

func TestPhysicalRouteWatchGapAndFailureScheduleFreshSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		observation scriptedRouteObservation
	}{
		{name: "watch creation failure", observation: scriptedRouteObservation{watchErr: errors.New("watch unavailable")}},
		{name: "nil stream", observation: scriptedRouteObservation{}},
		{name: "closed stream", observation: func() scriptedRouteObservation {
			events := make(chan contracts.PhysicalRouteEvent)
			close(events)
			return scriptedRouteObservation{watch: contracts.PhysicalRouteWatch{Revision: 4, Events: events}}
		}()},
		{name: "revision gap", observation: func() scriptedRouteObservation {
			events := make(chan contracts.PhysicalRouteEvent, 1)
			events <- contracts.PhysicalRouteEvent{Revision: 6, Kind: contracts.PhysicalRouteEventChanged}
			return scriptedRouteObservation{watch: contracts.PhysicalRouteWatch{Revision: 4, Events: events}}
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			host := &Host{physicalRoutes: test.observation, runtimePhysicalWake: make(chan struct{}, 1)}
			done := make(chan struct{})
			go func() {
				host.runPhysicalRouteWatchWorker(ctx)
				close(done)
			}()
			select {
			case <-host.runtimePhysicalWake:
				// The convergence worker consumes this coalesced hint and obtains
				// the authoritative Snapshot outside the Watch reader.
			case <-time.After(time.Second):
				t.Fatal("watch invalidation did not schedule a fresh snapshot")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("watch worker did not stop")
			}
		})
	}
}

func TestPhysicalRuntimeMatchRequiresOneExactReadyRoute(t *testing.T) {
	desired := contracts.RuntimeDesired{Enabled: true, ConnectionID: "device-1", ReleaseDigest: "release-1", Generation: 7}
	exact := contracts.PhysicalRoute{ConnectorKey: "github", ConnectionID: "device-1", ReleaseDigest: "release-1",
		Generation: contracts.HostGeneration{BootEpoch: "boot-1", Generation: 7}, State: contracts.PhysicalRouteStateReady}
	if !physicalRuntimeMatchesDesired(desired, "boot-1", []contracts.PhysicalRoute{exact}) {
		t.Fatal("exact physical route did not satisfy desired")
	}
	for name, routes := range map[string][]contracts.PhysicalRoute{
		"missing":   nil,
		"duplicate": {exact, exact},
		"degraded": {func() contracts.PhysicalRoute {
			route := exact
			route.State = contracts.PhysicalRouteStateDegraded
			return route
		}()},
		"wrong boot": {func() contracts.PhysicalRoute {
			route := exact
			route.Generation.BootEpoch = "old-boot"
			return route
		}()},
	} {
		if physicalRuntimeMatchesDesired(desired, "boot-1", routes) {
			t.Fatalf("%s physical route unexpectedly satisfied desired", name)
		}
	}
	desired.Enabled = false
	if !physicalRuntimeMatchesDesired(desired, "boot-1", nil) ||
		physicalRuntimeMatchesDesired(desired, "boot-1", []contracts.PhysicalRoute{exact}) {
		t.Fatal("disabled desired did not require an empty physical snapshot")
	}
}

func testPhysicalRouteRecovery(t *testing.T, dropExitEvent bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime := newConvergencePhysicalRuntime()
	runtime.suppressEvents = dropExitEvent
	host, store, connector, _ := newPhysicalRouteRecoveryHost(t, runtime)
	if dropExitEvent {
		host.physicalAntiEntropyInterval = 50 * time.Millisecond
	}
	if err := host.Start(ctx, contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		if err := host.Close(closeContext); err != nil {
			t.Errorf("close connector host: %v", err)
		}
	})
	if err := host.ActivateScope(ctx, contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	firstCount := waitForRuntimeReconcile(t, ctx, runtime.reconciled, 1)
	before := waitForObservedRuntimeConvergence(t, ctx, store, connector.Key, 0)
	if before.Observed.DesiredGeneration != before.Desired.Generation {
		t.Fatalf("initial convergence = %#v", before)
	}

	runtime.unexpectedExit(dropExitEvent)
	secondCount := waitForRuntimeReconcile(t, ctx, runtime.reconciled, firstCount+1)
	if secondCount <= firstCount {
		t.Fatalf("runtime reconcile count = %d, want greater than %d", secondCount, firstCount)
	}
	after := waitForObservedRuntimeConvergence(t, ctx, store, connector.Key, before.Desired.Generation)
	if after.Desired.Generation <= before.Desired.Generation ||
		after.Observed.DesiredGeneration != after.Desired.Generation || after.Observed.BootEpoch == "" {
		t.Fatalf("recovered convergence = %#v; before = %#v", after, before)
	}
	physical, err := runtime.Snapshot(ctx)
	if err != nil || len(physical.Routes) != 1 ||
		physical.Routes[0].Generation.Generation != after.Desired.Generation {
		t.Fatalf("recovered physical snapshot = %#v, error = %v", physical, err)
	}
}

func waitForObservedRuntimeConvergence(
	t *testing.T,
	ctx context.Context,
	store *marketdata.Store,
	connectorKey string,
	minimumGeneration uint64,
) contracts.RuntimeConvergence {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		convergence, err := store.RuntimeConvergence(ctx, contracts.OperationScope{}, connectorKey)
		if err == nil && convergence.Desired.Generation > minimumGeneration &&
			convergence.Observed.DesiredGeneration == convergence.Desired.Generation && convergence.LeaseOwner == "" {
			return convergence
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for observed runtime convergence after %d: state=%#v error=%v",
				minimumGeneration, convergence, err)
		case <-ticker.C:
		}
	}
}

func TestDurableScanDoesNotAmplifyPhysicalSnapshotsAndWatchWakeCoalesces(t *testing.T) {
	runtime := newConvergencePhysicalRuntime()
	host, _, _, _ := newPhysicalRouteRecoveryHost(t, runtime)
	host.bootstrapMu.Lock()
	host.bootstrapped = true
	host.bootstrapMu.Unlock()
	host.physicalAntiEntropyInterval = time.Hour
	host.physicalAntiEntropyJitter = func(maximum time.Duration) time.Duration { return maximum }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		host.runRuntimeConvergenceWorker(ctx)
		close(done)
	}()
	time.Sleep(2*runtimeConvergenceScanInterval + 100*time.Millisecond)
	if calls := runtime.snapshotCount(); calls != 0 {
		cancel()
		<-done
		t.Fatalf("500ms durable scans triggered %d physical snapshots", calls)
	}
	cancel()
	<-done
	for index := 0; index < 20; index++ {
		host.notifyPhysicalRouteChanged()
	}
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() {
		host.runRuntimeConvergenceWorker(ctx)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for runtime.snapshotCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if calls := runtime.snapshotCount(); calls != 1 {
		cancel()
		<-done
		t.Fatalf("coalesced Watch burst triggered %d physical snapshots, want 1", calls)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime convergence worker did not stop")
	}
}

func TestPhysicalAntiEntropyUsesIndependentConfiguredInterval(t *testing.T) {
	runtime := newConvergencePhysicalRuntime()
	host, _, _, _ := newPhysicalRouteRecoveryHost(t, runtime)
	host.bootstrapMu.Lock()
	host.bootstrapped = true
	host.bootstrapMu.Unlock()
	host.physicalAntiEntropyInterval = 25 * time.Millisecond
	host.physicalAntiEntropyJitter = func(maximum time.Duration) time.Duration { return maximum }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		host.runRuntimeConvergenceWorker(ctx)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for runtime.snapshotCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if calls := runtime.snapshotCount(); calls < 2 {
		t.Fatalf("configured physical anti-entropy snapshots = %d, want at least 2", calls)
	}
}

func TestPhysicalOrphanRemovalIsLimitedToKnownIdentityFromCurrentBoot(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*contracts.PhysicalRoute)
		wantRemove bool
		wantError  bool
	}{
		{name: "current boot", wantRemove: true},
		{name: "other boot", mutate: func(route *contracts.PhysicalRoute) { route.Generation.BootEpoch = "other-boot" }, wantError: true},
		{name: "unknown identity", mutate: func(route *contracts.PhysicalRoute) { route.ConnectionID = "" }, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := newConvergencePhysicalRuntime()
			host, _, _, _ := newPhysicalRouteRecoveryHost(t, runtime)
			host.bootstrapMu.Lock()
			host.bootstrapped = true
			host.bootstrapMu.Unlock()
			route := contracts.PhysicalRoute{ConnectorKey: "orphan", ConnectionID: "device-orphan",
				ReleaseDigest: "orphan-release", Generation: contracts.HostGeneration{
					BootEpoch: host.runtimeMaintenance.RuntimeBootEpoch(), Generation: 7}, State: contracts.PhysicalRouteStateReady}
			if test.mutate != nil {
				test.mutate(&route)
			}
			runtime.mu.Lock()
			runtime.route = &route
			runtime.revision++
			runtime.mu.Unlock()
			err := host.reconcilePhysicalRouteSnapshot(context.Background())
			if (err != nil) != test.wantError {
				t.Fatalf("orphan scan error = %v, want error=%t", err, test.wantError)
			}
			runtime.mu.Lock()
			deactivations := append([]contracts.RuntimeDeactivationRequest(nil), runtime.deactivations...)
			remaining := runtime.route != nil
			runtime.mu.Unlock()
			if (len(deactivations) == 1) != test.wantRemove || remaining == test.wantRemove {
				t.Fatalf("deactivations=%#v remaining=%t wantRemove=%t", deactivations, remaining, test.wantRemove)
			}
		})
	}
}

func TestHealthyPeriodicObservationResetsPersistedFailureBudget(t *testing.T) {
	runtime := newConvergencePhysicalRuntime()
	host, store, connector, _ := newPhysicalRouteRecoveryHost(t, runtime)
	host.bootstrapMu.Lock()
	host.bootstrapped = true
	host.bootstrapMu.Unlock()
	now := time.Now().UTC()
	bootEpoch := host.runtimeMaintenance.RuntimeBootEpoch()
	route := contracts.PhysicalRoute{ConnectorKey: connector.Key, ConnectionID: "device-healthy",
		ReleaseDigest: connector.Release.ReleaseDigest, Generation: contracts.HostGeneration{BootEpoch: bootEpoch, Generation: 7},
		State: contracts.PhysicalRouteStateReady}
	runtime.mu.Lock()
	runtime.route = &route
	runtime.revision++
	runtime.mu.Unlock()
	convergence := contracts.RuntimeConvergence{
		Desired: contracts.RuntimeDesired{ConnectorKey: connector.Key, Generation: 7, Enabled: true,
			ConnectionID: route.ConnectionID, ReleaseDigest: route.ReleaseDigest, UpdatedAt: now},
		Observed: contracts.RuntimeObserved{DesiredGeneration: 7, BootEpoch: bootEpoch, Enabled: true,
			ConnectionID: route.ConnectionID, ReleaseDigest: route.ReleaseDigest,
			Readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady}, ObservedAt: now},
		Attempt: 3, NextAttemptAt: now.Add(time.Minute), LastErrorCode: "unavailable", LastError: "early exit", UpdatedAt: now,
	}
	if err := store.Transaction(context.Background(), func(tx application.Transaction) error {
		return tx.SaveRuntimeConvergence(convergence)
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.reconcilePhysicalRouteSnapshotWithPolicy(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	stored, err := store.RuntimeConvergence(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Attempt != 0 || !stored.NextAttemptAt.IsZero() || stored.LastErrorCode != "" {
		t.Fatalf("healthy observation did not reset failure budget = %#v", stored)
	}
}

type countingRuntimeRepository struct {
	application.Repository
	mu         sync.Mutex
	batchReads int
	pointReads int
}

func (repository *countingRuntimeRepository) RuntimeConvergence(
	ctx context.Context, scope contracts.OperationScope, connectorKey string,
) (contracts.RuntimeConvergence, error) {
	repository.mu.Lock()
	repository.pointReads++
	repository.mu.Unlock()
	return repository.Repository.RuntimeConvergence(ctx, scope, connectorKey)
}

func (repository *countingRuntimeRepository) RuntimeConvergences(
	ctx context.Context, scope contracts.OperationScope, limit int,
) ([]contracts.RuntimeConvergence, error) {
	repository.mu.Lock()
	repository.batchReads++
	repository.mu.Unlock()
	return repository.Repository.RuntimeConvergences(ctx, scope, limit)
}

func (repository *countingRuntimeRepository) resetReads() {
	repository.mu.Lock()
	repository.batchReads, repository.pointReads = 0, 0
	repository.mu.Unlock()
}

func (repository *countingRuntimeRepository) reads() (int, int) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.batchReads, repository.pointReads
}

func TestPhysicalRouteScanUsesOneRepositoryReadForManyConnectors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime := newConvergencePhysicalRuntime()
	host, store, _, repository := newPhysicalRouteRecoveryHost(t, runtime)
	host.bootstrapMu.Lock()
	host.bootstrapped = true
	host.bootstrapScope = contracts.OperationScope{}
	host.bootstrapMu.Unlock()
	now := time.Now().UTC()
	bootEpoch := host.runtimeMaintenance.RuntimeBootEpoch()
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		for index := 0; index < 100; index++ {
			connectorKey := fmt.Sprintf("batch-%03d", index)
			convergence := contracts.RuntimeConvergence{
				Desired:       contracts.RuntimeDesired{ConnectorKey: connectorKey, Generation: 1, Enabled: false, UpdatedAt: now},
				Observed:      contracts.RuntimeObserved{DesiredGeneration: 1, BootEpoch: bootEpoch, Enabled: false, ObservedAt: now},
				NextAttemptAt: now, UpdatedAt: now,
			}
			if err := tx.SaveRuntimeConvergence(convergence); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	repository.resetReads()
	if err := host.reconcilePhysicalRouteSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	batchReads, pointReads := repository.reads()
	if batchReads != 1 || pointReads != 0 {
		t.Fatalf("100-connector scan used batch reads=%d point reads=%d", batchReads, pointReads)
	}
}

func waitForRuntimeReconcile(t *testing.T, ctx context.Context, reconciled <-chan int, target int) int {
	t.Helper()
	count := 0
	for count < target {
		select {
		case count = <-reconciled:
		case <-ctx.Done():
			t.Fatalf("wait for runtime reconcile %d: %v", target, ctx.Err())
		}
	}
	return count
}

func newPhysicalRouteRecoveryHost(
	t *testing.T,
	runtime *convergencePhysicalRuntime,
) (*Host, *marketdata.Store, contracts.Connector, *countingRuntimeRepository) {
	t.Helper()
	ctx := context.Background()
	store, err := marketdata.Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	release := hostTestRelease()
	connector := contracts.Connector{Key: release.ConnectorKey, Release: release,
		Installation: contracts.Installation{State: contracts.InstallationStateInstalled,
			InstalledVersion: release.Version, InstalledReleaseID: release.ReleaseID,
			InstalledReleaseDigest: release.ReleaseDigest},
		Authorization: contracts.Authorization{State: contracts.AuthorizationStateNotRequired},
		Compatibility: contracts.Compatibility{State: contracts.CompatibilityStateSupported}}
	installedRelease := release
	operation := contracts.Operation{OperationID: "install-physical-route", ClientRequestID: "install-physical-route",
		ConnectorKey: connector.Key, Kind: contracts.OperationKindInstall, State: contracts.OperationStateCompleted,
		Stage: contracts.OperationStageCompleted, Target: &contracts.OperationTarget{ConnectorKey: connector.Key,
			Version: release.Version, ReleaseID: release.ReleaseID, ReleaseDigest: release.ReleaseDigest,
			ArtifactSHA256: release.Artifact.SHA256, Release: &installedRelease},
		CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		connector.Revision = tx.AdvanceRevision()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		return tx.SaveOperation(operation)
	}); err != nil {
		t.Fatal(err)
	}
	repository := &countingRuntimeRepository{Repository: store}
	host, err := NewHost(HostConfig{Repository: repository, CatalogSource: &countingCatalogSource{release: release},
		ReleaseInstallations: runtime, ImplementationCommands: runtime, PhysicalRoutes: runtime,
		PhysicalAntiEntropyInterval: time.Hour,
		RuntimeBindings: runtimeBindingResolverFunc(func(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
			return contracts.RuntimeBinding{ConnectionID: "device-physical-route", Enabled: true,
				AuthorizationState: contracts.AuthorizationStateNotRequired}, nil
		}),
		Authorization: unavailableAuthorization{}, Compatibility: rejectingCompatibility{},
		ImplementationRegistry: application.NewImplementationRegistry(nil), Outbox: store, Lifecycle: store,
		Publisher: discardChangedEventPublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	host.catalogRefreshInitialDelay = time.Hour
	host.physicalAntiEntropyJitter = func(maximum time.Duration) time.Duration { return maximum }
	return host, store, connector, repository
}

var _ application.RouteObservation = (*convergencePhysicalRuntime)(nil)
var _ application.ImplementationCommands = (*convergencePhysicalRuntime)(nil)
var _ application.ReleaseInstallationManager = (*convergencePhysicalRuntime)(nil)
