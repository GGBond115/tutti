package storesqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

const maxLifecycleCleanupBatchSize = 500

func (store *Store) migrateLifecycle(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `ALTER TABLE connector_market_operations ADD COLUMN updated_at_unix_ms INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!isDuplicateColumnError(err) {
		return fmt.Errorf("migrate connector market operation update timestamp: %w", err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS connector_market_release_installations (
  connector_key TEXT NOT NULL,
  release_digest TEXT NOT NULL,
  release_json TEXT NOT NULL,
  PRIMARY KEY (connector_key, release_digest)
)`); err != nil {
		return fmt.Errorf("migrate connector release installations: %w", err)
	}
	if err := store.backfillLifecycleColumns(ctx); err != nil {
		return err
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS connector_market_operations_terminal_cleanup
ON connector_market_operations(updated_at_unix_ms, operation_id)
WHERE state IN ('completed', 'failed')`,
		`CREATE INDEX IF NOT EXISTS connector_market_outbox_published_cleanup
ON connector_market_outbox(published_at_unix_ms, sequence)
WHERE published_at_unix_ms IS NOT NULL`,
	}
	for _, statement := range statements {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate connector market lifecycle index: %w", err)
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}

func (store *Store) backfillLifecycleColumns(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
SELECT operation_id, operation_json FROM connector_market_operations
WHERE updated_at_unix_ms = 0`)
	if err != nil {
		return err
	}
	type timestampBackfill struct {
		operationID string
		updatedAtMS int64
	}
	var updates []timestampBackfill
	for rows.Next() {
		var operationID, payload string
		if err := rows.Scan(&operationID, &payload); err != nil {
			_ = rows.Close()
			return err
		}
		operation, err := decodeOperation(payload)
		if err != nil {
			_ = rows.Close()
			return err
		}
		updates = append(updates, timestampBackfill{operationID: operationID, updatedAtMS: operation.UpdatedAt.UTC().UnixMilli()})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE connector_market_operations SET updated_at_unix_ms = ? WHERE operation_id = ? AND updated_at_unix_ms = 0`,
			update.updatedAtMS, update.operationID); err != nil {
			return err
		}
	}
	if err := backfillInstalledReleaseEvidence(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func backfillInstalledReleaseEvidence(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT connector_json FROM connector_market_connectors`)
	if err != nil {
		return err
	}
	var connectors []contracts.Connector
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			_ = rows.Close()
			return err
		}
		connector, err := decodeConnector(payload)
		if err != nil {
			_ = rows.Close()
			return err
		}
		connectors = append(connectors, connector)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, connector := range connectors {
		digest := connector.Installation.InstalledReleaseDigest
		if digest == "" {
			continue
		}
		var historicalDigest string
		err := tx.QueryRowContext(ctx, `SELECT release_digest FROM connector_market_release_installations
WHERE connector_key = ? AND release_digest = ?`, connector.Key, digest).Scan(&historicalDigest)
		if err == nil {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		release, found, err := legacyInstalledReleaseEvidence(ctx, tx, connector, digest)
		if err != nil {
			return err
		}
		if !found {
			// Older stores could promote a prepared candidate in the current-release
			// index before runtime convergence completed. If that update then failed,
			// the previous release metadata was no longer recoverable from SQLite.
			// Keep the store available and let runtime recovery fail this connector
			// closed until a subsequent install repairs its durable evidence.
			continue
		}
		if err := saveInstalledReleaseOn(ctx, tx, release); err != nil {
			return err
		}
	}
	return nil
}

