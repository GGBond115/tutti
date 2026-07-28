package tuttimodeexecution_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	tuttimodeexecutionservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution"
	tuttimodeexecutionconformance "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution/conformance"
)

type recordingMainWakeTarget struct {
	mu                      sync.Mutex
	busy                    map[string]bool
	hang                    map[string]bool
	observationFailures     map[string]bool
	advanceDuringSend       map[string]func()
	deliveries              []tuttimodeexecutionconformance.WakeDelivery
	canonicalByClientSubmit map[string]string
	failBeforeCanonical     bool
	failAmbiguousBefore     bool
	failAfterCanonical      bool
	failCanonicalLookup     bool
	cancelDuringSend        context.CancelFunc
	cancelDeliveryOutcome   string
}

func newRecordingMainWakeTarget() *recordingMainWakeTarget {
	return &recordingMainWakeTarget{
		busy:                    make(map[string]bool),
		hang:                    make(map[string]bool),
		observationFailures:     make(map[string]bool),
		advanceDuringSend:       make(map[string]func()),
		canonicalByClientSubmit: make(map[string]string),
	}
}

type injectableWakeStore struct {
	tuttimodeexecutionservice.WakeStore
	mu            sync.Mutex
	claimFailures map[string]bool
}

func newInjectableWakeStore(store tuttimodeexecutionservice.WakeStore) *injectableWakeStore {
	return &injectableWakeStore{
		WakeStore:     store,
		claimFailures: make(map[string]bool),
	}
}

func (store *injectableWakeStore) ClaimTuttiModeExecutionWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
	now time.Time,
	leaseExpiresAt time.Time,
) (bool, error) {
	store.mu.Lock()
	fail := store.claimFailures[wakeID]
	if fail {
		delete(store.claimFailures, wakeID)
	}
	store.mu.Unlock()
	if fail {
		return false, errors.New("injected wake claim failure")
	}
	return store.WakeStore.ClaimTuttiModeExecutionWake(
		ctx, workspaceID, wakeID, leaseOwner, now, leaseExpiresAt,
	)
}

func wakeTargetKey(workspaceID string, sessionID string) string {
	return workspaceID + "\x00" + sessionID
}

func (target *recordingMainWakeTarget) ObserveSourceSession(
	_ context.Context,
	workspaceID string,
	sessionID string,
) (tuttimodeexecutionservice.SourceSessionObservation, error) {
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.observationFailures[wakeTargetKey(workspaceID, sessionID)] {
		return tuttimodeexecutionservice.SourceSessionObservation{},
			errors.New("injected permanent source observation failure")
	}
	return tuttimodeexecutionservice.SourceSessionObservation{
		Exists: true,
		Busy:   target.busy[wakeTargetKey(workspaceID, sessionID)],
	}, nil
}

