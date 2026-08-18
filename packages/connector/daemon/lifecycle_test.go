package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	market "github.com/tutti-os/tutti/packages/connector/host"
	marketdata "github.com/tutti-os/tutti/packages/connector/store-sqlite"
)

type countingOutbox struct {
	mu    sync.Mutex
	reads int
}

func (outbox *countingOutbox) PendingChangedEvents(context.Context, int) ([]market.ChangedEventRecord, error) {
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

func (controller *failingPublicationController) ApplyCapabilityPublication(context.Context, market.OperationScope, bool) error {
	controller.calls++
	return controller.err
}

type blockingOutbox struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (outbox *blockingOutbox) PendingChangedEvents(context.Context, int) ([]market.ChangedEventRecord, error) {
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

func (delegate *closableActivationGateDelegate) Close() error {
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
	outbox market.ChangedEventOutbox,
	lifecycle market.LifecycleCleanupStore,
	publication CapabilityPublicationController,
	runtime market.ImplementationHost,
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
		ReleaseInstallations:   runtime.(market.ReleaseInstallationManager),
		ImplementationHost:     runtime,
		Authorization:          unavailableAuthorization{},
		Compatibility:          rejectingCompatibility{},
		ImplementationRegistry: market.NewImplementationRegistry(nil),
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

func TestStartRegistersEveryConfiguredWorkerAndCloseIsIdempotent(t *testing.T) {
	runtime := &closableActivationGateDelegate{}
	host := newLifecycleTestHost(t, nil, nil, nil, runtime)
	if err := host.Start(context.Background()); err != nil {
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

func TestStartFailureRollsBackWithoutStartingWorkers(t *testing.T) {
	outbox := &countingOutbox{}
	lifecycle := &memoryLifecycleCleanupStore{}
	publication := &failingPublicationController{err: errors.New("publication unavailable")}
	host := newLifecycleTestHost(t, outbox, lifecycle, publication, nil)

	err := host.Start(context.Background())
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

func TestCloseHonorsCallerCancellationWhileWaitingForRegisteredWorker(t *testing.T) {
	outbox := &blockingOutbox{entered: make(chan struct{}), release: make(chan struct{})}
	runtime := &closableActivationGateDelegate{}
	host := newLifecycleTestHost(t, outbox, nil, nil, runtime)
	if err := host.Start(context.Background()); err != nil {
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