func legacyInstalledReleaseEvidence(ctx context.Context, tx *sql.Tx, connector contracts.Connector, digest string) (contracts.Release, bool, error) {
	var currentPayload string
	err := tx.QueryRowContext(ctx, `SELECT release_json FROM connector_market_installed_releases
WHERE connector_key = ? AND release_digest = ?`, connector.Key, digest).Scan(&currentPayload)
	if err == nil {
		var release contracts.Release
		if err := json.Unmarshal([]byte(currentPayload), &release); err != nil {
			return contracts.Release{}, false, fmt.Errorf("decode current installed connector release: %w", err)
		}
		return release, true, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return contracts.Release{}, false, err
	}
	if connector.Release.ReleaseDigest == digest {
		return connector.Release, true, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT operation_json FROM connector_market_operations
WHERE connector_key = ? AND kind = ? AND state = ? ORDER BY updated_at_unix_ms DESC`,
		connector.Key, contracts.OperationKindInstall, contracts.OperationStateCompleted)
	if err != nil {
		return contracts.Release{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return contracts.Release{}, false, err
		}
		operation, err := decodeOperation(payload)
		if err != nil {
			return contracts.Release{}, false, err
		}
		if operation.Target != nil && operation.Target.Release != nil && operation.Target.ReleaseDigest == digest {
			return *operation.Target.Release, true, nil
		}
	}
	return contracts.Release{}, false, rows.Err()
}

func saveInstalledReleaseEvidenceOn(ctx context.Context, tx *sql.Tx, operation contracts.Operation) error {
	switch operation.Kind {
	case contracts.OperationKindInstall:
		if operation.Execution.ReleaseInstallation == nil && operation.State != contracts.OperationStateCompleted {
			return nil
		}
		if operation.Target == nil || operation.Target.Release == nil || operation.Target.ReleaseDigest == "" {
			return errors.New("prepared connector install is missing release evidence")
		}
		if operation.State == contracts.OperationStateCompleted {
			return saveInstalledReleaseOn(ctx, tx, *operation.Target.Release)
		}
		return saveReleaseInstallationOn(ctx, tx, *operation.Target.Release)
	case contracts.OperationKindUninstall:
		if operation.State != contracts.OperationStateCompleted {
			return nil
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM connector_market_installed_releases WHERE connector_key = ?`, operation.ConnectorKey)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM connector_market_release_installations WHERE connector_key = ?`, operation.ConnectorKey)
		return err
	default:
		return nil
	}
}

func saveInstalledReleaseOn(ctx context.Context, tx *sql.Tx, release contracts.Release) error {
	if err := saveReleaseInstallationOn(ctx, tx, release); err != nil {
		return err
	}
	payload, err := json.Marshal(release)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO connector_market_installed_releases (connector_key, release_digest, release_json)
VALUES (?, ?, ?)
ON CONFLICT(connector_key) DO UPDATE SET
  release_digest = excluded.release_digest,
  release_json = excluded.release_json`, release.ConnectorKey, release.ReleaseDigest, string(payload))
	return err
}

func saveReleaseInstallationOn(ctx context.Context, tx *sql.Tx, release contracts.Release) error {
	payload, err := json.Marshal(release)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO connector_market_release_installations (connector_key, release_digest, release_json)
VALUES (?, ?, ?)
ON CONFLICT(connector_key, release_digest) DO UPDATE SET
  release_json = excluded.release_json`, release.ConnectorKey, release.ReleaseDigest, string(payload))
	return err
}

func (store *Store) CleanupLifecycle(ctx context.Context, request contracts.LifecycleCleanupRequest) (contracts.LifecycleCleanupResult, error) {
	if request.BatchSize <= 0 || request.BatchSize > maxLifecycleCleanupBatchSize {
		return contracts.LifecycleCleanupResult{}, fmt.Errorf("connector market lifecycle cleanup batch size must be between 1 and %d", maxLifecycleCleanupBatchSize)
	}
	if request.TerminalOperationsUpdatedThrough.IsZero() || request.PublishedEventsPublishedThrough.IsZero() {
		return contracts.LifecycleCleanupResult{}, errors.New("connector market lifecycle cleanup cutoffs are required")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return contracts.LifecycleCleanupResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	operationResult, err := tx.ExecContext(ctx, `
DELETE FROM connector_market_operations
WHERE operation_id IN (
  SELECT operation_id FROM connector_market_operations
  WHERE state IN ('completed', 'failed') AND updated_at_unix_ms <= ?
  ORDER BY updated_at_unix_ms, operation_id LIMIT ?
)
AND state IN ('completed', 'failed') AND updated_at_unix_ms <= ?`,
		request.TerminalOperationsUpdatedThrough.UTC().UnixMilli(), request.BatchSize,
		request.TerminalOperationsUpdatedThrough.UTC().UnixMilli())
	if err != nil {
		return contracts.LifecycleCleanupResult{}, err
	}
	outboxResult, err := tx.ExecContext(ctx, `
DELETE FROM connector_market_outbox
WHERE sequence IN (
  SELECT sequence FROM connector_market_outbox
  WHERE published_at_unix_ms IS NOT NULL AND published_at_unix_ms <= ?
  ORDER BY published_at_unix_ms, sequence LIMIT ?
)
AND published_at_unix_ms IS NOT NULL AND published_at_unix_ms <= ?`,
		request.PublishedEventsPublishedThrough.UTC().UnixMilli(), request.BatchSize,
		request.PublishedEventsPublishedThrough.UTC().UnixMilli())
	if err != nil {
		return contracts.LifecycleCleanupResult{}, err
	}
	operationsDeleted, err := operationResult.RowsAffected()
	if err != nil {
		return contracts.LifecycleCleanupResult{}, err
	}
	eventsDeleted, err := outboxResult.RowsAffected()
	if err != nil {
		return contracts.LifecycleCleanupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return contracts.LifecycleCleanupResult{}, err
	}
	return contracts.LifecycleCleanupResult{
		TerminalOperationsDeleted: operationsDeleted,
		PublishedEventsDeleted:    eventsDeleted,
	}, nil
}
