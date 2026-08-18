package daemon

import (
	"context"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	marketdata "github.com/tutti-os/tutti/packages/connector/store-sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeConvergenceWorkerAppliesDurableDesiredState(t *testing.T) {
	ctx := context.Background()
	store, err := marketdata.Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	release := hostTestRelease()
	connector := contracts.Connector{
		Key: release.ConnectorKey, Release: release,
		Installation: contracts.Installation{
			State: contracts.InstallationStateInstalled, InstalledVersion: release.Version,
			InstalledReleaseID: release.ReleaseID, InstalledReleaseDigest: release.ReleaseDigest,
		},
		Authorization: contracts.Authorization{State: contracts.AuthorizationStateNotRequired},
		Compatibility: contracts.Compatibility{State: contracts.CompatibilityStateSupported},
	}
	now := time.Date(2026, 8, 15, 4, 0, 0, 0, time.UTC)
	install := contracts.Operation{
		OperationID: "install-1", ClientRequestID: "install-request-1", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindInstall, State: contracts.OperationStateCompleted, Stage: contracts.OperationStageCompleted,
		Target: &contracts.OperationTarget{
			ConnectorKey: connector.Key, Version: release.Version, ReleaseID: release.ReleaseID,
			ReleaseDigest: release.ReleaseDigest, Release: &release,
		},
		CreatedAt: now, UpdatedAt: now,
	}
	desired := contracts.RuntimeConvergence{
		Desired: contracts.RuntimeDesired{
			ConnectorKey: connector.Key, Generation: 1, Enabled: true,
			ConnectionID: "device-github", ReleaseDigest: release.ReleaseDigest,
			AuthorizationState: contracts.AuthorizationStateNotRequired, UpdatedAt: now,
		},
		NextAttemptAt: now,
		UpdatedAt:     now,
	}
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		connector.Revision = tx.AdvanceRevision()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(install); err != nil {
			return err
		}
		return tx.SaveRuntimeConvergence(desired)
	}); err != nil {
		t.Fatal(err)
	}

	runtime := &activationGateDelegate{}
	gate := newActivationGateHost(runtime)
	gate.setOpen(contracts.OperationScope{}, true)
	scheduler := NewOperationScheduler(ctx)
	composition, err := application.New(application.Config{
		Repository: store, CatalogSource: &countingCatalogSource{release: release},
		ReleaseInstallations: runtime, ImplementationCommands: gate, Authorization: unavailableAuthorization{},
		RuntimeBindings: runtimeBindingResolverFunc(func(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
			return contracts.RuntimeBinding{
				ConnectionID: "device-github", Enabled: true, AuthorizationState: contracts.AuthorizationStateNotRequired,
			}, nil
		}),
		Compatibility: rejectingCompatibility{}, Scheduler: scheduler,
		ImplementationRegistry: application.NewImplementationRegistry(nil),
		Now:                    func() time.Time { return now },
		BootEpoch:              "boot-1",
		WorkerID:               "worker-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Bind(composition.Daemon.Recovery); err != nil {
		t.Fatal(err)
	}
	host := &Host{runtimeMaintenance: composition.Daemon.Runtime, bootstrapped: true}
	if err := host.convergeDueRuntimes(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := store.RuntimeConvergence(ctx, contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.reconciles != 1 || stored.Observed.DesiredGeneration != 1 ||
		stored.Observed.BootEpoch != "boot-1" || stored.Observed.Readiness.State != contracts.RuntimeReadinessReady {
		t.Fatalf("runtime reconciles = %d, convergence = %#v", runtime.reconciles, stored)
	}
}
