package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
)

func prepareTuttiModeMainWakeTx(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	executionID string,
	checkpoint executionbiz.Checkpoint,
	now time.Time,
) error {
	if checkpoint.Status != executionbiz.CheckpointStatusActive {
		return nil
	}
	wakeID, ok := executionbiz.MainWakeID(checkpoint.ID, 1)
	if !ok {
		return executionbiz.ErrWakeIntegrity
	}
	clientSubmitID, ok := executionbiz.MainWakeClientSubmitID(wakeID)
	if !ok {
		return executionbiz.ErrWakeIntegrity
	}
	result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO workspace_tutti_execution_wakes (
  workspace_id, execution_id, checkpoint_id, wake_id, target_kind,
  wake_sequence, client_submit_id, target_session_id, status,
  due_at_unix_ms, created_at_unix_ms, updated_at_unix_ms
)
SELECT e.workspace_id, e.execution_id, ?, ?, 'main', 1, ?,
       e.source_session_id, 'prepared', ?, ?, ?
FROM workspace_tutti_executions e
JOIN workspace_tutti_execution_checkpoints c
  ON c.workspace_id = e.workspace_id AND c.execution_id = e.execution_id
WHERE e.workspace_id = ? AND e.execution_id = ?
  AND c.checkpoint_id = ? AND c.status = 'active'
`, checkpoint.ID, wakeID, clientSubmitID, unixMs(now), unixMs(now), unixMs(now),
		workspaceID, executionID, checkpoint.ID)
	if err != nil {
		return fmt.Errorf("prepare Tutti mode main wake: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect prepared Tutti mode main wake: %w", err)
	}
	if affected == 0 {
		var equivalent int
		err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM workspace_tutti_execution_wakes w
JOIN workspace_tutti_executions e
  ON e.workspace_id = w.workspace_id AND e.execution_id = w.execution_id
JOIN workspace_tutti_execution_checkpoints c
  ON c.workspace_id = w.workspace_id AND c.execution_id = w.execution_id
 AND c.checkpoint_id = w.checkpoint_id
WHERE w.workspace_id = ? AND w.execution_id = ? AND w.wake_id = ?
  AND w.checkpoint_id = ? AND w.target_kind = 'main' AND w.wake_sequence = 1
  AND w.client_submit_id = ? AND w.target_session_id = e.source_session_id
  AND w.status IN ('prepared', 'leased', 'dispatched', 'turn_settled')
  AND c.status = 'active'
`, workspaceID, executionID, wakeID, checkpoint.ID, clientSubmitID).Scan(&equivalent)
		if err != nil || equivalent != 1 {
			if err != nil {
				return fmt.Errorf("verify Tutti mode main wake: %w", err)
			}
			return executionbiz.ErrWakeIntegrity
		}
	}
	return nil
}

