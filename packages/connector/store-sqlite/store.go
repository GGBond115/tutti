package storesqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
	_ "modernc.org/sqlite"
)

const metadataID = 1

type Store struct {
	db *sql.DB
}

var _ application.Repository = (*Store)(nil)
var _ application.ChangedEventOutbox = (*Store)(nil)
var _ application.LifecycleCleanupStore = (*Store)(nil)
var _ application.AuthorizationProjectionStore = (*Store)(nil)
var _ application.AuthorizationSnapshotStore = (*Store)(nil)

func Open(ctx context.Context, dbPath string) (*Store, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, errors.New("connector market database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create connector market database directory: %w", err)
	}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	dsn := connectorMarketSQLiteDSN(dbPath, query)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open connector market database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func connectorMarketSQLiteDSN(dbPath string, query url.Values) string {
	databaseURL := &url.URL{Scheme: "file", Path: dbPath, RawQuery: query.Encode()}
	if runtime.GOOS == "windows" && filepath.IsAbs(dbPath) {
		slashPath := filepath.ToSlash(dbPath)
		if uncPath := strings.TrimPrefix(slashPath, "//"); uncPath != slashPath {
			host, path, found := strings.Cut(uncPath, "/")
			if found {
				databaseURL.Host = host
				databaseURL.Path = "/" + path
			}
		} else {
			databaseURL.Path = "/" + slashPath
		}
	}
	return databaseURL.String()
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) migrate(ctx context.Context) error {
	statements := []string{
		`DROP TABLE IF EXISTS connector_market_catalog_trust`,
		`DROP TABLE IF EXISTS connector_market_security_revocations`,
		`CREATE TABLE IF NOT EXISTS connector_market_metadata (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  revision INTEGER NOT NULL,
  catalog_state TEXT NOT NULL,
  source_revision TEXT NOT NULL
)`,
		`INSERT INTO connector_market_metadata (id, revision, catalog_state, source_revision)
VALUES (1, 0, 'stale', '') ON CONFLICT(id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS connector_market_catalog_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  active_snapshot_id TEXT,
  fetch_generation INTEGER NOT NULL DEFAULT 0 CHECK (fetch_generation >= 0),
  applied_generation INTEGER NOT NULL DEFAULT 0 CHECK (applied_generation >= 0),
  freshness_state TEXT NOT NULL CHECK (freshness_state IN ('unavailable', 'refreshing', 'fresh', 'stale')),
  stale_since_unix_ms INTEGER,
  last_failure_code TEXT NOT NULL DEFAULT ''
)`,
		`INSERT INTO connector_market_catalog_state
(id, active_snapshot_id, fetch_generation, applied_generation, freshness_state, stale_since_unix_ms, last_failure_code)
VALUES (1, NULL, 0, 0, 'unavailable', NULL, '') ON CONFLICT(id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS connector_market_catalog_snapshots (
  snapshot_id TEXT PRIMARY KEY,
  source_revision TEXT NOT NULL,
  accepted_at_unix_ms INTEGER NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_catalog_categories (
  snapshot_id TEXT NOT NULL,
  category_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  sort_order INTEGER NOT NULL,
  item_count INTEGER NOT NULL,
  display_name_zh TEXT NOT NULL,
  display_name_en TEXT NOT NULL,
  PRIMARY KEY (snapshot_id, category_id),
  FOREIGN KEY (snapshot_id) REFERENCES connector_market_catalog_snapshots(snapshot_id) ON DELETE CASCADE
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_catalog_releases (
  snapshot_id TEXT NOT NULL,
  connector_key TEXT NOT NULL,
  release_json TEXT NOT NULL,
  PRIMARY KEY (snapshot_id, connector_key),
  FOREIGN KEY (snapshot_id) REFERENCES connector_market_catalog_snapshots(snapshot_id) ON DELETE CASCADE
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_catalog_placements (
  snapshot_id TEXT NOT NULL,
  section_id TEXT NOT NULL,
  placement_order INTEGER NOT NULL,
  category_id TEXT NOT NULL,
  connector_key TEXT NOT NULL,
  featured INTEGER NOT NULL CHECK (featured IN (0, 1)),
  PRIMARY KEY (snapshot_id, section_id, placement_order),
  FOREIGN KEY (snapshot_id) REFERENCES connector_market_catalog_snapshots(snapshot_id) ON DELETE CASCADE,
  FOREIGN KEY (snapshot_id, connector_key) REFERENCES connector_market_catalog_releases(snapshot_id, connector_key) ON DELETE CASCADE
)`,
		`CREATE INDEX IF NOT EXISTS connector_market_catalog_placements_section
ON connector_market_catalog_placements(snapshot_id, section_id, placement_order)`,
		`CREATE TABLE IF NOT EXISTS connector_market_connectors (
  connector_key TEXT PRIMARY KEY,
  connector_json TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_installed_releases (
  connector_key TEXT PRIMARY KEY,
  release_digest TEXT NOT NULL,
  release_json TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_authorization_projections (
  account_id TEXT NOT NULL,
  connector_key TEXT NOT NULL,
  projection_json TEXT NOT NULL,
  PRIMARY KEY (account_id, connector_key)
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_authorization_snapshot_revisions (
  account_id TEXT PRIMARY KEY,
  revision INTEGER NOT NULL CHECK (revision >= 0)
)`,
		`CREATE TABLE IF NOT EXISTS connector_market_operations (
  operation_id TEXT PRIMARY KEY,
  client_request_id TEXT NOT NULL,
  owner_account_id TEXT NOT NULL,
  visibility TEXT NOT NULL CHECK (visibility IN ('account', 'system_private')),
  connector_key TEXT NOT NULL,
  kind TEXT NOT NULL,
  state TEXT NOT NULL,
  lease_owner TEXT NOT NULL,
	lease_token INTEGER NOT NULL DEFAULT 0,
  lease_expires_at_unix_ms INTEGER,
	updated_at_unix_ms INTEGER NOT NULL DEFAULT 0,
  operation_json TEXT NOT NULL
)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS connector_market_one_active_operation
ON connector_market_operations(connector_key)
WHERE state IN ('accepted', 'running')`,
		`CREATE TABLE IF NOT EXISTS connector_market_outbox (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  revision INTEGER NOT NULL,
  event_json TEXT NOT NULL,
  published_at_unix_ms INTEGER
)`,
		`CREATE INDEX IF NOT EXISTS connector_market_outbox_pending
ON connector_market_outbox(published_at_unix_ms, sequence)`,
	}
	for _, statement := range statements {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate connector market store: %w", err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `ALTER TABLE connector_market_operations ADD COLUMN lease_token INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return fmt.Errorf("migrate connector market operation lease token: %w", err)
	}
	if err := store.migrateLifecycle(ctx); err != nil {
		return err
	}
	if err := store.migrateOperationOwnership(ctx); err != nil {
		return err
	}
	return store.migrateRuntimeConvergence(ctx)
}

