package daemon

import (
	"context"
	"errors"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	marketdata "github.com/tutti-os/tutti/packages/connector/store-sqlite"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type countingOutbox struct {
	mu    sync.Mutex
	reads int
}

func (outbox *countingOutbox) PendingChangedEvents(context.Context, int) ([]contracts.ChangedEventRecord, error) {
	outbox.mu.Lock()
	outbox.reads++
	outbox.mu.Unlock()
	return nil, nil
}

func (*countingOutbox) MarkChangedEventPublished(context.Context, int64, time.Time) error {
	return nil
}

func (outbox *countingOutbox) readCount() int {
	outbox.mu.Lock()
	defer outbox.mu.Unlock()
	return outbox.reads
}

type failingPublicationController struct {
	err   error
	calls int
}

type synchronizedPublicationController struct {
	mu     sync.Mutex
	values []publicationTransition
}

type publicationTransition struct {
	scope   contracts.OperationScope
	enabled bool
}

func (controller *synchronizedPublicationController) ApplyCapabilityPublication(_ context.Context, scope contracts.OperationScope, enabled bool) error {
	controller.mu.Lock()
	controller.values = append(controller.values, publicationTransition{scope: scope, enabled: enabled})
	controller.mu.Unlock()
	return nil
}

func (controller *synchronizedPublicationController) snapshot() []publicationTransition {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return append([]publicationTransition(nil), controller.values...)
}

func (controller *failingPublicationController) ApplyCapabilityPublication(context.Context, contracts.OperationScope, bool) error {
	controller.calls++
	return controller.err
}

type blockingOutbox struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type blockingPublicationController struct {
	mu      sync.Mutex
	values  []publicationTransition
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type armedPublicationController struct {
	mu      sync.Mutex
	values  []publicationTransition
	armed   bool
	entered chan struct{}
	release chan struct{}
}

func (controller *armedPublicationController) arm() {
	controller.mu.Lock()
	controller.armed = true
	controller.mu.Unlock()
}

func (controller *armedPublicationController) ApplyCapabilityPublication(
	_ context.Context,
	scope contracts.OperationScope,
	enabled bool,
) error {
	controller.mu.Lock()
	controller.values = append(controller.values, publicationTransition{scope: scope, enabled: enabled})
	block := enabled && controller.armed
	if block {
		controller.armed = false
	}
	controller.mu.Unlock()
	if block {
		close(controller.entered)
		<-controller.release
	}
	return nil
}

func (controller *armedPublicationController) snapshot() []publicationTransition {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return append([]publicationTransition(nil), controller.values...)
}

func (controller *blockingPublicationController) ApplyCapabilityPublication(
	_ context.Context,
	scope contracts.OperationScope,
	enabled bool,
) error {
	controller.mu.Lock()
	controller.values = append(controller.values, publicationTransition{scope: scope, enabled: enabled})
	controller.mu.Unlock()
	controller.once.Do(func() { close(controller.entered) })
	<-controller.release
	return nil
}

func (controller *blockingPublicationController) snapshot() []publicationTransition {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return append([]publicationTransition(nil), controller.values...)
}

func (outbox *blockingOutbox) PendingChangedEvents(context.Context, int) ([]contracts.ChangedEventRecord, error) {
	outbox.once.Do(func() { close(outbox.entered) })
	<-outbox.release
	return nil, nil
}

func (*blockingOutbox) MarkChangedEventPublished(context.Context, int64, time.Time) error {
	return nil
}

type closableActivationGateDelegate struct {
	activationGateDelegate
	mu         sync.Mutex
	closeCalls int
}

func (delegate *closableActivationGateDelegate) Close(context.Context) error {
	delegate.mu.Lock()
	delegate.closeCalls++
	delegate.mu.Unlock()
	return nil
}

func (delegate *closableActivationGateDelegate) closes() int {
	delegate.mu.Lock()
	defer delegate.mu.Unlock()
	return delegate.closeCalls
}

func newLifecycleTestHost(
	t *testing.T,
	outbox application.ChangedEventOutbox,
	lifecycle application.LifecycleCleanupStore,
	publication CapabilityPublicationController,
	runtime interface {
		application.ImplementationCommands
		application.RouteObservation
	},
) *Host {
	t.Helper()
	store, err := marketdata.Open(context.Background(), filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if outbox == nil {
		outbox = store
	}
	if lifecycle == nil {
		lifecycle = store
	}
	if runtime == nil {
		runtime = &activationGateDelegate{}
	}
	host, err := NewHost(HostConfig{
		Repository:             store,
		CatalogSource:          &countingCatalogSource{release: hostTestRelease()},
		ReleaseInstallations:   runtime.(application.ReleaseInstallationManager),
		ImplementationCommands: runtime,
		PhysicalRoutes:         runtime,
		Authorization:          unavailableAuthorization{},
		Compatibility:          rejectingCompatibility{},
		ImplementationRegistry: application.NewImplementationRegistry(nil),
		Outbox:                 outbox,
		Lifecycle:              lifecycle,
		Publisher:              discardChangedEventPublisher{},
		Publication:            publication,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Errorf("close connector host: %v", err)
		}
	})
	return host
}

func TestNewHostHasNoWorkerSideEffectsBeforeStart(t *testing.T) {
	outbox := &countingOutbox{}
	lifecycle := &memoryLifecycleCleanupStore{}
	host := newLifecycleTestHost(t, outbox, lifecycle, nil, nil)

	if state := host.State(); state != LifecycleStateCreated {
		t.Fatalf("constructor state = %q, want created", state)
	}
	if host.workers != nil || outbox.readCount() != 0 || len(lifecycle.snapshot()) != 0 {
		t.Fatalf("constructor started work: workers=%v outboxReads=%d cleanupCalls=%d",
			host.workers != nil, outbox.readCount(), len(lifecycle.snapshot()))
	}
}

func TestStartReturnsAfterInitialAccountScopeBootstrap(t *testing.T) {
	publication := &synchronizedPublicationController{}
	host := newLifecycleTestHost(t, nil, nil, publication, nil)
	scope := contracts.OperationScope{AccountID: "account-1"}
	if err := host.Start(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	if host.State() != LifecycleStateRunning || !host.bootstrapped || host.bootstrapScope != scope {
		t.Fatalf("state=%q bootstrapped=%v scope=%#v", host.State(), host.bootstrapped, host.bootstrapScope)
	}
	values := publication.snapshot()
	if len(values) == 0 || !values[len(values)-1].enabled || values[len(values)-1].scope != scope {
		t.Fatalf("publication transitions = %#v, want initial scope published", values)
	}
}

func TestUnexpectedWorkerExitFailsClosedAndRejectsCommands(t *testing.T) {
	publication := &synchronizedPublicationController{}
	host := newLifecycleTestHost(t, nil, nil, publication, nil)
	group := newWorkerGroup(context.Background(), host.handleUnexpectedWorkerExit)
	host.lifecycleMu.Lock()
	host.workers = group
	host.lifecycleState = LifecycleStateRunning
	host.lifecycleMu.Unlock()
	host.bootstrapMu.Lock()
	host.bootstrapScope = contracts.OperationScope{AccountID: "account-1"}
	host.bootstrapMu.Unlock()
	host.publicationScopeMu.Lock()
	host.publicationScope = contracts.OperationScope{AccountID: "account-1"}
	host.publicationScopeMu.Unlock()
	if err := group.Go("critical-test", func(context.Context) {}); err != nil {
		t.Fatal(err)
	}
	group.Seal()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		health := host.Health()
		values := publication.snapshot()
		if health.Lifecycle == LifecycleStateFailed && reflect.DeepEqual(health.UnexpectedExits, []string{"critical-test"}) &&
			len(health.Workers) == 1 && health.Workers[0].Status == WorkerStatusFailed &&
			health.Workers[0].FailureCode == "unexpected_exit" && !health.Workers[0].LastFailureAt.IsZero() &&
			len(values) != 0 && !values[len(values)-1].enabled && values[len(values)-1].scope.AccountID == "account-1" {
			result := host.CatalogCommands().RefreshCatalog(context.Background(), contracts.Mutation{})
			if result.Outcome != contracts.CommandRejected || result.Failure == nil || result.Failure.Code != contracts.ErrorCodeUnavailable {
				t.Fatalf("command after worker exit result = %#v", result)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("health=%#v publication=%#v", host.Health(), publication.snapshot())
}

func TestStartRegistersEveryConfiguredWorkerAndCloseIsIdempotent(t *testing.T) {
	runtime := &closableActivationGateDelegate{}
	host := newLifecycleTestHost(t, nil, nil, nil, runtime)
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	wantWorkers := []string{
		"catalog-refresh",
		"lifecycle-cleanup",
		"operation-recovery",
		"outbox",
		"runtime-convergence",
		"runtime-recovery",
		"runtime-route-watch",
	}
	if got := host.workers.names(); !reflect.DeepEqual(got, wantWorkers) {
		t.Fatalf("registered workers = %#v, want %#v", got, wantWorkers)
	}
	if state := host.State(); state != LifecycleStateRunning {
		t.Fatalf("started state = %q, want running", state)
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if state := host.State(); state != LifecycleStateStopped || runtime.closes() != 1 {
		t.Fatalf("closed state=%q runtime closes=%d", state, runtime.closes())
	}
}

func TestCloseFailClosesPublicationAndRejectsPublicCommands(t *testing.T) {
	publication := &recordingPublicationController{}
	host := newLifecycleTestHost(t, nil, nil, publication, &closableActivationGateDelegate{})
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if len(publication.values) == 0 || publication.values[len(publication.values)-1] {
		t.Fatalf("publication transitions = %#v, want final false", publication.values)
	}
	if result := host.CatalogCommands().RefreshCatalog(context.Background(), contracts.Mutation{}); result.Outcome != contracts.CommandRejected {
		t.Fatalf("refresh after close result = %#v", result)
	}
	if result := host.InstallationCommands().Install(context.Background(), contracts.ConnectorMutation{}); result.Outcome != contracts.CommandRejected {
		t.Fatalf("install after close result = %#v", result)
	}
	if result := host.RuntimeCommands().RestartRuntime(context.Background(), contracts.ConnectorMutation{}); result.Outcome != contracts.CommandRejected {
		t.Fatalf("runtime restart after close result = %#v", result)
	}
	secret := []byte("must-clear")
	if result := host.AuthorizationCommands().BeginAuthorization(context.Background(), contracts.ConnectorMutation{}, secret); result.Outcome != contracts.CommandRejected {
		t.Fatalf("authorization after close result = %#v", result)
	}
	for _, value := range secret {
		if value != 0 {
			t.Fatalf("authorization secret was not cleared: %v", secret)
		}
	}
}

func TestStartFailureRollsBackWithoutStartingWorkers(t *testing.T) {
	outbox := &countingOutbox{}
	lifecycle := &memoryLifecycleCleanupStore{}
	publication := &failingPublicationController{err: errors.New("publication unavailable")}
	host := newLifecycleTestHost(t, outbox, lifecycle, publication, nil)

	err := host.Start(context.Background(), contracts.OperationScope{})
	if err == nil || !errors.Is(err, publication.err) {
		t.Fatalf("Start() error = %v, want publication failure", err)
	}
	if state := host.State(); state != LifecycleStateFailed {
		t.Fatalf("failed start state = %q, want failed", state)
	}
	if host.workers != nil || outbox.readCount() != 0 || len(lifecycle.snapshot()) != 0 {
		t.Fatalf("failed start leaked work: workers=%v outboxReads=%d cleanupCalls=%d",
			host.workers != nil, outbox.readCount(), len(lifecycle.snapshot()))
	}
}

func TestCloseDeadlineIsHonoredDuringBlockingStartPublication(t *testing.T) {
	publication := &blockingPublicationController{entered: make(chan struct{}), release: make(chan struct{})}
	host := newLifecycleTestHost(t, nil, nil, publication, nil)
	startResult := make(chan error, 1)
	go func() {
		startResult <- host.Start(context.Background(), contracts.OperationScope{AccountID: "account-1"})
	}()
	select {
	case <-publication.entered:
	case <-time.After(time.Second):
		t.Fatal("Start did not enter capability publication")
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := host.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close during blocked Start error = %v, want deadline exceeded", err)
	}
	if state := host.State(); state != LifecycleStateStopping {
		t.Fatalf("state during blocked Start close = %q, want stopping", state)
	}

	close(publication.release)
	if err := <-startResult; err == nil {
		t.Fatal("Start succeeded after Close moved lifecycle to stopping")
	}
	finalCtx, finalCancel := context.WithTimeout(context.Background(), time.Second)
	defer finalCancel()
	if err := host.Close(finalCtx); err != nil {
		t.Fatalf("finish Close after publication release: %v", err)
	}
	for _, transition := range publication.snapshot() {
		if transition.enabled {
			t.Fatalf("stale Start reopened publication: %#v", publication.snapshot())
		}
	}
}

func TestCanceledScopeTransitionDuringStartRollsBackWithoutPanic(t *testing.T) {
	host := newLifecycleTestHost(t, nil, nil, nil, nil)
	<-host.scopeTransition
	ctx, cancel := context.WithCancel(context.Background())
	startResult := make(chan error, 1)
	go func() { startResult <- host.Start(ctx, contracts.OperationScope{}) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		host.lifecycleMu.Lock()
		workers := host.workers
		host.lifecycleMu.Unlock()
		if workers != nil && len(workers.names()) >= 7 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-startResult; err == nil {
		t.Fatal("Start succeeded after its scope-transition context was canceled")
	}
	host.releaseScopeTransition()
	if state := host.State(); state != LifecycleStateFailed {
		t.Fatalf("canceled Start state = %q, want failed", state)
	}
}

func TestStartParentCancellationIsAnUnexpectedWorkerFailure(t *testing.T) {
	publication := &synchronizedPublicationController{}
	host := newLifecycleTestHost(t, nil, nil, publication, nil)
	startContext, cancelStart := context.WithCancel(context.Background())
	if err := host.Start(startContext, contracts.OperationScope{AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	cancelStart()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		values := publication.snapshot()
		if host.State() == LifecycleStateFailed && len(values) != 0 && !values[len(values)-1].enabled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("parent cancellation did not fail close: health=%#v publication=%#v", host.Health(), publication.snapshot())
}

func TestCloseDuringScopeActivationCannotReopenPublication(t *testing.T) {
	publication := &armedPublicationController{entered: make(chan struct{}), release: make(chan struct{})}
	host := newLifecycleTestHost(t, nil, nil, publication, nil)
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	publication.arm()
	activateResult := make(chan error, 1)
	go func() {
		activateResult <- host.ActivateScope(context.Background(), contracts.OperationScope{AccountID: "account-1"})
	}()
	select {
	case <-publication.entered:
	case <-time.After(time.Second):
		t.Fatal("scope activation did not enter enabled publication")
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelClose()
	if err := host.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close during scope activation error = %v, want deadline exceeded", err)
	}
	close(publication.release)
	if err := <-activateResult; !errors.Is(err, errHostNotRunning) {
		t.Fatalf("scope activation after Close error = %v, want host-not-running", err)
	}
	finalCtx, finalCancel := context.WithTimeout(context.Background(), time.Second)
	defer finalCancel()
	if err := host.Close(finalCtx); err != nil {
		t.Fatal(err)
	}
	values := publication.snapshot()
	if len(values) == 0 || values[len(values)-1].enabled {
		t.Fatalf("Close allowed stale publication reopen: %#v", values)
	}
}

func TestConcurrentScopeActivationsCommitInSerializedOrder(t *testing.T) {
	publication := &armedPublicationController{entered: make(chan struct{}), release: make(chan struct{})}
	host := newLifecycleTestHost(t, nil, nil, publication, nil)
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	publication.arm()
	firstScope := contracts.OperationScope{AccountID: "account-1"}
	secondScope := contracts.OperationScope{AccountID: "account-2"}
	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)
	go func() { firstResult <- host.ActivateScope(context.Background(), firstScope) }()
	select {
	case <-publication.entered:
	case <-time.After(time.Second):
		t.Fatal("first activation did not reach publication")
	}
	go func() { secondResult <- host.ActivateScope(context.Background(), secondScope) }()
	close(publication.release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first ActivateScope: %v", err)
	}
	if err := <-secondResult; err != nil {
		t.Fatalf("second ActivateScope: %v", err)
	}
	host.bootstrapMu.Lock()
	gotScope := host.bootstrapScope
	host.bootstrapMu.Unlock()
	values := publication.snapshot()
	if gotScope != secondScope || len(values) == 0 || !values[len(values)-1].enabled || values[len(values)-1].scope != secondScope {
		t.Fatalf("serialized activation scope=%#v publication=%#v", gotScope, values)
	}
}

func TestCloseHonorsCallerCancellationWhileWaitingForRegisteredWorker(t *testing.T) {
	outbox := &blockingOutbox{entered: make(chan struct{}), release: make(chan struct{})}
	runtime := &closableActivationGateDelegate{}
	host := newLifecycleTestHost(t, outbox, nil, nil, runtime)
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-outbox.entered:
	case <-time.After(time.Second):
		t.Fatal("outbox worker did not start")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := host.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context cancellation", err)
	}
	if state := host.State(); state != LifecycleStateStopping {
		t.Fatalf("state while worker is blocked = %q, want stopping", state)
	}
	if runtime.closes() != 0 {
		t.Fatalf("runtime closed %d times before registered worker exited", runtime.closes())
	}

	close(outbox.release)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("Close() after worker release: %v", err)
	}
	if state := host.State(); state != LifecycleStateStopped {
		t.Fatalf("final state = %q, want stopped", state)
	}
	if runtime.closes() != 1 {
		t.Fatalf("runtime close calls = %d, want 1 after workers exited", runtime.closes())
	}
}