func acknowledgeTuttiModeCheckpointWakesTx(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	executionID string,
	checkpointID string,
	now time.Time,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'acknowledged', lease_owner = '', lease_expires_at_unix_ms = 0,
    acknowledged_at_unix_ms = ?, updated_at_unix_ms = ?
WHERE workspace_id = ? AND execution_id = ? AND checkpoint_id = ?
  AND status IN ('prepared', 'leased', 'dispatched', 'turn_settled')
`, unixMs(now), unixMs(now), workspaceID, executionID, checkpointID)
	if err != nil {
		return fmt.Errorf("acknowledge Tutti mode checkpoint wakes: %w", err)
	}
	return requireRowsAffected(
		result,
		executionbiz.ErrWakeRejected,
		"acknowledge Tutti mode checkpoint wake",
	)
}

func (s *SQLiteStore) ListTuttiModeExecutionWakes(
	ctx context.Context,
	workspaceID string,
	issueID string,
) ([]executionbiz.Wake, error) {
	if err := s.ensureIssueDatabase(); err != nil {
		return nil, err
	}
	rows, err := s.readDB.QueryContext(ctx, wakeSelectQuery+`
WHERE e.workspace_id = ? AND e.issue_id = ?
ORDER BY c.sequence ASC, w.wake_sequence ASC
`, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID))
	if err != nil {
		return nil, fmt.Errorf("list Tutti mode execution wakes: %w", err)
	}
	return scanTuttiModeWakes(rows)
}

func (s *SQLiteStore) ListDispatchableTuttiModeMainWakes(
	ctx context.Context,
	workspaceID string,
	now time.Time,
) ([]executionbiz.Wake, error) {
	if err := s.ensureIssueDatabase(); err != nil {
		return nil, err
	}
	rows, err := s.readDB.QueryContext(ctx, wakeSelectQuery+`
WHERE e.workspace_id = ?
  AND e.status IN ('awaiting_schedule', 'running', 'awaiting_main', 'pending_goal_review')
  AND c.status = 'active'
  AND w.target_kind = 'main' AND w.status = 'prepared'
  AND w.due_at_unix_ms <= ?
ORDER BY w.due_at_unix_ms ASC, e.execution_id ASC, c.sequence ASC, w.wake_sequence ASC
`, strings.TrimSpace(workspaceID), unixMs(now))
	if err != nil {
		return nil, fmt.Errorf("list dispatchable Tutti mode main wakes: %w", err)
	}
	return scanTuttiModeWakes(rows)
}

func (s *SQLiteStore) ListCorruptedTuttiModeMainWakes(
	ctx context.Context,
	workspaceID string,
	now time.Time,
) ([]executionbiz.Wake, error) {
	if err := s.ensureIssueDatabase(); err != nil {
		return nil, err
	}
	rows, err := s.readDB.QueryContext(ctx, wakeSelectQuery+`
WHERE e.workspace_id = ?
  AND e.status IN ('awaiting_schedule', 'running', 'awaiting_main', 'pending_goal_review')
  AND c.status = 'active'
  AND w.status = 'prepared' AND w.due_at_unix_ms <= ?
  AND w.target_kind != 'main'
  AND w.wake_sequence > 0
  AND w.wake_id = c.checkpoint_id || ':wake:main:' || CAST(w.wake_sequence AS TEXT)
  AND w.client_submit_id = 'tutti-execution-wake:' || w.wake_id
ORDER BY w.due_at_unix_ms ASC, e.execution_id ASC, c.sequence ASC
`, strings.TrimSpace(workspaceID), unixMs(now))
	if err != nil {
		return nil, fmt.Errorf("list corrupted Tutti mode main wakes: %w", err)
	}
	return scanTuttiModeWakes(rows)
}

func (s *SQLiteStore) GetTuttiModeExecutionWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
) (executionbiz.Wake, bool, error) {
	if err := s.ensureIssueDatabase(); err != nil {
		return executionbiz.Wake{}, false, err
	}
	wake, err := scanTuttiModeWake(s.readDB.QueryRowContext(ctx, wakeSelectQuery+`
WHERE e.workspace_id = ? AND w.wake_id = ?
`, strings.TrimSpace(workspaceID), strings.TrimSpace(wakeID)))
	if errors.Is(err, sql.ErrNoRows) {
		return executionbiz.Wake{}, false, nil
	}
	if err != nil {
		return executionbiz.Wake{}, false, fmt.Errorf("get Tutti mode execution wake: %w", err)
	}
	return wake, true, nil
}

func (s *SQLiteStore) ClaimTuttiModeExecutionWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
	now time.Time,
	leaseExpiresAt time.Time,
) (bool, error) {
	result, err := s.writeDB.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'leased', attempt_count = attempt_count + 1,
    lease_owner = ?, lease_expires_at_unix_ms = ?, updated_at_unix_ms = ?
WHERE workspace_id = ? AND wake_id = ? AND status = 'prepared'
  AND due_at_unix_ms <= ?
  AND EXISTS (
    SELECT 1
    FROM workspace_tutti_executions e
    JOIN workspace_tutti_execution_checkpoints c
      ON c.workspace_id = e.workspace_id AND c.execution_id = e.execution_id
    WHERE e.workspace_id = workspace_tutti_execution_wakes.workspace_id
      AND e.execution_id = workspace_tutti_execution_wakes.execution_id
      AND e.source_session_id = workspace_tutti_execution_wakes.target_session_id
      AND e.status IN ('awaiting_schedule', 'running', 'awaiting_main', 'pending_goal_review')
      AND c.checkpoint_id = workspace_tutti_execution_wakes.checkpoint_id
      AND c.status = 'active'
  )
`, leaseOwner, unixMs(leaseExpiresAt), unixMs(now), workspaceID, wakeID, unixMs(now))
	if err != nil {
		return false, fmt.Errorf("claim Tutti mode execution wake: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (s *SQLiteStore) ReleaseTuttiModeExecutionWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
	message string,
	now time.Time,
) error {
	result, err := s.writeDB.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'prepared', lease_owner = '', lease_expires_at_unix_ms = 0,
    last_error = ?, updated_at_unix_ms = ?
WHERE workspace_id = ? AND wake_id = ? AND status = 'leased' AND lease_owner = ?
`, strings.TrimSpace(message), unixMs(now), workspaceID, wakeID, leaseOwner)
	if err != nil {
		return fmt.Errorf("release Tutti mode execution wake: %w", err)
	}
	return requireRowsAffected(result, executionbiz.ErrWakeRejected, "release owned Tutti mode execution wake")
}

func (s *SQLiteStore) MarkTuttiModeExecutionWakeDispatched(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
	canonicalSessionID string,
	canonicalTurnID string,
	now time.Time,
) error {
	result, err := s.writeDB.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'dispatched', canonical_session_id = ?, canonical_turn_id = ?,
    lease_owner = '', lease_expires_at_unix_ms = 0, last_error = '',
    dispatched_at_unix_ms = ?, updated_at_unix_ms = ?
WHERE workspace_id = ? AND wake_id = ? AND status = 'leased' AND lease_owner = ?
  AND lease_expires_at_unix_ms > ?
  AND target_session_id = ?
  AND EXISTS (
    SELECT 1
    FROM workspace_tutti_executions e
    JOIN workspace_tutti_execution_checkpoints c
      ON c.workspace_id = e.workspace_id AND c.execution_id = e.execution_id
    WHERE e.workspace_id = workspace_tutti_execution_wakes.workspace_id
      AND e.execution_id = workspace_tutti_execution_wakes.execution_id
      AND e.source_session_id = workspace_tutti_execution_wakes.target_session_id
      AND e.status IN ('awaiting_schedule', 'running', 'awaiting_main', 'pending_goal_review')
      AND c.checkpoint_id = workspace_tutti_execution_wakes.checkpoint_id
      AND c.status = 'active'
  )
`, canonicalSessionID, canonicalTurnID, unixMs(now), unixMs(now),
		workspaceID, wakeID, leaseOwner, unixMs(now), canonicalSessionID)
	if err != nil {
		return fmt.Errorf("mark Tutti mode execution wake dispatched: %w", err)
	}
	return requireRowsAffected(result, executionbiz.ErrWakeRejected, "dispatch owned Tutti mode execution wake")
}