func (store *Store) Snapshot(ctx context.Context) (contracts.Snapshot, error) {
	return store.snapshot(ctx, "")
}

func (store *Store) CatalogView(ctx context.Context) (contracts.CatalogView, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return contracts.CatalogView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	view, err := readCatalogView(ctx, tx)
	if err != nil {
		return contracts.CatalogView{}, err
	}
	if err := tx.Commit(); err != nil {
		return contracts.CatalogView{}, err
	}
	return view, nil
}

func (store *Store) BeginCatalogRefresh(ctx context.Context, _ time.Time) (uint64, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var generation uint64
	if err := tx.QueryRowContext(ctx, `
UPDATE connector_market_catalog_state
SET fetch_generation = fetch_generation + 1, freshness_state = 'refreshing'
WHERE id = ?
RETURNING fetch_generation`, metadataID).Scan(&generation); err != nil {
		return 0, fmt.Errorf("begin connector catalog refresh: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return generation, nil
}

func (store *Store) FailCatalogRefresh(ctx context.Context, generation uint64, failureCode string, now time.Time) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE connector_market_catalog_state
SET freshness_state = CASE WHEN active_snapshot_id IS NULL THEN 'unavailable' ELSE 'stale' END,
    stale_since_unix_ms = CASE
      WHEN active_snapshot_id IS NULL THEN NULL
      ELSE COALESCE(stale_since_unix_ms, ?)
    END,
    last_failure_code = ?
WHERE id = ? AND fetch_generation = ?`, now.UTC().UnixMilli(), strings.TrimSpace(failureCode), metadataID, generation)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.Commit()
	}
	return tx.Commit()
}

// SnapshotForScope scopes private operations but deliberately returns base
// connector state. The application layer is the single owner of account
// authorization and readiness projection.
func (store *Store) SnapshotForScope(ctx context.Context, scope contracts.OperationScope) (contracts.Snapshot, error) {
	return store.snapshot(ctx, strings.TrimSpace(scope.AccountID))
}

func (store *Store) snapshot(ctx context.Context, accountID string) (contracts.Snapshot, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return contracts.Snapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var result contracts.Snapshot
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM connector_market_metadata WHERE id = ?`, metadataID).
		Scan(&result.Revision); err != nil {
		return contracts.Snapshot{}, fmt.Errorf("read connector market metadata: %w", err)
	}
	freshness, err := readCatalogFreshness(ctx, tx)
	if err != nil {
		return contracts.Snapshot{}, err
	}
	result.CatalogFreshness = freshness
	connectors, err := listConnectorsOn(ctx, tx)
	if err != nil {
		return contracts.Snapshot{}, err
	}
	operations, err := listOperationsOn(ctx, tx, accountID)
	if err != nil {
		return contracts.Snapshot{}, err
	}
	result.Connectors = connectors
	result.Operations = operations
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM connector_market_outbox`).Scan(&result.EventCursor); err != nil {
		return contracts.Snapshot{}, fmt.Errorf("read connector market event cursor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return contracts.Snapshot{}, err
	}
	return result, nil
}

type catalogQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readCatalogFreshness(ctx context.Context, queryer catalogQueryer) (contracts.CatalogFreshness, error) {
	var freshness contracts.CatalogFreshness
	var snapshotID sql.NullString
	var sourceRevision sql.NullString
	var acceptedAtMS sql.NullInt64
	var staleSinceMS sql.NullInt64
	if err := queryer.QueryRowContext(ctx, `
SELECT state.freshness_state, state.active_snapshot_id, snapshot.source_revision,
       snapshot.accepted_at_unix_ms, state.stale_since_unix_ms, state.last_failure_code
FROM connector_market_catalog_state state
LEFT JOIN connector_market_catalog_snapshots snapshot ON snapshot.snapshot_id = state.active_snapshot_id
WHERE state.id = ?`, metadataID).Scan(
		&freshness.State, &snapshotID, &sourceRevision, &acceptedAtMS, &staleSinceMS, &freshness.LastFailure,
	); err != nil {
		return contracts.CatalogFreshness{}, fmt.Errorf("read connector catalog freshness: %w", err)
	}
	if snapshotID.Valid {
		freshness.SnapshotID = snapshotID.String
	}
	if sourceRevision.Valid {
		freshness.SourceRevision = sourceRevision.String
	}
	if acceptedAtMS.Valid {
		acceptedAt := time.UnixMilli(acceptedAtMS.Int64).UTC()
		freshness.AcceptedAt = &acceptedAt
	}
	if staleSinceMS.Valid {
		staleSince := time.UnixMilli(staleSinceMS.Int64).UTC()
		freshness.StaleSince = &staleSince
	}
	return freshness, nil
}

func readCatalogView(ctx context.Context, tx *sql.Tx) (contracts.CatalogView, error) {
	freshness, err := readCatalogFreshness(ctx, tx)
	if err != nil {
		return contracts.CatalogView{}, err
	}
	view := contracts.CatalogView{Freshness: freshness, ListingsBySection: make(map[string][]contracts.CatalogListing)}
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM connector_market_metadata WHERE id = ?`, metadataID).Scan(&view.Revision); err != nil {
		return contracts.CatalogView{}, err
	}
	if freshness.SnapshotID == "" {
		view.Categories = []contracts.CatalogCategory{}
		return view, nil
	}
	categoryRows, err := tx.QueryContext(ctx, `
SELECT category_id, kind, sort_order, item_count, display_name_zh, display_name_en
FROM connector_market_catalog_categories
WHERE snapshot_id = ?
ORDER BY sort_order, category_id`, freshness.SnapshotID)
	if err != nil {
		return contracts.CatalogView{}, err
	}
	for categoryRows.Next() {
		var category contracts.CatalogCategory
		if err := categoryRows.Scan(&category.CategoryID, &category.Kind, &category.SortOrder, &category.ItemCount,
			&category.DisplayNameZH, &category.DisplayNameEN); err != nil {
			_ = categoryRows.Close()
			return contracts.CatalogView{}, err
		}
		view.Categories = append(view.Categories, category)
	}
	if err := categoryRows.Close(); err != nil {
		return contracts.CatalogView{}, err
	}
	if err := categoryRows.Err(); err != nil {
		return contracts.CatalogView{}, err
	}
	if view.Categories == nil {
		view.Categories = []contracts.CatalogCategory{}
	}
	listingRows, err := tx.QueryContext(ctx, `
SELECT placement.section_id, placement.category_id, placement.featured, release.release_json, connector.connector_json
FROM connector_market_catalog_placements placement
JOIN connector_market_catalog_releases release
  ON release.snapshot_id = placement.snapshot_id AND release.connector_key = placement.connector_key
LEFT JOIN connector_market_connectors connector ON connector.connector_key = placement.connector_key
WHERE placement.snapshot_id = ?
ORDER BY placement.section_id, placement.placement_order`, freshness.SnapshotID)
	if err != nil {
		return contracts.CatalogView{}, err
	}
	for listingRows.Next() {
		var sectionID string
		var listing contracts.CatalogListing
		var featured int
		var releasePayload string
		var connectorPayload sql.NullString
		if err := listingRows.Scan(&sectionID, &listing.CategoryID, &featured, &releasePayload, &connectorPayload); err != nil {
			_ = listingRows.Close()
			return contracts.CatalogView{}, err
		}
		if err := json.Unmarshal([]byte(releasePayload), &listing.Connector.Release); err != nil {
			_ = listingRows.Close()
			return contracts.CatalogView{}, err
		}
		listing.Connector.Key = listing.Connector.Release.ConnectorKey
		listing.Connector.Installation.State = contracts.InstallationStateNotInstalled
		listing.Connector.Compatibility.State = contracts.CompatibilityStateSupported
		listing.Connector.Authorization.State = contracts.AuthorizationStateDisconnected
		if listing.Connector.Release.Manifest.AuthorizationKind == "none" {
			listing.Connector.Authorization.State = contracts.AuthorizationStateNotRequired
		}
		if connectorPayload.Valid {
			var projection contracts.Connector
			if err := json.Unmarshal([]byte(connectorPayload.String), &projection); err != nil {
				_ = listingRows.Close()
				return contracts.CatalogView{}, err
			}
			listing.Connector.Installation = projection.Installation
			listing.Connector.Authorization = projection.Authorization
			listing.Connector.Compatibility = projection.Compatibility
			listing.Connector.Revision = projection.Revision
		}
		listing.Featured = featured != 0
		view.ListingsBySection[sectionID] = append(view.ListingsBySection[sectionID], listing)
	}
	if err := listingRows.Close(); err != nil {
		return contracts.CatalogView{}, err
	}
	if err := listingRows.Err(); err != nil {
		return contracts.CatalogView{}, err
	}
	return view, nil
}

