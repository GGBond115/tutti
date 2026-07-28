package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	agentactivitybiz "github.com/tutti-os/tutti/services/tuttid/biz/agentactivity"
	tuttimodeexecutionservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution"
	workspaceservice "github.com/tutti-os/tutti/services/tuttid/service/workspace"
)

type wrappedMissingWakeSessionHost struct{}

func (wrappedMissingWakeSessionHost) GetSession(
	context.Context,
	agenthost.SessionRef,
) (agenthost.GetSessionResult, error) {
	return agenthost.GetSessionResult{}, fmt.Errorf(
		"load canonical wake target: %w",
		agenthost.ErrSessionNotFound,
	)
}

func (wrappedMissingWakeSessionHost) FindTurnByClientSubmitID(
	context.Context,
	agenthost.SessionRef,
	string,
) (string, bool, error) {
	return "", false, nil
}

func TestTuttiModeMainWakeAdapterTreatsWrappedSessionNotFoundAsAbsent(t *testing.T) {
	observation, err := (tuttiModeMainWakeAgentAdapter{
		Host: wrappedMissingWakeSessionHost{},
	}).ObserveSourceSession(context.Background(), "workspace-1", "session-1")
	if err != nil {
		t.Fatalf("ObserveSourceSession() error = %v", err)
	}
	if observation.Exists || observation.Busy {
		t.Fatalf("ObserveSourceSession() = %+v, want absent and idle", observation)
	}
}

type failingStartupWakeRecoverer struct {
	calls int
}

func (recoverer *failingStartupWakeRecoverer) PrepareStartupMainWakeRecovery(
	context.Context,
	string,
) error {
	recoverer.calls++
	return errors.New("transient canonical session lookup failure")
}

func TestTuttiModeMainWakeStartupRepairIsNonFatalAndDoesNotDispatch(t *testing.T) {
	recoverer := &failingStartupWakeRecoverer{}

	repairTuttiModeMainWakesAtStartup(
		context.Background(),
		recoverer,
		"workspace-1",
	)

	if recoverer.calls != 1 {
		t.Fatalf("PrepareStartupMainWakeRecovery() calls = %d, want 1", recoverer.calls)
	}
}

type recordingMainWakeRecoverer struct {
	workspaceID string
	leaseOwner  string
	prepared    int
}

func (recoverer *recordingMainWakeRecoverer) PrepareStartupMainWakeRecovery(
	context.Context,
	string,
) error {
	recoverer.prepared++
	return nil
}

func (recoverer *recordingMainWakeRecoverer) RecoverMainWakes(
	_ context.Context,
	workspaceID string,
	leaseOwner string,
) error {
	recoverer.workspaceID = workspaceID
	recoverer.leaseOwner = leaseOwner
	return nil
}

func TestTuttiModeRunReconcileAlsoRecoversDurableMainWakes(t *testing.T) {
	wakes := &recordingMainWakeRecoverer{}
	runCalls := 0
	result, err := reconcileTuttiModeRunsAndMainWakes(
		context.Background(),
		"workspace-1",
		"daemon-owner-1",
		func(context.Context, string) (workspaceservice.IssueRunReconcileResult, error) {
			runCalls++
			return workspaceservice.IssueRunReconcileResult{RunningCount: 1}, nil
		},
		wakes,
	)
	if err != nil {
		t.Fatalf("reconcileTuttiModeRunsAndMainWakes() error = %v", err)
	}
	if runCalls != 1 || result.RunningCount != 1 {
		t.Fatalf("run reconciliation calls/result = %d/%+v", runCalls, result)
	}
	if wakes.prepared != 1 ||
		wakes.workspaceID != "workspace-1" || wakes.leaseOwner != "daemon-owner-1" {
		t.Fatalf("wake recovery = %+v, want exact workspace and daemon owner", wakes)
	}
}