func (target *recordingMainWakeTarget) SendMainWake(
	ctx context.Context,
	workspaceID string,
	sessionID string,
	clientSubmitID string,
	prompt string,
) (tuttimodeexecutionservice.MainWakeDelivery, error) {
	deadline, hadDeadline := ctx.Deadline()
	deadlineBudget := time.Duration(0)
	if hadDeadline {
		deadlineBudget = time.Until(deadline)
	}
	target.mu.Lock()
	target.deliveries = append(target.deliveries, tuttimodeexecutionconformance.WakeDelivery{
		TargetSessionID: sessionID, ClientSubmitID: clientSubmitID, Prompt: prompt,
		HadDeadline: hadDeadline, DeadlineBudget: deadlineBudget,
	})
	key := wakeTargetKey(workspaceID, sessionID)
	cancelCaller := target.cancelDuringSend
	cancelOutcome := target.cancelDeliveryOutcome
	target.cancelDuringSend = nil
	target.cancelDeliveryOutcome = ""
	if cancelCaller != nil && cancelOutcome == "before-canonical-error" {
		target.mu.Unlock()
		cancelCaller()
		return tuttimodeexecutionservice.MainWakeDelivery{},
			errors.New("injected caller cancellation before canonical delivery")
	}
	if target.hang[key] {
		delete(target.hang, key)
		target.mu.Unlock()
		if !hadDeadline {
			return tuttimodeexecutionservice.MainWakeDelivery{},
				errors.New("SendMainWake was not bounded")
		}
		<-ctx.Done()
		return tuttimodeexecutionservice.MainWakeDelivery{}, ctx.Err()
	}
	if target.failBeforeCanonical {
		target.failBeforeCanonical = false
		target.mu.Unlock()
		return tuttimodeexecutionservice.MainWakeDelivery{}, errors.New("injected definite pre-canonical failure")
	}
	if target.failAmbiguousBefore {
		target.failAmbiguousBefore = false
		target.mu.Unlock()
		return tuttimodeexecutionservice.MainWakeDelivery{}, errors.New("injected ambiguous pre-canonical failure")
	}
	turnID := target.canonicalByClientSubmit[clientSubmitID]
	if turnID == "" {
		turnID = fmt.Sprintf("wake-turn-%d", len(target.canonicalByClientSubmit)+1)
		target.canonicalByClientSubmit[clientSubmitID] = turnID
	}
	if target.failAfterCanonical {
		target.failAfterCanonical = false
		target.mu.Unlock()
		return tuttimodeexecutionservice.MainWakeDelivery{}, errors.New("injected response loss")
	}
	advance := target.advanceDuringSend[key]
	delete(target.advanceDuringSend, key)
	target.mu.Unlock()
	if cancelCaller != nil {
		cancelCaller()
		if cancelOutcome == "after-canonical-error" {
			return tuttimodeexecutionservice.MainWakeDelivery{},
				errors.New("injected caller cancellation after canonical delivery")
		}
	}
	if advance != nil {
		advance()
	}
	return tuttimodeexecutionservice.MainWakeDelivery{
		CanonicalSessionID: sessionID,
		CanonicalTurnID:    turnID,
	}, nil
}

func (target *recordingMainWakeTarget) FindMainWakeTurn(
	ctx context.Context,
	_ string,
	_ string,
	clientSubmitID string,
) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.failCanonicalLookup {
		target.failCanonicalLookup = false
		return "", false, errors.New("injected canonical lookup outage")
	}
	turnID := target.canonicalByClientSubmit[clientSubmitID]
	return turnID, turnID != "", nil
}

func (driver *sqliteConformanceDriver) ListWakes(
	ctx context.Context,
	workspaceID string,
	issueID string,
) ([]tuttimodeexecutionconformance.Wake, error) {
	items, err := driver.executions.ListWakes(ctx, workspaceID, issueID)
	if err != nil {
		return nil, err
	}
	result := make([]tuttimodeexecutionconformance.Wake, 0, len(items))
	for _, item := range items {
		result = append(result, tuttimodeexecutionconformance.Wake{
			WakeID: item.ID, ExecutionID: item.ExecutionID,
			CheckpointID: item.CheckpointID, TargetKind: string(item.TargetKind),
			WakeSequence: item.Sequence, ClientSubmitID: item.ClientSubmitID,
			TargetSessionID:    item.TargetSessionID,
			CanonicalSessionID: item.CanonicalSessionID,
			CanonicalTurnID:    item.CanonicalTurnID, Status: string(item.Status),
			AttemptCount: item.AttemptCount, LeaseOwner: item.LeaseOwner,
			DueAt: item.DueAt, LeaseExpiresAt: item.LeaseExpiresAt,
		})
	}
	return result, nil
}

func (driver *sqliteConformanceDriver) ClaimWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
	duration time.Duration,
) (bool, error) {
	return driver.executions.ClaimMainWake(
		ctx, workspaceID, wakeID, leaseOwner, duration,
	)
}