func (store *Store) Connector(ctx context.Context, connectorKey string) (contracts.Connector, error) {
	var payload string
	if err := store.db.QueryRowContext(ctx, `
SELECT connector_json FROM connector_market_connectors WHERE connector_key = ?`, connectorKey).Scan(&payload); err != nil {
		return contracts.Connector{}, mapNotFound(err)
	}
	connector, err := decodeConnector(payload)
	if err != nil {
		return contracts.Connector{}, err
	}
	return connector, nil
}

func (store *Store) Operation(ctx context.Context, operationID string) (contracts.Operation, error) {
	var payload string
	if err := store.db.QueryRowContext(ctx, `
SELECT operation_json FROM connector_market_operations WHERE operation_id = ?`, operationID).Scan(&payload); err != nil {
		return contracts.Operation{}, mapNotFound(err)
	}
	return decodeOperation(payload)
}

func (store *Store) ClaimOperation(
	ctx context.Context,
	operationID string,
	owner string,
	now time.Time,
	leaseExpiresAt time.Time,
) (contracts.Operation, bool, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return contracts.Operation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE connector_market_operations
SET lease_owner = ?, lease_token = lease_token + 1, lease_expires_at_unix_ms = ?
WHERE operation_id = ?
  AND state IN ('accepted', 'running')
  AND (
    lease_owner = '' OR lease_expires_at_unix_ms IS NULL OR
    lease_expires_at_unix_ms <= ?
  )`, owner, leaseExpiresAt.UTC().UnixMilli(), operationID, now.UTC().UnixMilli())
	if err != nil {
		return contracts.Operation{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return contracts.Operation{}, false, err
	}
	operation, err := operationOn(ctx, tx, operationID)
	if err != nil {
		return contracts.Operation{}, false, err
	}
	if changed == 0 {
		return operation, false, tx.Commit()
	}
	if err := tx.QueryRowContext(ctx, `SELECT lease_token FROM connector_market_operations WHERE operation_id = ?`, operationID).Scan(&operation.LeaseToken); err != nil {
		return contracts.Operation{}, false, err
	}
	expiresAt := leaseExpiresAt.UTC()
	operation.LeaseOwner = owner
	operation.LeaseExpiresAt = &expiresAt
	if err := saveOperationOn(ctx, tx, operation); err != nil {
		return contracts.Operation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return contracts.Operation{}, false, err
	}
	return operation, true, nil
}

func (store *Store) RenewOperationLease(ctx context.Context, operationID, owner string, token uint64, now, leaseExpiresAt time.Time) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	operation, err := operationOn(ctx, tx, operationID)
	if err != nil {
		return err
	}
	var currentOwner string
	var currentToken uint64
	var currentExpiry sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT lease_owner, lease_token, lease_expires_at_unix_ms FROM connector_market_operations WHERE operation_id = ?`, operationID).
		Scan(&currentOwner, &currentToken, &currentExpiry); err != nil {
		return err
	}
	if currentOwner != owner || currentToken != token || !currentExpiry.Valid || currentExpiry.Int64 <= now.UTC().UnixMilli() {
		return contracts.ErrOperationLeaseLost
	}
	expiresAt := leaseExpiresAt.UTC()
	operation.LeaseOwner, operation.LeaseToken, operation.LeaseExpiresAt = owner, token, &expiresAt
	payload, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE connector_market_operations SET lease_expires_at_unix_ms = ?, operation_json = ? WHERE operation_id = ? AND lease_owner = ? AND lease_token = ? AND lease_expires_at_unix_ms > ?`,
		expiresAt.UnixMilli(), string(payload), operationID, owner, token, now.UTC().UnixMilli())
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return contracts.ErrOperationLeaseLost
	}
	return tx.Commit()
}

func (store *Store) ReleaseOperationLease(ctx context.Context, operationID, owner string, token uint64) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
UPDATE connector_market_operations
SET lease_owner = '', lease_expires_at_unix_ms = NULL
WHERE operation_id = ? AND lease_owner = ? AND lease_token = ?`, operationID, owner, token)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.Commit()
	}
	operation, err := operationOn(ctx, tx, operationID)
	if err != nil {
		return err
	}
	operation.LeaseOwner = ""
	operation.LeaseExpiresAt = nil
	if err := saveOperationOn(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) InstalledRelease(ctx context.Context, connectorKey, releaseDigest string) (contracts.Release, error) {
	var payload string
	err := store.db.QueryRowContext(ctx, `
SELECT release_json FROM connector_market_release_installations
WHERE connector_key = ? AND release_digest = ?`, connectorKey, releaseDigest).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		err = store.db.QueryRowContext(ctx, `
SELECT release_json FROM connector_market_installed_releases
WHERE connector_key = ? AND release_digest = ?`, connectorKey, releaseDigest).Scan(&payload)
	}
	if err != nil {
		return contracts.Release{}, mapNotFound(err)
	}
	var release contracts.Release
	if err := json.Unmarshal([]byte(payload), &release); err != nil {
		return contracts.Release{}, fmt.Errorf("decode installed connector release: %w", err)
	}
	return release, nil
}

