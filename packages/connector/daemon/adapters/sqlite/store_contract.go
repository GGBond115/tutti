package storesqlite

import (
	"context"
	"database/sql"
	"fmt"

	market "github.com/tutti-os/tutti/packages/connector/daemon/core"
)

func (store *Store) migrateStoreContract(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `
ALTER TABLE connector_market_metadata ADD COLUMN store_contract INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!isDuplicateColumnError(err) {
		return fmt.Errorf("migrate connector market store contract: %w", err)
	}
	if err := store.ensureStoreContractTables(ctx); err != nil {
		return err
	}
	var contract int
	if err := store.db.QueryRowContext(ctx, `
SELECT store_contract FROM connector_market_metadata WHERE id = ?`, metadataID).Scan(&contract); err != nil {
		return fmt.Errorf("read connector market store contract: %w", err)
	}
	if contract >= currentStoreContract {
		return nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := discardIncompatibleStoreContractOn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) ensureStoreContractTables(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS connector_market_release_installations (
  connector_key TEXT NOT NULL,
  release_digest TEXT NOT NULL,
  release_json TEXT NOT NULL,
  PRIMARY KEY (connector_key, release_digest)
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_runtime_convergence (
  account_id TEXT NOT NULL,
  connector_key TEXT NOT NULL,
  desired_generation INTEGER NOT NULL CHECK (desired_generation > 0),
  observed_generation INTEGER NOT NULL CHECK (observed_generation >= 0),
  observed_boot_epoch TEXT NOT NULL,
  next_attempt_at_unix_ms INTEGER NOT NULL,
  lease_owner TEXT NOT NULL,
  lease_token INTEGER NOT NULL DEFAULT 0,
  lease_expires_at_unix_ms INTEGER,
  updated_at_unix_ms INTEGER NOT NULL,
  convergence_json TEXT NOT NULL,
  PRIMARY KEY (account_id, connector_key)
)`,
	}
	for _, statement := range statements {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure connector market store contract tables: %w", err)
		}
	}
	return nil
}

func discardIncompatibleStoreContractOn(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`DELETE FROM connector_market_connectors`,
		`DELETE FROM connector_market_installed_releases`,
		`DELETE FROM connector_market_release_installations`,
		`DELETE FROM connector_market_runtime_convergence`,
		`DELETE FROM connector_market_operations`,
		`DELETE FROM connector_market_outbox`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("discard incompatible connector market rows: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE connector_market_metadata
SET catalog_state = ?, source_revision = '', store_contract = ?
WHERE id = ?`, market.CatalogStateStale, currentStoreContract, metadataID); err != nil {
		return fmt.Errorf("record connector market store contract: %w", err)
	}
	return nil
}