func (s *SQLiteStore) MarkTuttiModeExecutionWakeTurnSettled(
	ctx context.Context,
	workspaceID string,
	sessionID string,
	turnID string,
	now time.Time,
) (bool, error) {
	result, err := s.writeDB.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'turn_settled', turn_settled_at_unix_ms = ?, updated_at_unix_ms = ?
WHERE workspace_id = ? AND target_kind = 'main' AND status = 'dispatched'
  AND canonical_session_id = ? AND canonical_turn_id = ?
`, unixMs(now), unixMs(now), workspaceID, sessionID, turnID)
	if err != nil {
		return false, fmt.Errorf("settle Tutti mode main wake Turn: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (s *SQLiteStore) FailTuttiModeExecutionWakeIntegrity(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	message string,
	now time.Time,
) error {
	result, err := s.writeDB.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'failed', lease_owner = '', lease_expires_at_unix_ms = 0,
    last_error = ?, updated_at_unix_ms = ?
WHERE workspace_id = ? AND wake_id = ? AND status IN ('prepared', 'leased')
`, strings.TrimSpace(message), unixMs(now), workspaceID, wakeID)
	if err != nil {
		return fmt.Errorf("fail invalid Tutti mode execution wake: %w", err)
	}
	return requireRowsAffected(result, executionbiz.ErrWakeRejected, "fail invalid Tutti mode execution wake")
}