func (store *Store) InstalledReleases(
	ctx context.Context,
	refs []contracts.InstalledReleaseRef,
) (map[contracts.InstalledReleaseRef]contracts.Release, error) {
	result := make(map[contracts.InstalledReleaseRef]contracts.Release, len(refs))
	if len(refs) == 0 {
		return result, nil
	}
	values := make([]string, 0, len(refs))
	arguments := make([]any, 0, len(refs)*2)
	for _, ref := range refs {
		ref.ConnectorKey = strings.TrimSpace(ref.ConnectorKey)
		ref.ReleaseDigest = strings.TrimSpace(ref.ReleaseDigest)
		if ref.ConnectorKey == "" || ref.ReleaseDigest == "" {
			continue
		}
		values = append(values, "(?, ?)")
		arguments = append(arguments, ref.ConnectorKey, ref.ReleaseDigest)
	}
	if len(values) == 0 {
		return result, nil
	}
	query := `
WITH requested(connector_key, release_digest) AS (VALUES ` + strings.Join(values, ",") + `),
releases AS (
  SELECT requested.connector_key, requested.release_digest, installation.release_json, 2 AS priority
  FROM requested
  JOIN connector_market_release_installations installation USING (connector_key, release_digest)
  UNION ALL
  SELECT requested.connector_key, requested.release_digest, installed.release_json, 1 AS priority
  FROM requested
  JOIN connector_market_installed_releases installed USING (connector_key, release_digest)
)
SELECT connector_key, release_digest, release_json FROM releases ORDER BY priority`
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref contracts.InstalledReleaseRef
		var payload string
		if err := rows.Scan(&ref.ConnectorKey, &ref.ReleaseDigest, &payload); err != nil {
			return nil, err
		}
		var release contracts.Release
		if err := json.Unmarshal([]byte(payload), &release); err != nil {
			return nil, fmt.Errorf("decode installed connector release: %w", err)
		}
		result[ref] = release
	}
	return result, rows.Err()
}

