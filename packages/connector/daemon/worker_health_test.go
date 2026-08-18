package daemon

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func TestWorkerHealthReporterTransitionsAndCopiesFailureBudget(t *testing.T) {
	group := &workerGroup{health: map[string]WorkerHealth{
		"worker": {Name: "worker", Status: WorkerStatusRunning},
	}}
	reporter := workerHealthReporter{group: group, name: "worker"}
	failureAt := time.Date(2026, 8, 18, 1, 2, 3, 0, time.UTC)
	backoffUntil := failureAt.Add(time.Minute)
	budget := uint32(2)
	reporter.Failure(failureAt, "stable_failure", backoffUntil, &budget)

	first := group.healthSnapshot()[0]
	if first.ConsecutiveFailures != 1 || first.FailureBudget == nil || *first.FailureBudget != 2 ||
		first.Exhausted || first.FailureCode != "stable_failure" || !first.BackoffUntil.Equal(backoffUntil) {
		t.Fatalf("failure health = %#v", first)
	}
	*first.FailureBudget = 99
	if got := group.healthSnapshot()[0]; got.FailureBudget == nil || *got.FailureBudget != 2 {
		t.Fatalf("health snapshot leaked mutable budget pointer: %#v", got)
	}

	successAt := failureAt.Add(2 * time.Minute)
	reporter.Success(successAt)
	recovered := group.healthSnapshot()[0]
	if !recovered.LastSuccess.Equal(successAt) || recovered.ConsecutiveFailures != 0 ||
		!recovered.BackoffUntil.IsZero() || recovered.Exhausted {
		t.Fatalf("recovered health = %#v", recovered)
	}
	if !recovered.LastFailureAt.Equal(failureAt) || recovered.FailureCode != "stable_failure" {
		t.Fatalf("historical failure was not retained after success: %#v", recovered)
	}

	reporter.Failure(successAt.Add(time.Second), "next_failure", successAt.Add(time.Minute), &budget)
	reporter.Reset()
	reset := group.healthSnapshot()[0]
	if reset.ConsecutiveFailures != 0 || !reset.BackoffUntil.IsZero() || reset.FailureBudget != nil || reset.Exhausted {
		t.Fatalf("reset health = %#v", reset)
	}
	if reset.FailureCode != "next_failure" || !reset.LastFailureAt.Equal(successAt.Add(time.Second)) {
		t.Fatalf("reset discarded historical failure: %#v", reset)
	}
}

func TestWorkerHealthReporterIsRaceSafe(_ *testing.T) {
	group := &workerGroup{health: map[string]WorkerHealth{
		"worker": {Name: "worker", Status: WorkerStatusRunning},
	}}
	reporter := workerHealthReporter{group: group, name: "worker"}
	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			for attempt := 0; attempt < 100; attempt++ {
				now := time.Unix(int64(index*100+attempt), 0)
				switch attempt % 3 {
				case 0:
					reporter.Success(now)
				case 1:
					reporter.Failure(now, "stable_failure", now.Add(time.Second), nil)
				default:
					reporter.Reset()
				}
				_ = group.healthSnapshot()
			}
		}(index)
	}
	wait.Wait()
}

func TestRetryBackoffClampsAndCatalogUsesCurrentBase(t *testing.T) {
	host := &Host{catalogRetryJitter: func(base time.Duration) time.Duration { return base / 2 }}
	base := time.Minute
	firstDelay := host.catalogRetryDelay(base)
	base = nextBoundedRetry(base, 5*time.Minute)
	secondDelay := host.catalogRetryDelay(base)
	if firstDelay != 30*time.Second || secondDelay != time.Minute {
		t.Fatalf("catalog failure delays = %s, %s; want 30s, 1m", firstDelay, secondDelay)
	}

	retry := time.Second
	for index := 0; index < 20; index++ {
		retry = nextBoundedRetry(retry, time.Minute)
		if retry > time.Minute {
			t.Fatalf("retry exceeded one minute at step %d: %s", index, retry)
		}
	}
	if retry != time.Minute {
		t.Fatalf("bounded retry = %s, want 1m", retry)
	}
}

type catalogHealthControl struct {
	application.RecoveryControl
	application.CatalogMaintenance
	refreshes atomic.Int32
}

func (*catalogHealthControl) Snapshot(context.Context) (contracts.Snapshot, error) {
	return contracts.Snapshot{Revision: 1}, nil
}

func (control *catalogHealthControl) RefreshCatalog(context.Context, contracts.Mutation) (contracts.MutationResult, error) {
	attempt := control.refreshes.Add(1)
	return contracts.MutationResult{Operation: contracts.Operation{OperationID: string(rune('0' + attempt))}}, nil
}

