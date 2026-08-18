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

type transaction struct {
	ctx      context.Context
	tx       *sql.Tx
	revision uint64
}

func (transaction *transaction) Revision() uint64 { return transaction.revision }

func (transaction *transaction) AdvanceRevision() uint64 {
	transaction.revision++
	return transaction.revision
}

func (transaction *transaction) Connectors() ([]contracts.Connector, error) {
	rows, err := transaction.tx.QueryContext(transaction.ctx, `
SELECT connector_json FROM connector_market_connectors ORDER BY connector_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connectors := make([]contracts.Connector, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		connector, err := decodeConnector(payload)
		if err != nil {
			return nil, err
		}
		connectors = append(connectors, connector)
	}
	return connectors, rows.Err()
}

func (transaction *transaction) Connector(connectorKey string) (contracts.Connector, error) {
	var payload string
	if err := transaction.tx.QueryRowContext(transaction.ctx, `
SELECT connector_json FROM connector_market_connectors WHERE connector_key = ?`, connectorKey).Scan(&payload); err != nil {
		return contracts.Connector{}, mapNotFound(err)
	}
	return decodeConnector(payload)
}

func (transaction *transaction) Operation(operationID string) (contracts.Operation, error) {
	return operationOn(transaction.ctx, transaction.tx, operationID)
}

func (transaction *transaction) OperationByClientRequestID(ownerAccountID, clientRequestID string) (*contracts.Operation, error) {
	var payload string
	if err := transaction.tx.QueryRowContext(transaction.ctx, `
SELECT operation_json FROM connector_market_operations
WHERE owner_account_id = ? AND client_request_id = ?`,
		strings.TrimSpace(ownerAccountID), clientRequestID).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	operation, err := decodeOperation(payload)
	return &operation, err
}

func (transaction *transaction) ActiveOperation(connectorKey string) (*contracts.Operation, error) {
	var payload string
	query := `
SELECT operation_json FROM connector_market_operations
WHERE connector_key IN ('', ?) AND state IN ('accepted', 'running') LIMIT 1`
	arguments := []any{connectorKey}
	if strings.TrimSpace(connectorKey) == "" {
		query = `SELECT operation_json FROM connector_market_operations WHERE state IN ('accepted', 'running') LIMIT 1`
		arguments = nil
	}
	if err := transaction.tx.QueryRowContext(transaction.ctx, query, arguments...).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	operation, err := decodeOperation(payload)
	return &operation, err
}

func (transaction *transaction) CatalogFreshness() (contracts.CatalogFreshness, error) {
	return readCatalogFreshness(transaction.ctx, transaction.tx)
}

func (transaction *transaction) ReplaceCatalogSnapshot(
	generation uint64,
	snapshot contracts.CatalogSnapshot,
	acceptedAt time.Time,
) (bool, error) {
	var currentGeneration uint64
	var appliedGeneration uint64
	var previousSnapshotID sql.NullString
	if err := transaction.tx.QueryRowContext(transaction.ctx, `
SELECT fetch_generation, applied_generation, active_snapshot_id
FROM connector_market_catalog_state WHERE id = ?`, metadataID).
		Scan(&currentGeneration, &appliedGeneration, &previousSnapshotID); err != nil {
		return false, err
	}
	if generation != currentGeneration || generation <= appliedGeneration {
		return false, nil
	}
	snapshotID := fmt.Sprintf("catalog-%d", generation)
	if _, err := transaction.tx.ExecContext(transaction.ctx, `
INSERT INTO connector_market_catalog_snapshots (snapshot_id, source_revision, accepted_at_unix_ms)
VALUES (?, ?, ?)`, snapshotID, strings.TrimSpace(snapshot.SourceRevision), acceptedAt.UTC().UnixMilli()); err != nil {
		return false, err
	}
	for _, category := range snapshot.Categories {
		if _, err := transaction.tx.ExecContext(transaction.ctx, `
INSERT INTO connector_market_catalog_categories
(snapshot_id, category_id, kind, sort_order, item_count, display_name_zh, display_name_en)
VALUES (?, ?, ?, ?, ?, ?, ?)`, snapshotID, category.CategoryID, category.Kind, category.SortOrder,
			category.ItemCount, category.DisplayNameZH, category.DisplayNameEN); err != nil {
			return false, err
		}
	}
	releases := make(map[string]contracts.Release, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		releases[entry.Release.ConnectorKey] = entry.Release
	}
	for connectorKey, release := range releases {
		payload, err := json.Marshal(release)
		if err != nil {
			return false, err
		}
		if _, err := transaction.tx.ExecContext(transaction.ctx, `
INSERT INTO connector_market_catalog_releases (snapshot_id, connector_key, release_json)
VALUES (?, ?, ?)`, snapshotID, connectorKey, string(payload)); err != nil {
			return false, err
		}
	}
	for _, entry := range snapshot.Entries {
		if _, err := transaction.tx.ExecContext(transaction.ctx, `
INSERT INTO connector_market_catalog_placements
(snapshot_id, section_id, placement_order, category_id, connector_key, featured)
VALUES (?, ?, ?, ?, ?, ?)`, snapshotID, entry.SectionID, entry.Order, entry.CategoryID,
			entry.Release.ConnectorKey, boolInt(entry.Featured)); err != nil {
			return false, err
		}
	}
	result, err := transaction.tx.ExecContext(transaction.ctx, `
UPDATE connector_market_catalog_state
SET active_snapshot_id = ?, applied_generation = ?, freshness_state = 'fresh',
    stale_since_unix_ms = NULL, last_failure_code = ''
WHERE id = ? AND fetch_generation = ? AND applied_generation < ?`, snapshotID, generation, metadataID, generation, generation)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed == 0 {
		return false, nil
	}
	if previousSnapshotID.Valid && previousSnapshotID.String != snapshotID {
		if _, err := transaction.tx.ExecContext(transaction.ctx, `
DELETE FROM connector_market_catalog_snapshots WHERE snapshot_id = ?`, previousSnapshotID.String); err != nil {
			return false, err
		}
	}
	return true, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (transaction *transaction) SaveConnector(connector contracts.Connector) error {
	payload, err := json.Marshal(connector)
	if err != nil {
		return err
	}
	_, err = transaction.tx.ExecContext(transaction.ctx, `
INSERT INTO connector_market_connectors (connector_key, connector_json)
VALUES (?, ?)
ON CONFLICT(connector_key) DO UPDATE SET connector_json = excluded.connector_json`,
		connector.Key, string(payload))
	return err
}

func (transaction *transaction) DeleteConnector(connectorKey string) error {
	_, err := transaction.tx.ExecContext(transaction.ctx, `
DELETE FROM connector_market_connectors WHERE connector_key = ?`, connectorKey)
	return err
}

func (transaction *transaction) SaveOperation(operation contracts.Operation) error {
	return saveOperationOn(transaction.ctx, transaction.tx, operation)
}

func (transaction *transaction) RuntimeConvergence(
	scope contracts.OperationScope,
	connectorKey string,
) (contracts.RuntimeConvergence, error) {
	return runtimeConvergenceOn(transaction.ctx, transaction.tx, scope, connectorKey)
}

func (transaction *transaction) SaveRuntimeConvergence(convergence contracts.RuntimeConvergence) error {
	return saveRuntimeConvergenceOn(transaction.ctx, transaction.tx, convergence)
}

func (transaction *transaction) DeleteRuntimeConvergence(scope contracts.OperationScope, connectorKey string) error {
	_, err := transaction.tx.ExecContext(transaction.ctx, `
DELETE FROM connector_market_runtime_convergence
WHERE account_id = ? AND connector_key = ?`, strings.TrimSpace(scope.AccountID), strings.TrimSpace(connectorKey))
	return err
}

func (transaction *transaction) EnqueueConnectorMarketChanged(event contracts.ChangedEvent) error {
	if strings.TrimSpace(event.OperationID) != "" {
		operation, err := transaction.Operation(event.OperationID)
		if err != nil && !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		if err == nil {
			operation = contracts.NormalizeOperationOwnership(operation)
			if operation.Visibility == contracts.OperationVisibilityAccount {
				accountEvent := event
				accountEvent.OwnerAccountID = operation.OwnerAccountID
				accountEvent.Visibility = contracts.OperationVisibilityAccount
				if err := transaction.appendChangedEvent(accountEvent); err != nil {
					return err
				}
			}
		}
	}
	event.OperationID = ""
	event.OwnerAccountID = ""
	event.Visibility = contracts.OperationVisibilitySystemPrivate
	return transaction.appendChangedEvent(event)
}

func (transaction *transaction) appendChangedEvent(event contracts.ChangedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = transaction.tx.ExecContext(transaction.ctx, `
INSERT INTO connector_market_outbox (revision, event_json, published_at_unix_ms)
VALUES (?, ?, NULL)`, event.Revision, string(payload))
	return err
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func listConnectorsOn(ctx context.Context, database queryer) ([]contracts.Connector, error) {
	rows, err := database.QueryContext(ctx, `
SELECT connector_json FROM connector_market_connectors ORDER BY connector_key`)
	if err != nil {
		return nil, err
	}
	connectors := make([]contracts.Connector, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		connector, err := decodeConnector(payload)
		if err != nil {
			return nil, err
		}
		connectors = append(connectors, connector)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return connectors, nil
}

func listOperationsOn(ctx context.Context, database queryer, accountID string) ([]contracts.Operation, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return []contracts.Operation{}, nil
	}
	rows, err := database.QueryContext(ctx, `
SELECT operation_json FROM connector_market_operations
WHERE owner_account_id = ? AND visibility = ? ORDER BY operation_id`,
		accountID, contracts.OperationVisibilityAccount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	operations := make([]contracts.Operation, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		operation, err := decodeOperation(payload)
		if err != nil {
			return nil, err
		}
		operations = append(operations, publicOperation(operation))
	}
	return operations, rows.Err()
}

func operationOn(ctx context.Context, tx *sql.Tx, operationID string) (contracts.Operation, error) {
	var payload string
	if err := tx.QueryRowContext(ctx, `
SELECT operation_json FROM connector_market_operations WHERE operation_id = ?`, operationID).Scan(&payload); err != nil {
		return contracts.Operation{}, mapNotFound(err)
	}
	return decodeOperation(payload)
}

func saveOperationOn(ctx context.Context, tx *sql.Tx, operation contracts.Operation) error {
	operation = contracts.NormalizeOperationOwnership(operation)
	payload, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	var leaseExpiresAt any
	if operation.LeaseExpiresAt != nil {
		leaseExpiresAt = operation.LeaseExpiresAt.UTC().UnixMilli()
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO connector_market_operations (
  operation_id, client_request_id, owner_account_id, visibility, connector_key, kind, state,
  lease_owner, lease_token, lease_expires_at_unix_ms, updated_at_unix_ms, operation_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(operation_id) DO UPDATE SET
	owner_account_id = excluded.owner_account_id,
	visibility = excluded.visibility,
  state = excluded.state,
  lease_owner = excluded.lease_owner,
	lease_token = excluded.lease_token,
  lease_expires_at_unix_ms = excluded.lease_expires_at_unix_ms,
	updated_at_unix_ms = excluded.updated_at_unix_ms,
	operation_json = excluded.operation_json
WHERE excluded.lease_token = 0 OR (
  connector_market_operations.lease_owner = excluded.lease_owner AND
  connector_market_operations.lease_token = excluded.lease_token
)`,
		operation.OperationID, operation.ClientRequestID, operation.OwnerAccountID, operation.Visibility, operation.ConnectorKey,
		operation.Kind, operation.State, operation.LeaseOwner, operation.LeaseToken, leaseExpiresAt,
		operation.UpdatedAt.UTC().UnixMilli(), string(payload))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if operation.LeaseToken > 0 && changed != 1 {
		return contracts.ErrOperationLeaseLost
	}
	return saveInstalledReleaseEvidenceOn(ctx, tx, operation)
}

func decodeConnector(payload string) (contracts.Connector, error) {
	var connector contracts.Connector
	if err := json.Unmarshal([]byte(payload), &connector); err != nil {
		return contracts.Connector{}, fmt.Errorf("decode connector market connector: %w", err)
	}
	return connector, nil
}

func decodeOperation(payload string) (contracts.Operation, error) {
	var operation contracts.Operation
	if err := json.Unmarshal([]byte(payload), &operation); err != nil {
		return contracts.Operation{}, fmt.Errorf("decode connector market operation: %w", err)
	}
	return operation, nil
}

func publicOperation(operation contracts.Operation) contracts.Operation {
	operation.Execution = contracts.OperationExecution{}
	operation.LeaseOwner = ""
	operation.LeaseToken = 0
	operation.LeaseExpiresAt = nil
	if operation.Target != nil {
		target := *operation.Target
		target.Release = nil
		operation.Target = &target
	}
	return operation
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.ErrNotFound
	}
	return err
}