func TestMainWakeRecoveryGateDoesNotDispatchBeforeListenerReadiness(t *testing.T) {
	delegate := &recordingMainWakeRecoverer{}
	gate := &tuttiModeMainWakeReadyRecovery{Delegate: delegate}

	if err := gate.RecoverMainWakes(
		context.Background(), "workspace-1", "daemon-owner-1",
	); !errors.Is(err, tuttimodeexecutionservice.ErrMainWakeDeliveryPending) {
		t.Fatalf("RecoverMainWakes(before ready) error=%v, want pending", err)
	}
	if delegate.workspaceID != "" {
		t.Fatalf("delegate dispatched before listener readiness: %+v", delegate)
	}

	gate.MarkReady()
	if err := gate.RecoverMainWakes(
		context.Background(), "workspace-1", "daemon-owner-1",
	); err != nil {
		t.Fatalf("RecoverMainWakes(after ready) error=%v", err)
	}
	if delegate.workspaceID != "workspace-1" || delegate.leaseOwner != "daemon-owner-1" {
		t.Fatalf("ready delegate recovery=%+v", delegate)
	}
}

type transientMainWakeRecoverer struct {
	calls        int
	workspaceIDs []string
	leaseOwners  []string
	completed    chan struct{}
}

func (recoverer *transientMainWakeRecoverer) PrepareStartupMainWakeRecovery(
	context.Context,
	string,
) error {
	return nil
}

func (recoverer *transientMainWakeRecoverer) RecoverMainWakes(
	_ context.Context,
	workspaceID string,
	leaseOwner string,
) error {
	recoverer.calls++
	recoverer.workspaceIDs = append(recoverer.workspaceIDs, workspaceID)
	recoverer.leaseOwners = append(recoverer.leaseOwners, leaseOwner)
	if recoverer.calls == 1 {
		return tuttimodeexecutionservice.ErrMainWakeDeliveryPending
	}
	close(recoverer.completed)
	return nil
}

func TestPendingMainWakeDeliveryKeepsProductionQueueForBoundedRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wakes := &transientMainWakeRecoverer{completed: make(chan struct{})}
	reconcile := func(ctx context.Context, workspaceID string) (workspaceservice.IssueRunReconcileResult, error) {
		return reconcileTuttiModeRunsAndMainWakes(
			ctx,
			workspaceID,
			"daemon-owner-1",
			func(context.Context, string) (workspaceservice.IssueRunReconcileResult, error) {
				return workspaceservice.IssueRunReconcileResult{}, nil
			},
			wakes,
		)
	}
	queue := workspaceservice.NewIssueRunReconcileQueue(workspaceservice.IssueRunReconcileQueueOptions{
		Context: ctx, Delay: time.Millisecond, Interval: time.Millisecond,
		Reconcile: reconcile,
	})

	queue.Enqueue("workspace-1")
	select {
	case <-wakes.completed:
	case <-time.After(time.Second):
		t.Fatal("pending durable wake did not retain the production queue retry")
	}
	if wakes.calls != 2 {
		t.Fatalf("wake recovery calls=%d, want 2", wakes.calls)
	}
	for index := range wakes.workspaceIDs {
		if wakes.workspaceIDs[index] != "workspace-1" ||
			wakes.leaseOwners[index] != "daemon-owner-1" {
			t.Fatalf(
				"wake retry[%d] workspace/owner=%q/%q, want stable identity",
				index, wakes.workspaceIDs[index], wakes.leaseOwners[index],
			)
		}
	}
}

type transientStartupRepairRecoverer struct {
	prepareCalls int
	recoverCalls int
	completed    chan struct{}
}

func (recoverer *transientStartupRepairRecoverer) PrepareStartupMainWakeRecovery(
	context.Context,
	string,
) error {
	recoverer.prepareCalls++
	if recoverer.prepareCalls == 1 {
		return errors.New("transient durable wake repair failure")
	}
	return nil
}

func (recoverer *transientStartupRepairRecoverer) RecoverMainWakes(
	context.Context,
	string,
	string,
) error {
	recoverer.recoverCalls++
	close(recoverer.completed)
	return nil
}