func (*catalogHealthControl) GetOperation(_ context.Context, operationID string) (contracts.Operation, error) {
	state := contracts.OperationStateCompleted
	failureCode := ""
	if operationID == "1" {
		state = contracts.OperationStateFailed
		failureCode = string(contracts.ErrorCodeUpstreamUnavailable)
	}
	return contracts.Operation{OperationID: operationID, State: state, FailureCode: failureCode}, nil
}

func TestCatalogRefreshReportsFailureThenSuccessWithCurrentRetryDeadline(t *testing.T) {
	control := &catalogHealthControl{}
	group := &workerGroup{health: map[string]WorkerHealth{
		"catalog-refresh": {Name: "catalog-refresh", Status: WorkerStatusRunning},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, workerHealthReporterContextKey{}, workerHealthReporter{group: group, name: "catalog-refresh"})
	firstFailure := make(chan WorkerHealth, 1)
	successWait := make(chan struct{}, 1)
	waits := atomic.Int32{}
	host := &Host{
		recoveryControl:    control,
		catalogMaintenance: control,
		catalogRetryJitter: func(time.Duration) time.Duration { return 0 },
		catalogRetryWait: func(ctx context.Context, _ time.Duration) bool {
			if waits.Add(1) == 1 {
				firstFailure <- group.healthSnapshot()[0]
				return true
			}
			successWait <- struct{}{}
			<-ctx.Done()
			return false
		},
	}
	done := make(chan struct{})
	go func() {
		host.runCatalogRefreshWorker(ctx)
		close(done)
	}()

	failure := <-firstFailure
	if failure.ConsecutiveFailures != 1 || failure.FailureCode != workerFailureCatalogRefresh {
		cancel()
		<-done
		t.Fatalf("catalog failure health = %#v", failure)
	}
	remaining := time.Until(failure.BackoffUntil)
	if remaining < 29*time.Second || remaining > 31*time.Second {
		cancel()
		<-done
		t.Fatalf("first catalog retry deadline is %s away, want 30s", remaining)
	}
	<-successWait
	recovered := group.healthSnapshot()[0]
	if recovered.LastSuccess.IsZero() || recovered.ConsecutiveFailures != 0 || !recovered.BackoffUntil.IsZero() ||
		recovered.FailureCode != workerFailureCatalogRefresh {
		cancel()
		<-done
		t.Fatalf("catalog recovered health = %#v", recovered)
	}
	cancel()
	<-done
}

type runtimeRecoveryCatalog struct {
	application.CatalogQueries
}

func (*runtimeRecoveryCatalog) GetConnectorForScope(
	context.Context,
	contracts.OperationScope,
	string,
) (contracts.Connector, error) {
	return contracts.Connector{
		Key: "github",
		Installation: contracts.Installation{
			State: contracts.InstallationStateInstalled,
		},
	}, nil
}

type runtimeRecoveryMaintenance struct {
	application.RuntimeMaintenance
	calls atomic.Int32
}

func (maintenance *runtimeRecoveryMaintenance) ReconcileRuntimeDesired(
	context.Context,
	contracts.OperationScope,
	string,
) error {
	if maintenance.calls.Add(1) == 1 {
		return errors.New("secret runtime detail")
	}
	return nil
}

func TestRuntimeRecoveryReportsPendingFailureThenDrainedSuccess(t *testing.T) {
	maintenance := &runtimeRecoveryMaintenance{}
	group := &workerGroup{health: map[string]WorkerHealth{
		"runtime-recovery": {Name: "runtime-recovery", Status: WorkerStatusRunning},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, workerHealthReporterContextKey{}, workerHealthReporter{group: group, name: "runtime-recovery"})
	host := &Host{
		catalogQueries:         &runtimeRecoveryCatalog{},
		runtimeMaintenance:     maintenance,
		runtimeRecoveryWake:    make(chan struct{}, 1),
		runtimeRecoveryPending: map[string]struct{}{"github": {}},
		bootstrapped:           true,
	}
	done := make(chan struct{})
	go func() {
		host.runRuntimeRecoveryWorker(ctx)
		close(done)
	}()
	host.runtimeRecoveryWake <- struct{}{}

	deadline := time.Now().Add(2 * time.Second)
	var failure WorkerHealth
	for time.Now().Before(deadline) {
		failure = group.healthSnapshot()[0]
		if failure.ConsecutiveFailures == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if failure.ConsecutiveFailures != 1 || failure.FailureCode != workerFailureRuntimeRecovery ||
		failure.BackoffUntil.Before(time.Now().Add(time.Second)) {
		cancel()
		<-done
		t.Fatalf("runtime recovery failure health = %#v", failure)
	}
	host.runtimeRecoveryWake <- struct{}{}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		health := group.healthSnapshot()[0]
		if maintenance.calls.Load() == 2 && health.ConsecutiveFailures == 0 && !health.LastSuccess.IsZero() {
			if health.FailureCode == "secret runtime detail" {
				t.Fatal("raw runtime error leaked into worker health")
			}
			cancel()
			<-done
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("runtime recovery did not report drained success: calls=%d health=%#v", maintenance.calls.Load(), group.healthSnapshot()[0])
}

type immediatelyClosingRouteObservation struct {
	calls atomic.Int32
}

func (*immediatelyClosingRouteObservation) Snapshot(context.Context) (contracts.PhysicalRouteSnapshot, error) {
	return contracts.PhysicalRouteSnapshot{}, nil
}

func (observation *immediatelyClosingRouteObservation) Watch(context.Context) (contracts.PhysicalRouteWatch, error) {
	observation.calls.Add(1)
	events := make(chan contracts.PhysicalRouteEvent)
	close(events)
	return contracts.PhysicalRouteWatch{Events: events}, nil
}

func TestImmediatelyClosingRouteWatchMaintainsBackoffDebt(t *testing.T) {
	observation := &immediatelyClosingRouteObservation{}
	group := &workerGroup{health: map[string]WorkerHealth{
		"runtime-route-watch": {Name: "runtime-route-watch", Status: WorkerStatusRunning},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, workerHealthReporterContextKey{}, workerHealthReporter{group: group, name: "runtime-route-watch"})
	host := &Host{physicalRoutes: observation, runtimePhysicalWake: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		host.runPhysicalRouteWatchWorker(ctx)
		close(done)
	}()

	deadline := time.Now().Add(4 * time.Second)
	for observation.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls := observation.calls.Load(); calls != 3 {
		cancel()
		<-done
		t.Fatalf("watch calls = %d, want third attempt after 1s + 2s", calls)
	}
	time.Sleep(1200 * time.Millisecond)
	if calls := observation.calls.Load(); calls != 3 {
		cancel()
		<-done
		t.Fatalf("immediately closing watch reset to a one-second loop: calls=%d", calls)
	}
	health := group.healthSnapshot()[0]
	if health.ConsecutiveFailures != 3 || health.FailureCode != workerFailureRuntimeRouteWatch ||
		health.BackoffUntil.Before(time.Now().Add(2*time.Second)) {
		cancel()
		<-done
		t.Fatalf("route watch health = %#v", health)
	}
	cancel()
	<-done
}

type authorizationEventSourceFunc func(context.Context, string, func()) error

func (run authorizationEventSourceFunc) RunAuthorizationEvents(ctx context.Context, accountID string, notify func()) error {
	return run(ctx, accountID, notify)
}

func TestAuthorizationEventCallbackIsSuccessButQuietClosureIsFailure(t *testing.T) {
	tests := []struct {
		name            string
		source          authorizationEventSourceFunc
		wantSuccess     bool
		wantFailureCode string
	}{
		{
			name: "callback proves success",
			source: func(ctx context.Context, _ string, notify func()) error {
				notify()
				<-ctx.Done()
				return ctx.Err()
			},
			wantSuccess: true,
		},
		{
			name: "quiet closure is failure",
			source: func(context.Context, string, func()) error {
				return errors.New("secret provider detail")
			},
			wantFailureCode: workerFailureAuthorizationEvents,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			group := &workerGroup{health: map[string]WorkerHealth{
				"authorization-events": {Name: "authorization-events", Status: WorkerStatusRunning},
			}}
			ctx, cancel := context.WithCancel(context.Background())
			ctx = context.WithValue(ctx, workerHealthReporterContextKey{}, workerHealthReporter{group: group, name: "authorization-events"})
			host := &Host{
				authorizationEvents:    test.source,
				authorizationScopeWake: make(chan struct{}, 1),
				authorizationSyncWake:  make(chan struct{}, 1),
				bootstrapScope:         contracts.OperationScope{AccountID: "account-1"},
			}
			done := make(chan struct{})
			go func() {
				host.runAuthorizationEventWorker(ctx)
				close(done)
			}()
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				health := group.healthSnapshot()[0]
				if test.wantSuccess && !health.LastSuccess.IsZero() ||
					test.wantFailureCode != "" && health.FailureCode == test.wantFailureCode {
					cancel()
					<-done
					if health.FailureCode == "secret provider detail" {
						t.Fatal("raw provider error leaked into worker health")
					}
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
			<-done
			t.Fatalf("authorization worker health = %#v", group.healthSnapshot()[0])
		})
	}
}
