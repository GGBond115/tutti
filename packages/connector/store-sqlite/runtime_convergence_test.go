package storesqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func TestRuntimeConvergencePersistsAndReconcilesPerBootEpoch(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "tuttid.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 2, 0, 0, 0, time.UTC)
	scope := contracts.OperationScope{AccountID: "account-1"}
	desired := runtimeConvergenceFixture(scope, "github", 1, now)
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		return tx.SaveRuntimeConvergence(desired)
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	due, err := store.DueRuntimeConvergences(ctx, scope, "boot-1", now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Desired.Generation != 1 {
		t.Fatalf("due convergence = %#v", due)
	}
	claimed, ok, err := store.ClaimRuntimeConvergence(
		ctx, scope, "github", "boot-1", "worker-1", now, now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || claimed.LeaseToken == 0 {
		t.Fatalf("claimed convergence = %#v, claimed = %v", claimed, ok)
	}
	observed := contracts.RuntimeObserved{
		DesiredGeneration: 1,
		BootEpoch:         "boot-1",
		Enabled:           true,
		ConnectionID:      desired.Desired.ConnectionID,
		ReleaseDigest:     desired.Desired.ReleaseDigest,
		Readiness:         contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady},
		ObservedAt:        now.Add(time.Second),
	}
	if err := store.CompleteRuntimeConvergence(
		ctx, scope, "github", "worker-1", claimed.LeaseToken, 1, observed, now.Add(time.Second),
	); err != nil {
		t.Fatal(err)
	}
	due, err = store.DueRuntimeConvergences(ctx, scope, "boot-1", now.Add(2*time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("same-boot convergence remained due: %#v", due)
	}
	due, err = store.DueRuntimeConvergences(ctx, scope, "boot-2", now.Add(2*time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("new boot due convergence = %#v", due)
	}
}

func TestRuntimeConvergenceRejectsStaleGenerationCompletion(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 15, 3, 0, 0, 0, time.UTC)
	scope := contracts.OperationScope{AccountID: "account-1"}
	first := runtimeConvergenceFixture(scope, "github", 1, now)
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		return tx.SaveRuntimeConvergence(first)
	}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimRuntimeConvergence(
		ctx, scope, "github", "boot-1", "worker-1", now, now.Add(time.Minute),
	)
	if err != nil || !ok {
		t.Fatalf("claim = %#v, %v, %v", claimed, ok, err)
	}
	second := first
	second.Desired.Generation = 2
	second.Desired.Enabled = false
	second.Desired.UpdatedAt = now.Add(time.Second)
	second.NextAttemptAt = now.Add(time.Second)
	second.LeaseOwner = ""
	second.LeaseToken = claimed.LeaseToken + 1
	second.LeaseExpiresAt = nil
	second.UpdatedAt = now.Add(time.Second)
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		return tx.SaveRuntimeConvergence(second)
	}); err != nil {
		t.Fatal(err)
	}
	err = store.CompleteRuntimeConvergence(ctx, scope, "github", "worker-1", claimed.LeaseToken, 1,
		contracts.RuntimeObserved{DesiredGeneration: 1, BootEpoch: "boot-1"}, now.Add(2*time.Second))
	if !errors.Is(err, contracts.ErrOperationLeaseLost) {
		t.Fatalf("stale completion error = %v", err)
	}
	stored, err := store.RuntimeConvergence(ctx, scope, "github")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Desired.Generation != 2 || stored.Observed.DesiredGeneration != 0 {
		t.Fatalf("stored convergence = %#v", stored)
	}
}