func (s *SQLiteStore) RequeueExpiredTuttiModeExecutionWakes(
	ctx context.Context,
	workspaceID string,
	now time.Time,
) error {
	_, err := s.writeDB.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'prepared', lease_owner = '', lease_expires_at_unix_ms = 0,
    updated_at_unix_ms = ?
WHERE workspace_id = ? AND status = 'leased' AND lease_expires_at_unix_ms <= ?
`, unixMs(now), strings.TrimSpace(workspaceID), unixMs(now))
	if err != nil {
		return fmt.Errorf("requeue expired Tutti mode execution wakes: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CancelSuppressedTuttiModeExecutionWakes(
	ctx context.Context,
	workspaceID string,
	now time.Time,
) error {
	_, err := s.writeDB.ExecContext(ctx, `
UPDATE workspace_tutti_execution_wakes
SET status = 'canceled', lease_owner = '', lease_expires_at_unix_ms = 0,
    updated_at_unix_ms = ?
WHERE workspace_id = ? AND status IN ('prepared', 'leased', 'dispatched', 'turn_settled')
  AND EXISTS (
    SELECT 1 FROM workspace_tutti_executions e
    WHERE e.workspace_id = workspace_tutti_execution_wakes.workspace_id
      AND e.execution_id = workspace_tutti_execution_wakes.execution_id
      AND e.status IN ('orphaned_source', 'completed', 'archiving', 'archived')
  )
`, unixMs(now), strings.TrimSpace(workspaceID))
	if err != nil {
		return fmt.Errorf("cancel suppressed Tutti mode execution wakes: %w", err)
	}
	return nil
}

const wakeSelectQuery = `
SELECT w.wake_id, w.workspace_id, w.execution_id, e.issue_id,
       w.checkpoint_id, c.kind, c.graph_revision, w.target_kind,
       w.wake_sequence, w.client_submit_id, e.source_session_id, w.target_session_id,
       w.canonical_session_id, w.canonical_turn_id, w.status,
       w.due_at_unix_ms, w.attempt_count, w.lease_owner,
       w.lease_expires_at_unix_ms, w.dispatched_at_unix_ms,
       w.turn_settled_at_unix_ms, w.acknowledged_at_unix_ms,
       w.last_error, w.created_at_unix_ms, w.updated_at_unix_ms
FROM workspace_tutti_execution_wakes w
JOIN workspace_tutti_executions e
  ON e.workspace_id = w.workspace_id AND e.execution_id = w.execution_id
JOIN workspace_tutti_execution_checkpoints c
  ON c.workspace_id = w.workspace_id AND c.execution_id = w.execution_id
 AND c.checkpoint_id = w.checkpoint_id
`

func scanTuttiModeWakes(rows *sql.Rows) ([]executionbiz.Wake, error) {
	defer func() { _ = rows.Close() }()
	wakes := make([]executionbiz.Wake, 0)
	for rows.Next() {
		wake, err := scanTuttiModeWake(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Tutti mode execution wake: %w", err)
		}
		wakes = append(wakes, wake)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Tutti mode execution wakes: %w", err)
	}
	return wakes, nil
}

func scanTuttiModeWake(scanner rowScanner) (executionbiz.Wake, error) {
	var wake executionbiz.Wake
	var dueAt, leaseExpiresAt, dispatchedAt, turnSettledAt, acknowledgedAt int64
	var createdAt, updatedAt int64
	err := scanner.Scan(
		&wake.ID, &wake.WorkspaceID, &wake.ExecutionID, &wake.IssueID,
		&wake.CheckpointID, &wake.CheckpointKind, &wake.CheckpointRevision,
		&wake.TargetKind, &wake.Sequence, &wake.ClientSubmitID,
		&wake.SourceSessionID, &wake.TargetSessionID,
		&wake.CanonicalSessionID, &wake.CanonicalTurnID,
		&wake.Status, &dueAt, &wake.AttemptCount, &wake.LeaseOwner,
		&leaseExpiresAt, &dispatchedAt, &turnSettledAt, &acknowledgedAt,
		&wake.LastError, &createdAt, &updatedAt,
	)
	if err != nil {
		return executionbiz.Wake{}, err
	}
	wake.DueAt = time.UnixMilli(dueAt).UTC()
	wake.LeaseExpiresAt = optionalTuttiModeExecutionTime(leaseExpiresAt)
	wake.DispatchedAt = optionalTuttiModeExecutionTime(dispatchedAt)
	wake.TurnSettledAt = optionalTuttiModeExecutionTime(turnSettledAt)
	wake.AcknowledgedAt = optionalTuttiModeExecutionTime(acknowledgedAt)
	wake.CreatedAt = time.UnixMilli(createdAt).UTC()
	wake.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return wake, nil
}