func (driver *sqliteConformanceDriver) DispatchClaimedWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
) error {
	return driver.executions.DispatchClaimedMainWake(
		ctx, workspaceID, wakeID, leaseOwner,
	)
}

func (driver *sqliteConformanceDriver) DispatchClaimedWakeWithCallerCancellation(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
	outcome string,
) error {
	callerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	driver.wakeTarget.mu.Lock()
	driver.wakeTarget.cancelDuringSend = cancel
	driver.wakeTarget.cancelDeliveryOutcome = outcome
	driver.wakeTarget.mu.Unlock()
	return driver.executions.DispatchClaimedMainWake(
		callerCtx, workspaceID, wakeID, leaseOwner,
	)
}

func (driver *sqliteConformanceDriver) RecoverWakes(
	ctx context.Context,
	workspaceID string,
	leaseOwner string,
) error {
	return driver.executions.RecoverMainWakes(ctx, workspaceID, leaseOwner)
}

func (driver *sqliteConformanceDriver) StartupRecoverWakes(
	ctx context.Context,
	workspaceID string,
	leaseOwner string,
) error {
	fresh := tuttimodeexecutionservice.Service{
		Store: driver.store, Wakes: driver.wakeStore, MainWakeTargets: driver.wakeTarget,
		Clock: driver.clock.Now,
	}
	return fresh.StartupRecoverMainWakes(ctx, workspaceID, leaseOwner)
}

func (driver *sqliteConformanceDriver) SetSourceBusy(
	workspaceID string,
	sessionID string,
	busy bool,
) {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.busy[wakeTargetKey(workspaceID, sessionID)] = busy
}

func (driver *sqliteConformanceDriver) FailNextWakeBeforeCanonical() {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.failBeforeCanonical = true
}

func (driver *sqliteConformanceDriver) FailNextWakeAmbiguouslyBeforeCanonical() {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.failAmbiguousBefore = true
}

func (driver *sqliteConformanceDriver) FailNextWakeAfterCanonical() {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.failAfterCanonical = true
}

func (driver *sqliteConformanceDriver) FailNextWakeCanonicalLookup() {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.failCanonicalLookup = true
}

func (driver *sqliteConformanceDriver) SetMainWakeSendTimeout(timeout time.Duration) {
	driver.executions.MainWakeSendTimeout = timeout
}

func (driver *sqliteConformanceDriver) HangWakeUntilContextDone(
	workspaceID string,
	sessionID string,
) {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.hang[wakeTargetKey(workspaceID, sessionID)] = true
}

func (driver *sqliteConformanceDriver) AdvanceClockDuringWake(
	workspaceID string,
	sessionID string,
	duration time.Duration,
) {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.advanceDuringSend[wakeTargetKey(workspaceID, sessionID)] = func() {
		driver.clock.Advance(duration)
	}
}

func (driver *sqliteConformanceDriver) FailWakeObservation(
	workspaceID string,
	sessionID string,
) {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	driver.wakeTarget.observationFailures[wakeTargetKey(workspaceID, sessionID)] = true
}

func (driver *sqliteConformanceDriver) FailWakeClaim(wakeID string) {
	driver.wakeStore.mu.Lock()
	defer driver.wakeStore.mu.Unlock()
	driver.wakeStore.claimFailures[wakeID] = true
}

func (driver *sqliteConformanceDriver) SeedPreparedReviewerWake(
	ctx context.Context,
	workspaceID string,
	issueID string,
) error {
	now := driver.clock.Now().UnixMilli()
	return driver.execWakeFixtureMutation(ctx, `
INSERT INTO workspace_tutti_execution_wakes (
  workspace_id, execution_id, checkpoint_id, wake_id, target_kind,
  wake_sequence, client_submit_id, target_session_id,
  review_agent_target_id, status, due_at_unix_ms,
  created_at_unix_ms, updated_at_unix_ms
)
SELECT e.workspace_id, e.execution_id, c.checkpoint_id,
       c.checkpoint_id || ':wake:reviewer:2', 'reviewer', 2,
       'tutti-execution-review:' || c.checkpoint_id || ':wake:reviewer:2',
       '', 'review-agent-target', 'prepared', ?, ?, ?
FROM workspace_tutti_executions e
JOIN workspace_tutti_execution_checkpoints c
  ON c.workspace_id = e.workspace_id AND c.execution_id = e.execution_id
WHERE e.workspace_id = ? AND e.issue_id = ? AND c.status = 'active'
`, now, now, now, workspaceID, issueID)
}

