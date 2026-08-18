package storesqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

const maxLifecycleCleanupBatchSize = 500

const installedReleaseEvidenceMigrationV1 = "installed_release_evidence_v1"

func (store *Store) migrateLifecycle(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `ALTER TABLE connector_market_operations ADD COLUMN updated_at_unix_ms INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!isDuplicateColumnError(err) {
		return fmt.Errorf("migrate connector market operation update timestamp: %w", err)
	}
	if err := store.backfillLifecycleColumns(ctx); err != nil {
		return err
	}
	if err := store.migrateInstalledReleaseEvidence(ctx); err != nil {
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
	return tx.Commit()
}

func (store *Store) migrateInstalledReleaseEvidence(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS connector_market_schema_migrations (
  id TEXT PRIMARY KEY,
  applied_at_unix_ms INTEGER NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_release_installations (
  connector_key TEXT NOT NULL,
  release_digest TEXT NOT NULL,
  release_json TEXT NOT NULL,
  PRIMARY KEY (connector_key, release_digest)
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_current_release_installations (
  connector_key TEXT PRIMARY KEY,
  release_digest TEXT NOT NULL,
  release_json TEXT NOT NULL
)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("prepare connector installed-release migration: %w", err)
		}
	}
	// Serialize concurrent Open calls before checking the durable marker. The
	// no-op write acquires SQLite's writer lock without changing a revision.
	if _, err := tx.ExecContext(ctx, `UPDATE connector_market_metadata SET revision = revision WHERE id = ?`, metadataID); err != nil {
		return fmt.Errorf("lock connector installed-release migration: %w", err)
	}
	var applied int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM connector_market_schema_migrations WHERE id = ?`, installedReleaseEvidenceMigrationV1).Scan(&applied)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read connector installed-release migration marker: %w", err)
	}
	legacyInstalledReleasesExist, err := sqliteTableExists(ctx, tx, "connector_market_installed_releases")
	if err != nil {
		return err
	}
	if err := migrateInstalledReleaseEvidenceOn(ctx, tx, legacyInstalledReleasesExist); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO connector_market_schema_migrations (id, applied_at_unix_ms) VALUES (?, ?)`,
		installedReleaseEvidenceMigrationV1, time.Now().UTC().UnixMilli()); err != nil {
		return fmt.Errorf("record connector installed-release migration: %w", err)
	}
	return tx.Commit()
}

func sqliteTableExists(ctx context.Context, tx *sql.Tx, tableName string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect connector migration source table %q: %w", tableName, err)
	}
	return true, nil
}

func migrateInstalledReleaseEvidenceOn(ctx context.Context, tx *sql.Tx, legacyInstalledReleasesExist bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT connector_json FROM connector_market_connectors`)
	if err != nil {
		return fmt.Errorf("read legacy connector installation projections: %w", err)
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
		release, found, err := installedReleaseMigrationEvidence(ctx, tx, connector, digest, legacyInstalledReleasesExist)
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
		if err := validateInstalledReleaseEvidence(release, connector.Key, digest); err != nil {
			return err
		}
		if err := saveInstalledReleaseOn(ctx, tx, release); err != nil {
			return err
		}
	}
	return nil
}

func installedReleaseMigrationEvidence(
	ctx context.Context,
	tx *sql.Tx,
	connector contracts.Connector,
	digest string,
	legacyInstalledReleasesExist bool,
) (contracts.Release, bool, error) {
	var historicalPayload string
	err := tx.QueryRowContext(ctx, `SELECT release_json FROM connector_market_release_installations
WHERE connector_key = ? AND release_digest = ?`, connector.Key, digest).Scan(&historicalPayload)
	if err == nil {
		return decodeInstalledReleaseEvidence(historicalPayload, connector.Key, digest, "canonical history")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return contracts.Release{}, false, err
	}

	var currentPayload string
	if legacyInstalledReleasesExist {
		err = tx.QueryRowContext(ctx, `SELECT release_json FROM connector_market_installed_releases
WHERE connector_key = ? AND release_digest = ?`, connector.Key, digest).Scan(&currentPayload)
		if err == nil {
			return decodeInstalledReleaseEvidence(currentPayload, connector.Key, digest, "legacy installed release")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return contracts.Release{}, false, err
		}
	}
	if connector.Release.ReleaseDigest == digest {
		if err := validateInstalledReleaseEvidence(connector.Release, connector.Key, digest); err != nil {
			return contracts.Release{}, false, fmt.Errorf("validate legacy connector release: %w", err)
		}
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
			if err := validateInstalledReleaseEvidence(*operation.Target.Release, connector.Key, digest); err != nil {
				return contracts.Release{}, false, fmt.Errorf("validate legacy install operation release: %w", err)
			}
			return *operation.Target.Release, true, nil
		}
	}
	return contracts.Release{}, false, rows.Err()
}

func decodeInstalledReleaseEvidence(payload, connectorKey, releaseDigest, source string) (contracts.Release, bool, error) {
	var release contracts.Release
	if err := json.Unmarshal([]byte(payload), &release); err != nil {
		return contracts.Release{}, false, fmt.Errorf("decode %s: %w", source, err)
	}
	if err := validateInstalledReleaseEvidence(release, connectorKey, releaseDigest); err != nil {
		return contracts.Release{}, false, fmt.Errorf("validate %s: %w", source, err)
	}
	return release, true, nil
}

func validateInstalledReleaseEvidence(release contracts.Release, connectorKey, releaseDigest string) error {
	if release.ConnectorKey != strings.TrimSpace(connectorKey) || release.ReleaseDigest != strings.TrimSpace(releaseDigest) {
		return errors.New("installed connector release identity does not match its durable key")
	}
	if err := contracts.ValidateRuntimeReleaseShape(release); err != nil {
		return fmt.Errorf("installed connector release is invalid: %w", err)
	}
	return nil
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
		_, err := tx.ExecContext(ctx, `DELETE FROM connector_market_current_release_installations WHERE connector_key = ?`, operation.ConnectorKey)
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
INSERT INTO connector_market_current_release_installations (connector_key, release_digest, release_json)
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