func TestListenerReadyQueueRetriesDurableRepairBeforeDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wakes := &transientStartupRepairRecoverer{completed: make(chan struct{})}
	queue := workspaceservice.NewIssueRunReconcileQueue(workspaceservice.IssueRunReconcileQueueOptions{
		Context: ctx, Delay: time.Millisecond, Interval: time.Millisecond,
		Reconcile: func(ctx context.Context, workspaceID string) (workspaceservice.IssueRunReconcileResult, error) {
			return reconcileTuttiModeRunsAndMainWakes(
				ctx,
				workspaceID,
				"daemon-owner-1",
				func(context.Context, string) (workspaceservice.IssueRunReconcileResult, error) {
					return workspaceservice.IssueRunReconcileResult{}, nil
				},
				wakes,
			)
		},
	})

	queue.Enqueue("workspace-1")
	select {
	case <-wakes.completed:
	case <-time.After(time.Second):
		t.Fatal("listener-ready queue did not retry transient durable repair")
	}
	if wakes.prepareCalls != 2 || wakes.recoverCalls != 1 {
		t.Fatalf(
			"prepare/recover calls=%d/%d, want 2/1",
			wakes.prepareCalls, wakes.recoverCalls,
		)
	}
}

type recordingRootTurnObserver struct {
	turnIDs []string
}

func (observer *recordingRootTurnObserver) ObserveRootTurnSettled(
	_ context.Context,
	_ string,
	_ string,
	turn agentactivitybiz.Turn,
) {
	observer.turnIDs = append(observer.turnIDs, turn.TurnID)
}

func TestRootTurnObserverFanoutPreservesEveryRegisteredConsumer(t *testing.T) {
	runtimeObserver := &recordingRootTurnObserver{}
	wakeObserver := &recordingRootTurnObserver{}
	rootTurnObserverFanout{runtimeObserver, wakeObserver}.ObserveRootTurnSettled(
		context.Background(),
		"workspace-1",
		"session-1",
		agentactivitybiz.Turn{TurnID: "turn-1"},
	)
	if len(runtimeObserver.turnIDs) != 1 || runtimeObserver.turnIDs[0] != "turn-1" {
		t.Fatalf("runtime observer turns = %#v, want turn-1", runtimeObserver.turnIDs)
	}
	if len(wakeObserver.turnIDs) != 1 || wakeObserver.turnIDs[0] != "turn-1" {
		t.Fatalf("wake observer turns = %#v, want turn-1", wakeObserver.turnIDs)
	}
}

type recordingWakeTurnSettler struct {
	workspaceID string
	sessionID   string
	turnID      string
}

func (settler *recordingWakeTurnSettler) ObserveMainWakeTurnSettled(
	_ context.Context,
	workspaceID string,
	sessionID string,
	turnID string,
) error {
	settler.workspaceID = workspaceID
	settler.sessionID = sessionID
	settler.turnID = turnID
	return nil
}

type recordingWorkspaceReconcileQueue struct {
	workspaceIDs []string
}

func (queue *recordingWorkspaceReconcileQueue) Enqueue(workspaceID string) {
	queue.workspaceIDs = append(queue.workspaceIDs, workspaceID)
}

func TestRootTurnSettlementQueuesMainWakeRecoveryInsteadOfSendingInline(t *testing.T) {
	settler := &recordingWakeTurnSettler{}
	queue := &recordingWorkspaceReconcileQueue{}
	(tuttiModeMainWakeTurnObserver{
		Settlements: settler,
		Queue:       queue,
	}).ObserveRootTurnSettled(
		context.Background(),
		"workspace-1",
		"session-1",
		agentactivitybiz.Turn{TurnID: "turn-1", Phase: agentactivitybiz.TurnPhaseSettled},
	)
	if settler.workspaceID != "workspace-1" ||
		settler.sessionID != "session-1" ||
		settler.turnID != "turn-1" {
		t.Fatalf("settled wake identity = %+v", settler)
	}
	if len(queue.workspaceIDs) != 1 || queue.workspaceIDs[0] != "workspace-1" {
		t.Fatalf("queued workspaces = %#v, want workspace-1", queue.workspaceIDs)
	}
}