func TestRuntimeConvergencesReadsOneOrderedBoundedScope(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 18, 4, 0, 0, 0, time.UTC)
	targetScope := contracts.OperationScope{AccountID: "account-1"}
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		for _, fixture := range []contracts.RuntimeConvergence{
			runtimeConvergenceFixture(targetScope, "notion", 1, now),
			runtimeConvergenceFixture(contracts.OperationScope{AccountID: "account-2"}, "ignored", 1, now),
			runtimeConvergenceFixture(targetScope, "github", 1, now),
		} {
			if err := tx.SaveRuntimeConvergence(fixture); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	convergences, err := store.RuntimeConvergences(ctx, targetScope, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(convergences) != 1 || convergences[0].Desired.ConnectorKey != "github" {
		t.Fatalf("bounded scoped convergences = %#v", convergences)
	}
}

func TestRuntimeConvergenceFailureBudgetPersistsAndSuppressesClaims(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "tuttid.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 18, 5, 0, 0, 0, time.UTC)
	scope := contracts.OperationScope{AccountID: "account-1"}
	fixture := runtimeConvergenceFixture(scope, "github", 1, now)
	if err := store.Transaction(ctx, func(tx application.Transaction) error { return tx.SaveRuntimeConvergence(fixture) }); err != nil {
		t.Fatal(err)
	}
	for attempt := uint32(1); attempt <= contracts.RuntimeFailureBudget; attempt++ {
		claimed, ok, claimErr := store.ClaimRuntimeConvergence(ctx, scope, "github", "boot-1", "worker-1", now, now.Add(time.Minute))
		if claimErr != nil || !ok {
			t.Fatalf("claim attempt %d = %#v, %t, %v", attempt, claimed, ok, claimErr)
		}
		nextAttemptAt := now.Add(time.Duration(attempt) * time.Second)
		if err := store.RetryRuntimeConvergence(ctx, scope, "github", "worker-1", claimed.LeaseToken, 1,
			nextAttemptAt, "unavailable", "failed", now); err != nil {
			t.Fatal(err)
		}
		now = nextAttemptAt
	}
	stored, err := store.RuntimeConvergence(ctx, scope, "github")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Attempt != contracts.RuntimeFailureBudget || stored.Observed.Readiness.State != contracts.RuntimeReadinessFailed ||
		stored.Observed.Readiness.ReasonCode != contracts.RuntimeReadinessReasonFailureBudgetExhausted {
		t.Fatalf("exhausted convergence = %#v", stored)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	due, err := store.DueRuntimeConvergences(ctx, scope, "boot-1", now, 10)
	if err != nil || len(due) != 0 {
		t.Fatalf("persisted exhausted generation due = %#v, %v", due, err)
	}
	if _, claimed, err := store.ClaimRuntimeConvergence(ctx, scope, "github", "boot-1", "worker-2", now, now.Add(time.Minute)); err != nil || claimed {
		t.Fatalf("persisted exhausted generation claim = %t, %v", claimed, err)
	}
}

func TestRuntimeConvergenceLeaseCannotBeReenteredBySameWorker(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 15, 4, 0, 0, 0, time.UTC)
	scope := contracts.OperationScope{AccountID: "account-1"}
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		return tx.SaveRuntimeConvergence(runtimeConvergenceFixture(scope, "github", 1, now))
	}); err != nil {
		t.Fatal(err)
	}
	first, claimed, err := store.ClaimRuntimeConvergence(
		ctx, scope, "github", "boot-1", "worker-1", now, now.Add(time.Minute),
	)
	if err != nil || !claimed {
		t.Fatalf("first claim = %#v, %v, %v", first, claimed, err)
	}
	second, claimed, err := store.ClaimRuntimeConvergence(
		ctx, scope, "github", "boot-1", "worker-1", now.Add(time.Second), now.Add(time.Minute),
	)
	if err != nil || claimed {
		t.Fatalf("reentrant claim = %#v, %v, %v", second, claimed, err)
	}
	if second.LeaseToken != first.LeaseToken || second.LeaseOwner != first.LeaseOwner {
		t.Fatalf("lease changed after rejected reentry: first=%#v second=%#v", first, second)
	}
}

func TestSnapshotHidesLegacyRuntimeReconcileOperations(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 15, 5, 0, 0, 0, time.UTC)
	operation := contracts.Operation{
		OperationID: "runtime-legacy", ClientRequestID: "runtime-legacy", ConnectorKey: "github",
		Kind: contracts.OperationKindReconcileRuntime, State: contracts.OperationStateCompleted,
		Stage: contracts.OperationStageCompleted, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Transaction(ctx, func(tx application.Transaction) error { return tx.SaveOperation(operation) }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Operations) != 0 {
		t.Fatalf("public snapshot leaked private operations: %#v", snapshot.Operations)
	}
	if _, err := store.Operation(ctx, operation.OperationID); err != nil {
		t.Fatalf("legacy recovery record was deleted: %v", err)
	}
}

func runtimeConvergenceFixture(
	scope contracts.OperationScope,
	connectorKey string,
	generation uint64,
	now time.Time,
) contracts.RuntimeConvergence {
	return contracts.RuntimeConvergence{
		Desired: contracts.RuntimeDesired{
			Scope: scope, ConnectorKey: connectorKey, Generation: generation, Enabled: true,
			ConnectionID: "account-connection", ReleaseDigest: "sha256:release", AuthorizationState: contracts.AuthorizationStateConnected,
			UpdatedAt: now,
		},
		NextAttemptAt: now,
		UpdatedAt:     now,
	}
}
