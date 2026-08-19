package storesqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	market "github.com/tutti-os/tutti/packages/connector/daemon/core"
)

func TestStoreContractDiscardLeavesAuthorizationAndRevision(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "tuttid.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	connector := testConnector()
	connector.Release.Manifest.IconURL = "data:image/png;base64,iVBORw0KGgo="
	connector.Installation = market.Installation{
		State: market.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	now := time.Unix(1, 0).UTC()
	if err := store.Transaction(ctx, func(tx market.Transaction) error {
		connector.Revision = tx.AdvanceRevision()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(market.ChangedEvent{ConnectorKey: connector.Key, Revision: connector.Revision}); err != nil {
			return err
		}
		return tx.SaveOperation(market.Operation{
			OperationID: "refresh-stale", ClientRequestID: "refresh-stale", ConnectorKey: "",
			Kind: market.OperationKindRefreshCatalog, State: market.OperationStateAccepted,
			CreatedAt: now, UpdatedAt: now,
		})
	}); err != nil {
		t.Fatal(err)
	}
	releasePayload, err := json.Marshal(connector.Release)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO connector_market_installed_releases (connector_key, release_digest, release_json)
VALUES (?, ?, ?)`, connector.Key, connector.Release.ReleaseDigest, string(releasePayload)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO connector_market_release_installations (connector_key, release_digest, release_json)
VALUES (?, ?, ?)`, connector.Key, connector.Release.ReleaseDigest, string(releasePayload)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAuthorizationProjection(ctx, market.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, State: market.AuthorizationStateConnected,
		ServerSynchronized: true, ServerRevision: 7, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO connector_market_authorization_snapshot_revisions (account_id, revision) VALUES (?, ?)
ON CONFLICT(account_id) DO UPDATE SET revision = excluded.revision`, "account-1", 7); err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision == 0 {
		t.Fatal("expected a preserved metadata revision")
	}
	if _, err := store.db.ExecContext(ctx, `
UPDATE connector_market_metadata
SET store_contract = 0, catalog_state = ?, source_revision = 'old-source'
WHERE id = ?`, market.CatalogStateReady, metadataID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertDiscardedStoreContract(t, reopened, before.Revision)

	again, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	assertDiscardedStoreContract(t, again, before.Revision)
	if err := again.Transaction(ctx, func(tx market.Transaction) error {
		return tx.SaveOperation(market.Operation{
			OperationID: "refresh-new", ClientRequestID: "refresh-new", ConnectorKey: "",
			Kind: market.OperationKindRefreshCatalog, State: market.OperationStateAccepted,
			CreatedAt: now, UpdatedAt: now,
		})
	}); err != nil {
		t.Fatalf("new catalog refresh blocked after contract discard: %v", err)
	}
}

func TestStoreContractIdempotentWhenCurrent(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "tuttid.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	connector := testConnector()
	if err := store.Transaction(ctx, func(tx market.Transaction) error {
		connector.Revision = tx.AdvanceRevision()
		return tx.SaveConnector(connector)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	snapshot, err := reopened.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Connectors) != 1 || snapshot.Connectors[0].Key != connector.Key {
		t.Fatalf("current-contract connectors = %#v", snapshot.Connectors)
	}
	if snapshot.CatalogState != market.CatalogStateStale {
		t.Fatalf("catalog state = %q", snapshot.CatalogState)
	}
}

func assertDiscardedStoreContract(t *testing.T, store *Store, wantRevision uint64) {
	t.Helper()
	ctx := context.Background()
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != wantRevision {
		t.Fatalf("revision = %d, want %d", snapshot.Revision, wantRevision)
	}
	if snapshot.CatalogState != market.CatalogStateStale || snapshot.SourceRevision != "" {
		t.Fatalf("catalog = %#v", snapshot)
	}
	if len(snapshot.Connectors) != 0 || len(snapshot.Operations) != 0 {
		t.Fatalf("discarded snapshot = %#v", snapshot)
	}
	if _, err := store.InstalledRelease(ctx, "github", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, market.ErrNotFound) {
		t.Fatalf("installed release after discard = %v", err)
	}
	var contract int
	if err := store.db.QueryRowContext(ctx, `SELECT store_contract FROM connector_market_metadata WHERE id = 1`).Scan(&contract); err != nil {
		t.Fatal(err)
	}
	if contract != currentStoreContract {
		t.Fatalf("store_contract = %d, want %d", contract, currentStoreContract)
	}
	projection, err := store.AuthorizationProjection(ctx, "account-1", "github")
	if err != nil {
		t.Fatal(err)
	}
	if projection.State != market.AuthorizationStateConnected || projection.ServerRevision != 7 {
		t.Fatalf("authorization projection = %#v", projection)
	}
	var snapshotRevision uint64
	if err := store.db.QueryRowContext(ctx, `SELECT revision FROM connector_market_authorization_snapshot_revisions WHERE account_id = ?`, "account-1").Scan(&snapshotRevision); err != nil {
		t.Fatal(err)
	}
	if snapshotRevision != 7 {
		t.Fatalf("authorization snapshot revision = %d, want 7", snapshotRevision)
	}
}