func (store *Store) RecoverableOperations(ctx context.Context) ([]contracts.Operation, error) {
	rows, err := store.db.QueryContext(ctx, `
SELECT operation_json FROM connector_market_operations
WHERE state IN ('accepted', 'running')
ORDER BY operation_id`)
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
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}

func (store *Store) UnresolvedAuthorizationSessionOperations(
	ctx context.Context,
	scope contracts.OperationScope,
) ([]contracts.Operation, error) {
	rows, err := store.db.QueryContext(ctx, `
SELECT operation_json FROM connector_market_operations
WHERE kind = 'start_authorization' AND state = 'completed'
ORDER BY operation_id`)
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
		if operation.Scope.AccountID != scope.AccountID || operation.Execution.AuthorizationSession == nil ||
			operation.Execution.AuthorizationSession.IsResolved() {
			continue
		}
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}

func (store *Store) ResolveAuthorizationSession(
	ctx context.Context,
	operationID string,
	resolution contracts.AuthorizationSessionResolution,
) error {
	if !validAuthorizationSessionResolutionTransition(resolution) {
		return errors.New("valid authorization session resolution is required")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	operation, err := operationOn(ctx, tx, operationID)
	if err != nil {
		return err
	}
	if operation.Execution.AuthorizationSession == nil || operation.Execution.AuthorizationSession.IsResolved() {
		return tx.Commit()
	}
	operation.Execution.AuthorizationSession.Resolution = resolution
	operation.UpdatedAt = time.Now().UTC()
	if err := saveOperationOn(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func validAuthorizationSessionResolutionTransition(resolution contracts.AuthorizationSessionResolution) bool {
	switch resolution {
	case contracts.AuthorizationSessionResolutionCanceling,
		contracts.AuthorizationSessionResolutionProviderConnected,
		contracts.AuthorizationSessionResolutionProviderFailed,
		contracts.AuthorizationSessionResolutionAccountStateConverged,
		contracts.AuthorizationSessionResolutionSuperseded:
		return true
	default:
		return false
	}
}

func (store *Store) Transaction(ctx context.Context, fn func(application.Transaction) error) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM connector_market_metadata WHERE id = ?`, metadataID).Scan(&revision); err != nil {
		return err
	}
	transaction := &transaction{ctx: ctx, tx: tx, revision: revision}
	if err := fn(transaction); err != nil {
		return err
	}
	if transaction.revision != revision {
		result, err := tx.ExecContext(ctx, `
UPDATE connector_market_metadata SET revision = ? WHERE id = ? AND revision = ?`,
			transaction.revision, metadataID, revision)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return errors.New("connector market revision changed during transaction")
		}
	}
	return tx.Commit()
}

func (store *Store) PendingChangedEvents(ctx context.Context, limit int) ([]contracts.ChangedEventRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := store.db.QueryContext(ctx, `
SELECT sequence, event_json FROM connector_market_outbox
WHERE published_at_unix_ms IS NULL ORDER BY sequence LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]contracts.ChangedEventRecord, 0)
	for rows.Next() {
		var entry contracts.ChangedEventRecord
		var payload string
		if err := rows.Scan(&entry.Sequence, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &entry.Event); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (store *Store) MarkChangedEventPublished(ctx context.Context, sequence int64, publishedAt time.Time) error {
	_, err := store.db.ExecContext(ctx, `
UPDATE connector_market_outbox
SET published_at_unix_ms = COALESCE(published_at_unix_ms, ?)
WHERE sequence = ?`, publishedAt.UTC().UnixMilli(), sequence)
	return err
}

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
