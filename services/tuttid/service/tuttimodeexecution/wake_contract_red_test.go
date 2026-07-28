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
	deliveries              []tuttimodeexecutionconformance.WakeDelivery
	canonicalByClientSubmit map[string]string
	failBeforeCanonical     bool
	failAmbiguousBefore     bool
	failAfterCanonical      bool
	failCanonicalLookup     bool
}

func newRecordingMainWakeTarget() *recordingMainWakeTarget {
	return &recordingMainWakeTarget{
		busy:                    make(map[string]bool),
		canonicalByClientSubmit: make(map[string]string),
	}
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
	return tuttimodeexecutionservice.SourceSessionObservation{
		Exists: true,
		Busy:   target.busy[wakeTargetKey(workspaceID, sessionID)],
	}, nil
}

func (target *recordingMainWakeTarget) SendMainWake(
	_ context.Context,
	_ string,
	sessionID string,
	clientSubmitID string,
	prompt string,
) (tuttimodeexecutionservice.MainWakeDelivery, error) {
	target.mu.Lock()
	defer target.mu.Unlock()
	target.deliveries = append(target.deliveries, tuttimodeexecutionconformance.WakeDelivery{
		TargetSessionID: sessionID, ClientSubmitID: clientSubmitID, Prompt: prompt,
	})
	if target.failBeforeCanonical {
		target.failBeforeCanonical = false
		return tuttimodeexecutionservice.MainWakeDelivery{}, errors.New("injected definite pre-canonical failure")
	}
	if target.failAmbiguousBefore {
		target.failAmbiguousBefore = false
		return tuttimodeexecutionservice.MainWakeDelivery{}, errors.New("injected ambiguous pre-canonical failure")
	}
	turnID := target.canonicalByClientSubmit[clientSubmitID]
	if turnID == "" {
		turnID = fmt.Sprintf("wake-turn-%d", len(target.canonicalByClientSubmit)+1)
		target.canonicalByClientSubmit[clientSubmitID] = turnID
	}
	if target.failAfterCanonical {
		target.failAfterCanonical = false
		return tuttimodeexecutionservice.MainWakeDelivery{}, errors.New("injected response loss")
	}
	return tuttimodeexecutionservice.MainWakeDelivery{
		CanonicalSessionID: sessionID,
		CanonicalTurnID:    turnID,
	}, nil
}

func (target *recordingMainWakeTarget) FindMainWakeTurn(
	_ context.Context,
	_ string,
	_ string,
	clientSubmitID string,
) (string, bool, error) {
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
		Store: driver.store, MainWakeTargets: driver.wakeTarget,
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