func (driver *sqliteConformanceDriver) CorruptWakeIdentity(
	ctx context.Context,
	workspaceID string,
	issueID string,
	field string,
	value string,
) error {
	switch field {
	case "wake_id", "client_submit_id", "target_kind", "wake_sequence", "target_session_id":
	default:
		return fmt.Errorf("unsupported wake identity field %q", field)
	}
	return driver.execWakeFixtureMutation(ctx, fmt.Sprintf(`
UPDATE workspace_tutti_execution_wakes
SET %s = ?
WHERE workspace_id = ? AND execution_id = (
  SELECT execution_id FROM workspace_tutti_executions
  WHERE workspace_id = ? AND issue_id = ?
)
`, field), value, workspaceID, workspaceID, issueID)
}

func (driver *sqliteConformanceDriver) SettleWakeTurn(
	ctx context.Context,
	workspaceID string,
	sessionID string,
	turnID string,
) error {
	return driver.executions.ObserveMainWakeTurnSettled(
		ctx, workspaceID, sessionID, turnID,
	)
}

func (driver *sqliteConformanceDriver) SetExecutionStatus(
	ctx context.Context,
	workspaceID string,
	issueID string,
	status string,
) error {
	return driver.execWakeFixtureMutation(ctx, `
UPDATE workspace_tutti_executions SET status = ?
WHERE workspace_id = ? AND issue_id = ?
`, status, workspaceID, issueID)
}

func (driver *sqliteConformanceDriver) CorruptWakeTargetSession(
	ctx context.Context,
	workspaceID string,
	issueID string,
	sessionID string,
) error {
	return driver.execWakeFixtureMutation(ctx, `
UPDATE workspace_tutti_execution_wakes
SET target_session_id = ?
WHERE workspace_id = ? AND execution_id = (
  SELECT execution_id FROM workspace_tutti_executions
  WHERE workspace_id = ? AND issue_id = ?
)
`, sessionID, workspaceID, workspaceID, issueID)
}

func (driver *sqliteConformanceDriver) execWakeFixtureMutation(
	ctx context.Context,
	query string,
	args ...any,
) error {
	db, err := sql.Open("sqlite", "file:"+driver.dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, query, args...)
	return err
}

func (driver *sqliteConformanceDriver) CurrentTime() time.Time {
	return driver.clock.Now()
}

func (driver *sqliteConformanceDriver) WakeDeliveryCallCount() int {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	return len(driver.wakeTarget.deliveries)
}

func (driver *sqliteConformanceDriver) WakeDeliveries() []tuttimodeexecutionconformance.WakeDelivery {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	return append([]tuttimodeexecutionconformance.WakeDelivery(nil), driver.wakeTarget.deliveries...)
}

func (driver *sqliteConformanceDriver) WakeDeliveryClientSubmitIDs() []string {
	deliveries := driver.WakeDeliveries()
	ids := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		ids = append(ids, delivery.ClientSubmitID)
	}
	return ids
}

func (driver *sqliteConformanceDriver) WakeDeliveryCanonicalTurnCount() int {
	driver.wakeTarget.mu.Lock()
	defer driver.wakeTarget.mu.Unlock()
	return len(driver.wakeTarget.canonicalByClientSubmit)
}

var _ tuttimodeexecutionservice.MainWakeTarget = (*recordingMainWakeTarget)(nil)
