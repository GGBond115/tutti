package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

type lifecycleSchedulerExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	ignore  bool
}

func (executor *lifecycleSchedulerExecutor) ExecuteOperation(ctx context.Context, _ string) error {
	executor.once.Do(func() { close(executor.started) })
	if executor.ignore {
		<-executor.release
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func newStartupCleanupTestHost(t *testing.T, executor OperationExecutor, timeout time.Duration) (*Host, context.CancelFunc) {
	t.Helper()
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	scheduler := NewOperationScheduler(nil)
	if err := scheduler.Bind(executor); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(lifecycleCtx); err != nil {
		t.Fatal(err)
	}
	delegate := &activationGateDelegate{}
	host := &Host{
		scheduler:              scheduler,
		activationGate:         newActivationGateHost(delegate),
		implementationCommands: delegate,
		scopeTransition:        make(chan struct{}, 1),
		shutdownTimeout:        timeout,
	}
	host.scopeTransition <- struct{}{}
	return host, lifecycleCancel
}

func TestFailedStartCleanupWaitsForCooperativeScheduledOperation(t *testing.T) {
	executor := &lifecycleSchedulerExecutor{started: make(chan struct{}), release: make(chan struct{})}
	host, cancelLifecycle := newStartupCleanupTestHost(t, executor, time.Second)
	workers := newWorkerGroup(context.Background(), nil)
	if err := host.scheduler.Schedule(context.Background(), "cooperative-operation"); err != nil {
		t.Fatal(err)
	}
	<-executor.started
	cancelLifecycle()
	if err := host.cleanupFailedStart(workers); err != nil {
		t.Fatalf("cleanupFailedStart() error = %v", err)
	}
}

func TestFailedStartCleanupDeadlineBoundsContextIgnoringScheduledOperation(t *testing.T) {
	executor := &lifecycleSchedulerExecutor{
		started: make(chan struct{}), release: make(chan struct{}), ignore: true,
	}
	host, cancelLifecycle := newStartupCleanupTestHost(t, executor, 20*time.Millisecond)
	workers := newWorkerGroup(context.Background(), nil)
	if err := host.scheduler.Schedule(context.Background(), "context-ignoring-operation"); err != nil {
		t.Fatal(err)
	}
	<-executor.started
	cancelLifecycle()
	startedAt := time.Now()
	err := host.cleanupFailedStart(workers)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanupFailedStart() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 250*time.Millisecond {
		t.Fatalf("context-ignoring cleanup took %s", elapsed)
	}
	close(executor.release)
}

func TestRegisteredWorkersDoNotRunBeforeBootstrapCompletes(t *testing.T) {
	outbox := &countingOutbox{}
	lifecycle := &memoryLifecycleCleanupStore{}
	host := newLifecycleTestHost(t, outbox, lifecycle, nil, nil)
	<-host.scopeTransition
	startCtx, cancelStart := context.WithCancel(context.Background())
	startResult := make(chan error, 1)
	go func() { startResult <- host.Start(startCtx, contracts.OperationScope{}) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		host.lifecycleMu.Lock()
		workers := host.workers
		host.lifecycleMu.Unlock()
		if workers != nil && len(workers.names()) == 7 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if outbox.readCount() != 0 || len(lifecycle.snapshot()) != 0 {
		t.Fatalf("workers ran before bootstrap: outboxReads=%d cleanupCalls=%d",
			outbox.readCount(), len(lifecycle.snapshot()))
	}
	cancelStart()
	host.releaseScopeTransition()
	if err := <-startResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() error = %v, want cancellation", err)
	}
}

type closePublicationController struct {
	mu         sync.Mutex
	armed      bool
	falseCalls int
	called     chan struct{}
	once       sync.Once
}

func (controller *closePublicationController) arm() {
	controller.mu.Lock()
	controller.armed = true
	controller.mu.Unlock()
}

func (controller *closePublicationController) ApplyCapabilityPublication(
	_ context.Context,
	_ contracts.OperationScope,
	enabled bool,
) error {
	controller.mu.Lock()
	if controller.armed && !enabled {
		controller.falseCalls++
		controller.once.Do(func() { close(controller.called) })
	}
	controller.mu.Unlock()
	return nil
}

func TestClosePublishesImmediateFalseBeforeStuckCommandDrains(t *testing.T) {
	publication := &closePublicationController{called: make(chan struct{})}
	host := newLifecycleTestHost(t, nil, nil, publication, nil)
	if err := host.Start(context.Background(), contracts.OperationScope{AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	publication.arm()
	if !host.commandAdmission.begin() {
		t.Fatal("command admission was not open")
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- host.Close(context.Background()) }()
	select {
	case <-publication.called:
	case <-time.After(time.Second):
		t.Fatal("Close did not publish false while command drain was blocked")
	}
	host.commandAdmission.end()
	if err := <-closeResult; err != nil {
		t.Fatal(err)
	}
}

func TestCloseCancelsLifecycleDerivedCommandContext(t *testing.T) {
	host := newLifecycleTestHost(t, nil, nil, nil, nil)
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	commandCtx, cancelCommand := host.commandContext(context.Background())
	defer cancelCommand()
	closeResult := make(chan error, 1)
	go func() { closeResult <- host.Close(context.Background()) }()
	select {
	case <-commandCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel lifecycle-derived command context")
	}
	if err := <-closeResult; err != nil {
		t.Fatal(err)
	}
}

type completionOrderedPublication struct {
	mu         sync.Mutex
	armed      bool
	entered    chan struct{}
	release    chan struct{}
	completion []bool
}

func (controller *completionOrderedPublication) arm() {
	controller.mu.Lock()
	controller.armed = true
	controller.mu.Unlock()
}

func (controller *completionOrderedPublication) ApplyCapabilityPublication(
	_ context.Context,
	_ contracts.OperationScope,
	enabled bool,
) error {
	controller.mu.Lock()
	block := controller.armed && enabled
	if block {
		controller.armed = false
	}
	controller.mu.Unlock()
	if block {
		close(controller.entered)
		<-controller.release
	}
	controller.mu.Lock()
	controller.completion = append(controller.completion, enabled)
	controller.mu.Unlock()
	return nil
}

func (controller *completionOrderedPublication) completed() []bool {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return append([]bool(nil), controller.completion...)
}

func TestCloseFinalPublicationCompletesAfterBlockedOldEnable(t *testing.T) {
	publication := &completionOrderedPublication{entered: make(chan struct{}), release: make(chan struct{})}
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
		t.Fatal("scope activation did not block in enabled publication")
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- host.Close(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for host.State() != LifecycleStateStopping && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if host.State() != LifecycleStateStopping {
		t.Fatal("Close did not enter stopping before blocked enable was released")
	}
	close(publication.release)
	if err := <-activateResult; !errors.Is(err, errHostNotRunning) {
		t.Fatalf("ActivateScope() error = %v, want host-not-running", err)
	}
	if err := <-closeResult; err != nil {
		t.Fatal(err)
	}
	completed := publication.completed()
	if len(completed) < 2 || completed[len(completed)-1] {
		t.Fatalf("publication completion order = %v, want final false", completed)
	}
}

func TestCloseBoundsStuckCommandAndScopeTransition(t *testing.T) {
	host := newLifecycleTestHost(t, nil, nil, nil, nil)
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	host.shutdownTimeout = 20 * time.Millisecond
	if !host.commandAdmission.begin() {
		t.Fatal("command admission was not open")
	}
	<-host.scopeTransition
	startedAt := time.Now()
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := host.Close(closeCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want bounded phase deadline", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 300*time.Millisecond {
		t.Fatalf("Close with stuck command and scope transition took %s", elapsed)
	}
	if state := host.State(); state != LifecycleStateStopped {
		t.Fatalf("state = %q, want stopped", state)
	}
	host.lifecycleMu.Lock()
	host.closeResult = nil
	host.lifecycleMu.Unlock()
	host.commandAdmission.end()
	host.releaseScopeTransition()
}

type contextIgnoringCloseRuntime struct {
	activationGateDelegate
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (runtime *contextIgnoringCloseRuntime) Close(context.Context) error {
	runtime.once.Do(func() { close(runtime.entered) })
	<-runtime.release
	return nil
}

func TestCloseDeadlineBoundsContextIgnoringRuntimeClose(t *testing.T) {
	runtime := &contextIgnoringCloseRuntime{entered: make(chan struct{}), release: make(chan struct{})}
	host := newLifecycleTestHost(t, nil, nil, nil, runtime)
	if err := host.Start(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	host.shutdownTimeout = 20 * time.Millisecond
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := host.Close(closeCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want runtime close deadline", err)
	}
	select {
	case <-runtime.entered:
	default:
		t.Fatal("runtime Close was not invoked")
	}
	close(runtime.release)
	host.lifecycleMu.Lock()
	host.closeResult = nil
	host.lifecycleMu.Unlock()
}
