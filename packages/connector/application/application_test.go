package application

import (
	"context"
	"errors"
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestApplicationInstallIsDurableAndIdempotent(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	command := contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	}

	accepted, err := application.Install(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Connector == nil || accepted.Connector.Installation.State != contracts.InstallationStateInstalling {
		t.Fatalf("connector = %#v", accepted.Connector)
	}
	if accepted.Operation.State != contracts.OperationStateAccepted || accepted.Revision != 1 {
		t.Fatalf("result = %#v", accepted)
	}
	retried, err := application.Install(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Operation.OperationID != accepted.Operation.OperationID {
		t.Fatalf("retry operation = %q, want %q", retried.Operation.OperationID, accepted.Operation.OperationID)
	}
	if repository.revision != 1 {
		t.Fatalf("revision = %d, want 1", repository.revision)
	}
	if len(scheduler.operationIDs) != 2 {
		t.Fatalf("scheduled operations = %#v", scheduler.operationIDs)
	}
}

func TestApplicationConnectorRevisionFenceAllowsIndependentConcurrentCommands(t *testing.T) {
	alpha := testConnector("alpha")
	beta := testConnector("beta")
	repository := newMemoryRepository(alpha, beta)
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	alphaRevision := alpha.Revision
	betaRevision := beta.Revision

	first, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "install-alpha", ExpectedRevision: 0}, ConnectorKey: alpha.Key,
		ExpectedConnectorRevision: &alphaRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Install(context.Background(), contracts.ConnectorMutation{
		// The global revision is intentionally stale after alpha was accepted.
		Mutation: contracts.Mutation{ClientRequestID: "install-beta", ExpectedRevision: 0}, ConnectorKey: beta.Key,
		ExpectedConnectorRevision: &betaRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Operation.OperationID == second.Operation.OperationID || len(scheduler.operationIDs) != 2 {
		t.Fatalf("independent operations = %#v, %#v; scheduled = %#v", first.Operation, second.Operation, scheduler.operationIDs)
	}
}

func TestApplicationRepairInstallClearsInvalidInstalledEvidence(t *testing.T) {
	for _, failureCode := range []string{
		InstallationFailureCodePhysicallyAbsent,
		InstallationFailureCodePhysicallyInvalid,
	} {
		t.Run(failureCode, func(t *testing.T) {
			connector := testConnector("github")
			connector.Installation = contracts.Installation{
				State:                  contracts.InstallationStateFailed,
				InstalledVersion:       connector.Release.Version,
				InstalledReleaseID:     connector.Release.ReleaseID,
				InstalledReleaseDigest: connector.Release.ReleaseDigest,
				FailureCode:            failureCode,
			}
			repository := newMemoryRepository(connector)
			application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

			accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
				Mutation:     contracts.Mutation{ClientRequestID: "repair-" + failureCode, ExpectedRevision: 0},
				ConnectorKey: connector.Key,
			})
			if err != nil {
				t.Fatal(err)
			}
			if accepted.Connector == nil {
				t.Fatal("accepted repair omitted Connector projection")
			}
			installation := accepted.Connector.Installation
			if installation.State != contracts.InstallationStateInstalling ||
				installation.InstalledVersion != "" ||
				installation.InstalledReleaseID != "" ||
				installation.InstalledReleaseDigest != "" ||
				installation.FailureCode != "" {
				t.Fatalf("accepted repair installation = %#v", installation)
			}
		})
	}
}

func TestApplicationUpdateInstallRetainsUsableInstalledEvidence(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{
		State:                  contracts.InstallationStateInstalled,
		InstalledVersion:       "0.9.0",
		InstalledReleaseID:     "github@0.9.0",
		InstalledReleaseDigest: strings.Repeat("a", 64),
	}
	repository := newMemoryRepository(connector)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "update-github", ExpectedRevision: 0},
		ConnectorKey: connector.Key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Connector == nil {
		t.Fatal("accepted update omitted Connector projection")
	}
	installation := accepted.Connector.Installation
	if installation.State != contracts.InstallationStateUpdating ||
		installation.InstalledVersion != connector.Installation.InstalledVersion ||
		installation.InstalledReleaseID != connector.Installation.InstalledReleaseID ||
		installation.InstalledReleaseDigest != connector.Installation.InstalledReleaseDigest {
		t.Fatalf("accepted update installation = %#v", installation)
	}
}

func TestApplicationClientRequestIDIsReusableOnlyAfterTerminalRetention(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	command := contracts.ConnectorMutation{Mutation: contracts.Mutation{ClientRequestID: "request-retained", ExpectedRevision: 0}, ConnectorKey: "github"}
	accepted, err := application.Install(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), scheduler.operationIDs[len(scheduler.operationIDs)-1]); err != nil {
		t.Fatal(err)
	}
	retried, err := application.Install(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Operation.OperationID != accepted.Operation.OperationID || retried.Operation.State != contracts.OperationStateCompleted {
		t.Fatalf("retained retry = %#v, want completed operation %q", retried.Operation, accepted.Operation.OperationID)
	}

	// Lifecycle cleanup removes the idempotency key with its terminal result.
	// A caller reusing that key after the documented window starts a new
	// operation and must provide the current revision like any fresh command.
	delete(repository.operations, accepted.Operation.OperationID)
	command.ExpectedRevision = repository.revision
	afterRetention, err := application.Install(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if afterRetention.Operation.OperationID == accepted.Operation.OperationID || afterRetention.Operation.State != contracts.OperationStateAccepted {
		t.Fatalf("post-retention operation = %#v", afterRetention.Operation)
	}
}

func TestApplicationExecutesAcceptedInstall(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	scheduler := &memoryScheduler{}
	installationHost := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, scheduler, installationHost, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	installed, err := repository.Connector(context.Background(), "github")
	if err != nil {
		t.Fatal(err)
	}
	operation, err := repository.Operation(context.Background(), accepted.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Installation.State != contracts.InstallationStateInstalled || installed.Installation.InstalledVersion != "1.0.0" {
		t.Fatalf("installation = %#v", installed.Installation)
	}
	if operation.State != contracts.OperationStateCompleted || installationHost.prepares != 1 || installationHost.reconciles != 1 {
		t.Fatalf("operation = %#v, prepares = %d, reconciles = %d", operation, installationHost.prepares, installationHost.reconciles)
	}
	convergence, err := repository.RuntimeConvergence(context.Background(), contracts.OperationScope{}, "github")
	if err != nil {
		t.Fatal(err)
	}
	if convergence.Desired.Generation == 0 || !convergence.Desired.Enabled ||
		convergence.Desired.ReleaseDigest != installed.Installation.InstalledReleaseDigest ||
		convergence.Observed.DesiredGeneration != convergence.Desired.Generation ||
		convergence.Observed.BootEpoch != application.config.BootEpoch {
		t.Fatalf("post-install runtime convergence = %#v", convergence)
	}
}

func TestApplicationDoesNotProjectInstalledBeforePhysicalCommit(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	scheduler := &memoryScheduler{}
	physicalCommitErr := errors.New("physical commit unavailable")
	installationHost := &memoryInstallRuntime{installationCommitErr: physicalCommitErr}
	application := newTestApplication(t, repository, scheduler, installationHost, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "request-physical-commit", ExpectedRevision: 0}, ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err == nil {
		t.Fatal("physical commit failure was accepted")
	}
	connector, err := repository.Connector(context.Background(), "github")
	if err != nil {
		t.Fatal(err)
	}
	if connector.Installation.State == contracts.InstallationStateInstalled {
		t.Fatalf("installation projected before physical commit: %#v", connector.Installation)
	}
}

func TestApplicationExecutesTypedCLIInstallationBeforeCompletion(t *testing.T) {
	connector := testConnector("lark")
	connector.Release.Manifest.SchemaVersion = "1"
	connector.Release.Manifest.Implementation.ManagedStdio.MCP = nil
	connector.Release.Manifest.Implementation.ManagedStdio.Runtime.VersionRange = ">=22.0.0 <23.0.0"
	connector.Release.Manifest.Implementation.ManagedStdio.Runtime.VersionRange = ">=22.0.0 <23.0.0"
	connector.Release.Manifest.Implementation.ManagedStdio.CLI = &contracts.ManagedCLIInterface{Entrypoint: "lark-cli", TimeoutMS: 120_000,
		Install: &contracts.CLIInstallation{Kind: "node_package", NodePackage: &contracts.NodePackageInstallation{Package: "@larksuite/cli",
			Version: "1.0.83", Integrity: "sha512-qbJYoJtNch6dV8RvYBO2wpcKO9+6Io3Cuf5alYFzvLbtkSntOKqoc+xHI7p6wRq4oH4F9fydgNJbTGy79ibPdg==",
			Launch: contracts.NodePackageLaunch{Kind: "native", Entrypoint: "bin/lark-cli", SHA256: strings.Repeat("c", 64)}}}}
	repository := newMemoryRepository(connector)
	host := &memoryInstallRuntime{}
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, host, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{Mutation: contracts.Mutation{ClientRequestID: "install-lark"},
		ConnectorKey: "lark"})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	operation, err := repository.Operation(context.Background(), accepted.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if host.cliInstalls != 1 || operation.Execution.ReleaseInstallation == nil ||
		operation.Execution.ReleaseInstallation.CLIInstallation == nil || operation.State != contracts.OperationStateCompleted {
		t.Fatalf("CLI installs = %d, operation = %#v", host.cliInstalls, operation)
	}
}

func TestApplicationLocalUninstallRemovesDeviceReleaseWithoutDisconnectingAuthorization(t *testing.T) {
	connector := testConnector("lark")
	connector.Release.Manifest.AuthorizationKind = "oauth2"
	connector.Release.Manifest.Implementation.ManagedStdio.MCP = nil
	connector.Release.Manifest.Implementation.ManagedStdio.Runtime.VersionRange = ">=22.0.0 <23.0.0"
	connector.Release.Manifest.Implementation.ManagedStdio.CLI = &contracts.ManagedCLIInterface{Entrypoint: "lark-cli", TimeoutMS: 120_000,
		Install: &contracts.CLIInstallation{Kind: "node_package", NodePackage: &contracts.NodePackageInstallation{
			Package: "@larksuite/cli", Version: "1.0.83",
			Integrity: "sha512-qbJYoJtNch6dV8RvYBO2wpcKO9+6Io3Cuf5alYFzvLbtkSntOKqoc+xHI7p6wRq4oH4F9fydgNJbTGy79ibPdg==",
			Launch:    contracts.NodePackageLaunch{Kind: "native", Entrypoint: "bin/lark-cli", SHA256: strings.Repeat("c", 64)},
		}},
	}
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest}
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateConnected}
	repository := newMemoryRepository(connector)
	runtime := &memoryInstallRuntime{}
	provider := &countingAuthorizationProvider{}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	application.config.Authorization = provider

	accepted, err := application.Uninstall(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "uninstall-lark", ExpectedRevision: 0}, ConnectorKey: connector.Key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Connector(context.Background(), connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Installation.State != contracts.InstallationStateNotInstalled || stored.Installation.InstalledReleaseDigest != "" {
		t.Fatalf("installation = %#v", stored.Installation)
	}
	if stored.Authorization.State != contracts.AuthorizationStateConnected {
		t.Fatalf("local uninstall changed authorization = %#v", stored.Authorization)
	}
	if runtime.reconciles != 1 || runtime.lastReconcile.Enabled || runtime.removes != 1 || runtime.cliRemoves != 1 {
		t.Fatalf("cleanup counts: reconcile=%d enabled=%t artifact=%d cli=%d",
			runtime.reconciles, runtime.lastReconcile.Enabled, runtime.removes, runtime.cliRemoves)
	}
	if provider.disconnects != 0 {
		t.Fatalf("authorization disconnects = %d, want 0", provider.disconnects)
	}
}

func TestCrossMachineReceiptsUseOpaqueReferences(t *testing.T) {
	release := testReleaseWithImplementation("lark", "1.0.0", contracts.ImplementationKindManagedStdio)
	release.Manifest.Implementation.ManagedStdio.MCP = nil
	release.Manifest.Implementation.ManagedStdio.CLI = &contracts.ManagedCLIInterface{Entrypoint: "lark-cli", Install: &contracts.CLIInstallation{
		Kind: "node_package", NodePackage: &contracts.NodePackageInstallation{Package: "@larksuite/cli", Version: "1.0.83",
			Integrity: "sha512-qbJYoJtNch6dV8RvYBO2wpcKO9+6Io3Cuf5alYFzvLbtkSntOKqoc+xHI7p6wRq4oH4F9fydgNJbTGy79ibPdg==",
			Launch:    contracts.NodePackageLaunch{Kind: "native", Entrypoint: "bin/lark-cli", SHA256: strings.Repeat("c", 64)}},
	}}
	operation := contracts.Operation{OperationID: "operation-1", ConnectorKey: "lark"}
	prepared := contracts.PreparedArtifactReceipt{OperationID: operation.OperationID, ConnectorKey: "lark", Version: release.Version,
		ReleaseDigest: release.ReleaseDigest, ArtifactSHA256: release.Artifact.SHA256,
		InventoryDigest: strings.Repeat("e", 64), OpaqueArtifactRef: "guest-artifact-1"}
	if err := validatePreparedArtifact(operation, release, prepared); err != nil {
		t.Fatal(err)
	}
	install := releaseCLIInstallation(release)
	installed := contracts.CLIInstallationReceipt{SchemaVersion: "tutti.connector.cli-installation.v1", OperationID: operation.OperationID,
		ConnectorKey: "lark", ReleaseDigest: release.ReleaseDigest, RuntimeProfile: "connector-node-static",
		RuntimeABI: "node24-linux-arm64", NodeVersion: "24.18.0", NodeSHA256: strings.Repeat("1", 64),
		Package: install.Package, PackageVersion: install.Version, PackageIntegrity: install.Integrity,
		LaunchKind: install.Launch.Kind, Entrypoint: "node_modules/@larksuite/cli/bin/lark-cli",
		EntrypointSHA256: strings.Repeat("2", 64), EntrypointSize: 7, OpaqueInstallationRef: "guest-install-1"}
	receipt := contracts.ReleaseInstallationReceipt{OperationID: operation.OperationID, ConnectorKey: release.ConnectorKey,
		Version: release.Version, ReleaseID: release.ReleaseID, ReleaseDigest: release.ReleaseDigest,
		ArtifactSHA256: release.Artifact.SHA256, Artifact: prepared, CLIInstallation: &installed,
		OpaqueRuntimeRef: "guest-runtime-1"}
	if err := validateReleaseInstallationReceipt(operation, release, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationReconcilesInstalledRuntimeAtStartup(t *testing.T) {
	connector := testConnector("github")
	connector.Revision = 7
	connector.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(connector)
	host := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})

	if err := application.ReconcileInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host.reconciles != 1 || host.lastReconcile.ConnectionID != defaultConnectorConnectionID ||
		host.lastReconcile.Generation.Generation != 8 || host.lastReconcile.Generation.BootEpoch == "" {
		t.Fatalf("startup reconcile = %#v, count=%d", host.lastReconcile, host.reconciles)
	}
	convergence, err := repository.RuntimeConvergence(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if convergence.Desired.Generation != 8 || convergence.Observed.DesiredGeneration != 8 ||
		convergence.Observed.BootEpoch != application.config.BootEpoch ||
		host.lastReconcile.Generation.Generation != convergence.Desired.Generation {
		t.Fatalf("startup runtime convergence = %#v, request = %#v", convergence, host.lastReconcile)
	}
	for _, operation := range repository.operations {
		if operation.Kind == contracts.OperationKindReconcileRuntime {
			t.Fatalf("startup leaked private runtime operation: %#v", operation)
		}
	}
}

func TestApplicationStartupReconcileAcceptsDisabledRuntimeWithoutBlockingEnabledRuntime(t *testing.T) {
	disabled := testConnector("dingtalk")
	disabled.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: disabled.Release.Version,
		InstalledReleaseID: disabled.Release.ReleaseID, InstalledReleaseDigest: disabled.Release.ReleaseDigest,
	}
	enabled := testConnector("lark")
	enabled.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: enabled.Release.Version,
		InstalledReleaseID: enabled.Release.ReleaseID, InstalledReleaseDigest: enabled.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(disabled, enabled)
	host := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})
	application.config.RuntimeBindings = runtimeBindingResolverFunc(func(_ context.Context, request contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
		return contracts.RuntimeBinding{
			ConnectionID: request.Connector.Key + "-connection",
			Enabled:      request.Connector.Key == enabled.Key,
		}, nil
	})

	if err := application.ReconcileInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(host.reconcileRequests) != 2 {
		t.Fatalf("runtime reconciles = %#v", host.reconcileRequests)
	}
	if host.reconcileRequests[0].Connector.Key != disabled.Key || host.reconcileRequests[0].Enabled ||
		host.reconcileRequests[1].Connector.Key != enabled.Key || !host.reconcileRequests[1].Enabled {
		t.Fatalf("runtime reconciles = %#v", host.reconcileRequests)
	}
}

func TestApplicationStartupReconcileContinuesAfterConnectorFailure(t *testing.T) {
	failing := testConnector("dingtalk")
	failing.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: failing.Release.Version,
		InstalledReleaseID: failing.Release.ReleaseID, InstalledReleaseDigest: failing.Release.ReleaseDigest,
	}
	healthy := testConnector("lark")
	healthy.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: healthy.Release.Version,
		InstalledReleaseID: healthy.Release.ReleaseID, InstalledReleaseDigest: healthy.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(failing, healthy)
	host := &memoryInstallRuntime{reconcileErrors: map[string]error{failing.Key: errors.New("runtime unavailable")}}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})

	err := application.ReconcileInstalledRuntimes(context.Background())
	var failures *RuntimeReconcileFailures
	if !errors.As(err, &failures) {
		t.Fatalf("startup reconcile error = %v", err)
	}
	failedKeys := failures.ConnectorKeys()
	if len(failedKeys) != 1 || failedKeys[0] != failing.Key {
		t.Fatalf("failed connector keys = %#v", failedKeys)
	}
	if len(host.reconcileRequests) != 2 || host.reconcileRequests[1].Connector.Key != healthy.Key {
		t.Fatalf("runtime reconciles = %#v", host.reconcileRequests)
	}
}

func TestValidateRuntimeReceiptRequiresExactDisabledReadiness(t *testing.T) {
	generation := contracts.HostGeneration{BootEpoch: "boot-1", Generation: 1}
	base := contracts.RuntimeReceipt{
		OperationID: "operation-1", ConnectionID: "connection-1", ConnectorKey: "dingtalk",
		ReleaseDigest: strings.Repeat("a", 64), Generation: generation,
	}
	tests := []struct {
		name      string
		readiness contracts.RuntimeReadiness
		wantError bool
	}{
		{name: "disabled", readiness: contracts.RuntimeReadiness{
			State: contracts.RuntimeReadinessBlocked, ReasonCode: contracts.RuntimeReadinessReasonRuntimeDisabled}},
		{name: "ready", readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady,
			Interfaces: []contracts.InterfaceReadiness{{Kind: "mcp", State: contracts.RuntimeReadinessReady}}}, wantError: true},
		{name: "unrelated block", readiness: contracts.RuntimeReadiness{
			State: contracts.RuntimeReadinessBlocked, ReasonCode: "publication_gate_closed"}, wantError: true},
		{name: "disabled with published interface", readiness: contracts.RuntimeReadiness{
			State: contracts.RuntimeReadinessBlocked, ReasonCode: contracts.RuntimeReadinessReasonRuntimeDisabled,
			Interfaces: []contracts.InterfaceReadiness{{Kind: "mcp", State: contracts.RuntimeReadinessReady}}}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipt := base
			receipt.Readiness = test.readiness
			err := validateRuntimeReceipt(receipt, base.OperationID, base.ConnectionID, base.ConnectorKey,
				base.ReleaseDigest, generation, false)
			if (err != nil) != test.wantError {
				t.Fatalf("validateRuntimeReceipt() error = %v, wantError = %t", err, test.wantError)
			}
		})
	}
}

func TestApplicationInstallCompletesAfterRuntimeIsObserved(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	host := &memoryInstallRuntime{}
	resolver := &runtimeBindingResolverStub{binding: contracts.RuntimeBinding{ConnectionID: "account-connection", Enabled: false}}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})
	application.config.RuntimeBindings = resolver

	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "install-account", ExpectedRevision: 0},
		ConnectorKey: "github", AccountID: "account-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	operation := repository.operations[accepted.Operation.OperationID]
	if operation.Scope.AccountID != "account-1" || host.lastPrepare.Scope.AccountID != "account-1" ||
		host.lastPrepare.Generation != operation.HostGeneration || host.reconciles != 1 ||
		host.lastReconcile.Generation.BootEpoch != application.config.BootEpoch {
		t.Fatalf("operation=%#v prepare=%#v reconcile=%#v", operation, host.lastPrepare, host.lastReconcile)
	}
	if repository.connectors["github"].Installation.State != contracts.InstallationStateInstalled {
		t.Fatalf("installation = %#v", repository.connectors["github"].Installation)
	}
}

func TestApplicationCredentialGrantIsNotPersistedAndIsCleared(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	host := &memoryInstallRuntime{}
	grant := []byte("one-shot-grant")
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, host, contracts.CatalogSnapshot{})
	application.config.RuntimeBindings = runtimeBindingResolverFunc(func(_ context.Context, request contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
		binding := contracts.RuntimeBinding{ConnectionID: "account-connection", Enabled: true}
		if request.Purpose == contracts.RuntimeBindingPurposeReconcile {
			binding.CredentialBrokerGrant = grant
		}
		return binding, nil
	})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "install-grant"}, ConnectorKey: "github", AccountID: "account-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if err := application.ConvergeRuntime(context.Background(), contracts.OperationScope{AccountID: "account-1"}, "github"); err != nil {
		t.Fatal(err)
	}
	if host.lastCredentialGrant != "one-shot-grant" {
		t.Fatalf("runtime grant = %q", host.lastCredentialGrant)
	}
	if string(grant) != strings.Repeat("\x00", len(grant)) {
		t.Fatalf("credential grant was not cleared: %v", grant)
	}
	payload := fmt.Sprintf("%#v", repository.operations[accepted.Operation.OperationID])
	if strings.Contains(payload, "one-shot-grant") {
		t.Fatalf("operation persisted credential authority: %s", payload)
	}
}

func TestApplicationReconcileRuntimeKeepsDeviceInstallationTruth(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest}
	repository := newMemoryRepository(connector)
	host := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})
	application.config.RuntimeBindings = &runtimeBindingResolverStub{binding: contracts.RuntimeBinding{ConnectionID: "account-connection", Enabled: false}}
	accepted, err := application.ReconcileRuntime(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "reconcile-account"}, ConnectorKey: "github", AccountID: "account-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	stored := repository.connectors["github"]
	if stored.Installation.State != contracts.InstallationStateInstalled || stored.Installation.InstalledReleaseDigest != connector.Release.ReleaseDigest ||
		host.lastReconcile.Enabled {
		t.Fatalf("connector=%#v reconcile=%#v", stored, host.lastReconcile)
	}
}

func TestApplicationAuthorizationObservationReconcilesWithoutChangingInstallation(t *testing.T) {
	connector := testConnector("github")
	connector.Release.Manifest.AuthorizationKind = "oauth2"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest}
	repository := newMemoryRepository(connector)
	host := &memoryInstallRuntime{}
	projections := &recordingAuthorizationProjectionStore{}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})
	application.config.AuthorizationProjections = projections
	application.config.RuntimeBindings = AccountRuntimeBindingResolver{
		Projections: projections, Credentials: &credentialGrantIssuerStub{grant: []byte("credential-grant")},
	}
	connected := contracts.AuthorizationProjection{AccountID: "account-1", ConnectorKey: "github",
		ConnectionID: "server-connection", State: contracts.AuthorizationStateConnected}
	accepted, err := application.ObserveAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "authorization-connected"}, ConnectorKey: "github", AccountID: "account-1",
	}, connected)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if !host.lastReconcile.Enabled || host.lastReconcile.ConnectionID != "server-connection" ||
		host.lastCredentialGrant != "credential-grant" {
		t.Fatalf("connected reconcile = %#v", host.lastReconcile)
	}
	expired := connected
	expired.State = contracts.AuthorizationStateExpired
	accepted, err = application.ObserveAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-expired", ExpectedRevision: repository.revision},
		ConnectorKey: "github", AccountID: "account-1",
	}, expired)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if host.lastReconcile.Enabled || repository.connectors["github"].Installation.State != contracts.InstallationStateInstalled {
		t.Fatalf("expired reconcile = %#v connector = %#v", host.lastReconcile, repository.connectors["github"])
	}
}

func TestApplicationStartupReconcileAdvancesPastFence(t *testing.T) {
	connector := testConnector("github")
	connector.Revision = 7
	connector.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(connector)
	host := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})

	if err := application.FenceInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := application.ReconcileInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host.lastDeactivation.Generation.Generation != 7 || host.lastReconcile.Generation.Generation != 8 || repository.connectors["github"].Revision < 8 {
		t.Fatalf("startup generations: fence=%#v reconcile=%#v connectorRevision=%d",
			host.lastDeactivation.Generation, host.lastReconcile.Generation, repository.connectors["github"].Revision)
	}
	firstReconcileGeneration := host.lastReconcile.Generation.Generation
	if err := application.FenceInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := application.ReconcileInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host.lastDeactivation.Generation.Generation < firstReconcileGeneration ||
		host.lastReconcile.Generation.Generation <= host.lastDeactivation.Generation.Generation {
		t.Fatalf("repeated startup generations: fence=%#v reconcile=%#v", host.lastDeactivation.Generation, host.lastReconcile.Generation)
	}
}

func TestApplicationStartupFenceFallsBackToConnectorIdentityWhenReleaseEvidenceIsMissing(t *testing.T) {
	connector := testConnector("lark-cli")
	installedRelease := connector.Release
	connector.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: installedRelease.Version,
		InstalledReleaseID: installedRelease.ReleaseID, InstalledReleaseDigest: installedRelease.ReleaseDigest,
	}
	connector.Release.Version = "2.0.0"
	connector.Release.ReleaseID = connector.Key + "@2.0.0"
	connector.Release.ReleaseDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	connector.Release.ManifestDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	repository := newMemoryRepository(connector)
	host := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})

	if err := application.FenceInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host.deactivations != 1 || !host.lastDeactivation.AllConnections ||
		host.lastDeactivation.ConnectorKey != connector.Key ||
		host.lastDeactivation.ReleaseDigest != installedRelease.ReleaseDigest {
		t.Fatalf("fallback deactivation = %#v", host.lastDeactivation)
	}
}

func TestApplicationCrossDeviceRemoteReconcileUsesAccountProjectionAuthorization(t *testing.T) {
	connector := testConnector("tencent-docs")
	connector.Release.Manifest.AuthorizationKind = "api_key"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Release.Manifest.Implementation = contracts.Implementation{Kind: contracts.ImplementationKindRemoteStreamableHTTP, RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
		ProtocolVersion: "2026-07-28", BindingRef: "tencent-docs.primary", ContractVersion: 1,
		BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest}
	repository := newMemoryRepository(connector)
	runtime := &memoryInstallRuntime{}
	projectionStore := &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, ConnectionID: "server-connection", State: contracts.AuthorizationStateConnected,
		ServerSynchronized: true,
	}}
	readiness := NewAuthorizationReadinessGate()
	readiness.SetReady("account-1", true)
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	application.config.AuthorizationProjections = projectionStore
	application.config.RuntimeBindings = AccountRuntimeBindingResolver{Projections: projectionStore, Readiness: readiness}

	if err := application.ReconcileInstalledRuntimesForScope(context.Background(), contracts.OperationScope{AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if !runtime.lastReconcile.Enabled || runtime.lastReconcile.Connector.Authorization.State != contracts.AuthorizationStateConnected ||
		runtime.lastReconcile.ConnectionID != contracts.AccountRuntimeConnectionID("account-1", connector.Key) {
		t.Fatalf("remote reconcile = %#v", runtime.lastReconcile)
	}
	if repository.connectors[connector.Key].Authorization.State != contracts.AuthorizationStateDisconnected {
		t.Fatalf("device installation authorization was mutated: %#v", repository.connectors[connector.Key].Authorization)
	}
}

func TestApplicationLocalUninstallKeepsRemoteProjectionAndReusesItAfterReinstall(t *testing.T) {
	connector := testConnector("tencent-docs")
	connector.Release.Manifest.AuthorizationKind = "api_key"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Release.Manifest.Implementation = contracts.Implementation{Kind: contracts.ImplementationKindRemoteStreamableHTTP,
		RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
			ProtocolVersion: "2026-07-28", BindingRef: "tencent-docs.primary", ContractVersion: 1,
			BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}}
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest}
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateConnected}
	repository := newMemoryRepository(connector)
	runtime := &memoryInstallRuntime{}
	projection := contracts.AuthorizationProjection{AccountID: "account-1", ConnectorKey: connector.Key,
		ConnectionID: "server-connection", State: contracts.AuthorizationStateConnected, ServerSynchronized: true}
	projectionStore := &authorizationProjectionStoreStub{projection: projection}
	readiness := NewAuthorizationReadinessGate()
	readiness.SetReady("account-1", true)
	provider := &countingAuthorizationProvider{}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	application.config.Authorization = provider
	application.config.AuthorizationProjections = projectionStore
	application.config.RuntimeBindings = AccountRuntimeBindingResolver{Projections: projectionStore, Readiness: readiness}
	application.config.ImplementationRegistry = NewImplementationRegistry(map[string]ImplementationValidator{
		contracts.ImplementationKindRemoteStreamableHTTP: nil,
	})

	uninstall, err := application.Uninstall(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "uninstall-tencent-docs", ExpectedRevision: 0},
		ConnectorKey: connector.Key, AccountID: "account-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), uninstall.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if runtime.lastReconcile.ConnectionID != contracts.AccountRuntimeConnectionID("account-1", connector.Key) || runtime.lastReconcile.Enabled {
		t.Fatalf("disabled runtime reconcile = %#v", runtime.lastReconcile)
	}
	if projectionStore.projection != projection {
		t.Fatalf("authorization projection changed = %#v, want %#v", projectionStore.projection, projection)
	}
	if provider.disconnects != 0 {
		t.Fatalf("authorization disconnects = %d, want 0", provider.disconnects)
	}
	if err := application.ReconcileRemoteAuthorizedRuntimesForScope(context.Background(), contracts.OperationScope{AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if runtime.reconciles != 1 {
		t.Fatalf("uninstalled connector reconciles = %d, want only durable disable", runtime.reconciles)
	}

	snapshot, err := application.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	install, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "reinstall-tencent-docs", ExpectedRevision: snapshot.Revision},
		ConnectorKey: connector.Key, AccountID: "account-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), install.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if err := application.ReconcileInstalledRuntimesForScope(context.Background(), contracts.OperationScope{AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	if runtime.reconciles != 3 || !runtime.lastReconcile.Enabled ||
		runtime.lastReconcile.Connector.Authorization.State != contracts.AuthorizationStateConnected {
		t.Fatalf("reinstalled runtime reconcile = %#v, count=%d", runtime.lastReconcile, runtime.reconciles)
	}
}

func TestApplicationRemoteAuthorizationStartPreservesPendingBeforeProjectionConverges(t *testing.T) {
	connector := testConnector("tencent-docs")
	connector.Release.Manifest.AuthorizationKind = "oauth2"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Release.Manifest.Implementation = contracts.Implementation{
		Kind: contracts.ImplementationKindRemoteStreamableHTTP,
		RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
			ProtocolVersion: "2026-07-28", BindingRef: "tencent-docs.primary", ContractVersion: 1,
			BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	// This device field may have been written while another account was active.
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateConnected}
	repository := newMemoryRepository(connector)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.AuthorizationProjections = &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-new", ConnectorKey: connector.Key, State: contracts.AuthorizationStateDisconnected,
		ServerSynchronized: true,
	}}

	result, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-new-account"},
		ConnectorKey: connector.Key, AccountID: "account-new",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationURL == "" || result.Connector.Authorization.State != contracts.AuthorizationStatePending ||
		repository.connectors[connector.Key].Authorization.State != contracts.AuthorizationStateConnected {
		t.Fatalf("result=%#v device authorization=%#v", result, repository.connectors[connector.Key].Authorization)
	}
	receipt := repository.operations[result.Operation.OperationID].Execution.AuthorizationSession
	if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionUnresolved {
		t.Fatalf("receipt = %#v", receipt)
	}
	wantExpiry := application.config.Now().UTC().Add(10 * time.Minute)
	if !result.AuthorizationExpiresAt.Equal(wantExpiry) || !receipt.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("authorization expiry result=%s receipt=%s want=%s", result.AuthorizationExpiresAt, receipt.ExpiresAt, wantExpiry)
	}
	operationCount := len(repository.operations)
	_, err = application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-after-renderer-reload"},
		ConnectorKey: connector.Key, AccountID: "account-new",
	}, nil)
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeOperationInProgress {
		t.Fatalf("second unresolved authorization error = %v", err)
	}
	if len(repository.operations) != operationCount {
		t.Fatalf("second unresolved authorization created another receipt: %#v", repository.operations)
	}
	currentRevision := repository.connectors[connector.Key].Revision
	cancelCommand := contracts.CancelAuthorizationCommand{
		ConnectorMutation: contracts.ConnectorMutation{
			Mutation:     contracts.Mutation{ClientRequestID: "cancel-authorization", ExpectedRevision: repository.revision},
			ConnectorKey: connector.Key, AccountID: "account-new", ExpectedConnectorRevision: &currentRevision,
		},
		OperationID: result.Operation.OperationID,
	}
	canceled := commandFacades{service: application}.CancelAuthorization(context.Background(), cancelCommand)
	if canceled.Outcome != contracts.CommandCompleted {
		t.Fatalf("cancel result = %#v", canceled)
	}
	restarted := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	replayedCancel := commandFacades{service: restarted}.CancelAuthorization(context.Background(), cancelCommand)
	if replayedCancel.Outcome != contracts.CommandCompleted || replayedCancel.Revision != canceled.Revision {
		t.Fatalf("replayed cancel result = %#v, first = %#v", replayedCancel, canceled)
	}
	if receipt.Resolution != contracts.AuthorizationSessionResolutionSuperseded {
		t.Fatalf("canceled receipt = %#v", receipt)
	}
	newAttempt, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-after-cancel", ExpectedRevision: repository.revision},
		ConnectorKey: connector.Key, AccountID: "account-new",
	}, nil)
	if err != nil {
		t.Fatalf("authorization retry after cancel: %v", err)
	}
	staleCancel := cancelCommand
	staleCancel.ClientRequestID = "cancel-stale-operation"
	staleCancel.ExpectedRevision = repository.revision
	currentRevision = repository.connectors[connector.Key].Revision
	staleCancel.ExpectedConnectorRevision = &currentRevision
	rejected := commandFacades{service: application}.CancelAuthorization(context.Background(), staleCancel)
	if rejected.Outcome != contracts.CommandRejected || rejected.Failure == nil ||
		rejected.Failure.Code != contracts.ErrorCodeRevisionConflict || rejected.Failure.Retryable {
		t.Fatalf("stale cancel result = %#v", rejected)
	}
	newReceipt := repository.operations[newAttempt.Operation.OperationID].Execution.AuthorizationSession
	if newReceipt == nil || newReceipt.Resolution != contracts.AuthorizationSessionResolutionUnresolved {
		t.Fatalf("newer attempt was canceled by stale command: %#v", newReceipt)
	}
}

func TestApplicationCancellationReplayResumesDurableFence(t *testing.T) {
	for _, resolution := range []contracts.AuthorizationSessionResolution{
		contracts.AuthorizationSessionResolutionUnresolved,
		contracts.AuthorizationSessionResolutionCanceling,
	} {
		t.Run(string(resolution), func(t *testing.T) {
			connector := testManagedAuthorizedConnector("resume-cancel-" + string(resolution))
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
			connector.Revision = 7
			repository := newMemoryRepository(connector)
			repository.revision = connector.Revision
			operation := contracts.Operation{
				OperationID: "authorization-resume", ClientRequestID: "authorization-start",
				ConnectorKey: connector.Key, Kind: contracts.OperationKindStartAuthorization,
				Scope: contracts.OperationScope{AccountID: "account-1"}, State: contracts.OperationStateCompleted,
				Stage:  contracts.OperationStageCompleted,
				Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
				Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
					OperationID: "authorization-resume", ConnectorKey: connector.Key,
					SessionID: "session-resume", State: contracts.AuthorizationStatePending,
					Resolution: resolution, CancellationClientRequestID: "cancel-request",
				}},
			}
			repository.operations[operation.OperationID] = operation
			provider := &cancelingAuthorizationProvider{}
			application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
			application.config.Authorization = provider
			staleConnectorRevision := uint64(1)
			result := commandFacades{service: application}.CancelAuthorization(context.Background(), contracts.CancelAuthorizationCommand{
				ConnectorMutation: contracts.ConnectorMutation{
					Mutation:     contracts.Mutation{ClientRequestID: "cancel-request", ExpectedRevision: 1},
					ConnectorKey: connector.Key, AccountID: "account-1", ExpectedConnectorRevision: &staleConnectorRevision,
				},
				OperationID: operation.OperationID,
			})
			if result.Outcome != contracts.CommandCompleted || result.Revision != 8 {
				t.Fatalf("cancel replay result = %#v", result)
			}
			if len(provider.canceled) != 1 || provider.canceled[0] != operation.OperationID {
				t.Fatalf("provider cancellations = %#v", provider.canceled)
			}
			receipt := repository.operations[operation.OperationID].Execution.AuthorizationSession
			if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionSuperseded ||
				receipt.CancellationClientRequestID != "cancel-request" {
				t.Fatalf("resumed receipt = %#v", receipt)
			}
			if current := repository.connectors[connector.Key]; current.Authorization.State != contracts.AuthorizationStateDisconnected {
				t.Fatalf("connector after resumed cancel = %#v", current.Authorization)
			}
		})
	}
}

func TestApplicationCancellationReplayFinishesProjectionAfterTerminalReceipt(t *testing.T) {
	connector := testManagedAuthorizedConnector("terminal-cancel-replay")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	connector.Revision = 4
	repository := newMemoryRepository(connector)
	repository.revision = connector.Revision
	operation := contracts.Operation{
		OperationID: "authorization-terminal", ClientRequestID: "authorization-start",
		ConnectorKey: connector.Key, Kind: contracts.OperationKindStartAuthorization,
		Scope: contracts.OperationScope{AccountID: "account-1"}, State: contracts.OperationStateCompleted,
		Stage:  contracts.OperationStageCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-terminal", ConnectorKey: connector.Key,
			SessionID: "session-terminal", State: contracts.AuthorizationStatePending,
			Resolution:                  contracts.AuthorizationSessionResolutionSuperseded,
			CancellationClientRequestID: "cancel-request",
		}},
	}
	repository.operations[operation.OperationID] = operation
	provider := &cancelingAuthorizationProvider{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider
	staleConnectorRevision := uint64(1)
	command := contracts.CancelAuthorizationCommand{
		ConnectorMutation: contracts.ConnectorMutation{
			Mutation:     contracts.Mutation{ClientRequestID: "cancel-request", ExpectedRevision: 1},
			ConnectorKey: connector.Key, AccountID: "account-1", ExpectedConnectorRevision: &staleConnectorRevision,
		},
		OperationID: operation.OperationID,
	}
	first := commandFacades{service: application}.CancelAuthorization(context.Background(), command)
	second := commandFacades{service: application}.CancelAuthorization(context.Background(), command)
	if first.Outcome != contracts.CommandCompleted || second.Outcome != contracts.CommandCompleted ||
		first.Revision != 5 || second.Revision != first.Revision {
		t.Fatalf("terminal cancel replays = first %#v second %#v", first, second)
	}
	if len(provider.canceled) != 0 {
		t.Fatalf("terminal replay called provider: %#v", provider.canceled)
	}
	if current := repository.connectors[connector.Key]; current.Authorization.State != contracts.AuthorizationStateDisconnected {
		t.Fatalf("connector after terminal cancel replay = %#v", current.Authorization)
	}
}

func TestApplicationCancellationRecoveryKeepsReceiptVisibleUntilProjectionReset(t *testing.T) {
	connector := testManagedAuthorizedConnector("cancel-reset-crash")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	repository := newMemoryRepository(connector)
	operation := contracts.Operation{
		OperationID: "authorization-reset-crash", ClientRequestID: "authorization-start",
		ConnectorKey: connector.Key, Kind: contracts.OperationKindStartAuthorization,
		Scope: contracts.OperationScope{AccountID: "account-1"}, State: contracts.OperationStateCompleted,
		Stage:  contracts.OperationStageCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-reset-crash", ConnectorKey: connector.Key,
			SessionID: "session-reset-crash", State: contracts.AuthorizationStatePending,
			Resolution: contracts.AuthorizationSessionResolutionUnresolved,
		}},
	}
	repository.operations[operation.OperationID] = operation
	repository.failTransactionCall = 3 // revision check + cancellation fence commit; projection reset fails
	repository.failTransactionErr = errors.New("injected projection reset failure")
	provider := &cancelingAuthorizationProvider{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider
	connectorRevision := connector.Revision
	_, err := application.cancelAuthorizationCommand(context.Background(), contracts.CancelAuthorizationCommand{
		ConnectorMutation: contracts.ConnectorMutation{
			Mutation:     contracts.Mutation{ClientRequestID: "cancel-request", ExpectedRevision: repository.revision},
			ConnectorKey: connector.Key, AccountID: "account-1", ExpectedConnectorRevision: &connectorRevision,
		},
		OperationID: operation.OperationID,
	})
	if err == nil || err.Error() != "injected projection reset failure" {
		t.Fatalf("cancel reset failure = %v", err)
	}
	receipt := repository.operations[operation.OperationID].Execution.AuthorizationSession
	if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionCanceling ||
		receipt.CancellationClientRequestID != "cancel-request" {
		t.Fatalf("receipt escaped recovery query after reset failure = %#v", receipt)
	}
	if current := repository.connectors[connector.Key]; current.Authorization.State != contracts.AuthorizationStatePending {
		t.Fatalf("failed reset changed connector = %#v", current.Authorization)
	}
	if len(provider.canceled) != 1 {
		t.Fatalf("provider calls before recovery = %#v", provider.canceled)
	}

	intents, err := application.ReconcileAuthorizations(context.Background(), operation.Scope)
	if err != nil || len(intents) != 0 {
		t.Fatalf("cancel recovery = intents %#v error %v", intents, err)
	}
	receipt = repository.operations[operation.OperationID].Execution.AuthorizationSession
	if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionSuperseded {
		t.Fatalf("recovered receipt = %#v", receipt)
	}
	if current := repository.connectors[connector.Key]; current.Authorization.State != contracts.AuthorizationStateDisconnected {
		t.Fatalf("recovered connector = %#v", current.Authorization)
	}
	if len(provider.canceled) != 2 {
		t.Fatalf("provider recovery calls = %#v", provider.canceled)
	}
}

func TestApplicationCancellationFenceRejectsDifferentRequestWithoutProviderCall(t *testing.T) {
	connector := testManagedAuthorizedConnector("different-cancel-request")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	repository := newMemoryRepository(connector)
	operation := contracts.Operation{
		OperationID: "authorization-fenced", ClientRequestID: "authorization-start",
		ConnectorKey: connector.Key, Kind: contracts.OperationKindStartAuthorization,
		Scope: contracts.OperationScope{AccountID: "account-1"}, State: contracts.OperationStateCompleted,
		Stage:  contracts.OperationStageCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-fenced", ConnectorKey: connector.Key,
			SessionID: "session-fenced", State: contracts.AuthorizationStatePending,
			Resolution:                  contracts.AuthorizationSessionResolutionUnresolved,
			CancellationClientRequestID: "original-cancel-request",
		}},
	}
	repository.operations[operation.OperationID] = operation
	provider := &cancelingAuthorizationProvider{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider
	connectorRevision := connector.Revision
	result := commandFacades{service: application}.CancelAuthorization(context.Background(), contracts.CancelAuthorizationCommand{
		ConnectorMutation: contracts.ConnectorMutation{
			Mutation:     contracts.Mutation{ClientRequestID: "different-cancel-request", ExpectedRevision: repository.revision},
			ConnectorKey: connector.Key, AccountID: "account-1", ExpectedConnectorRevision: &connectorRevision,
		},
		OperationID: operation.OperationID,
	})
	if result.Outcome != contracts.CommandRejected || result.Failure == nil ||
		result.Failure.Code != contracts.ErrorCodeRevisionConflict || result.Failure.Retryable {
		t.Fatalf("different cancellation result = %#v", result)
	}
	if len(provider.canceled) != 0 {
		t.Fatalf("different cancellation called provider: %#v", provider.canceled)
	}
	receipt := repository.operations[operation.OperationID].Execution.AuthorizationSession
	if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionUnresolved ||
		receipt.CancellationClientRequestID != "original-cancel-request" {
		t.Fatalf("different cancellation changed receipt = %#v", receipt)
	}
}

func TestApplicationAuthorizationStartProviderFailureIsTerminal(t *testing.T) {
	connector := testConnector("notion")
	connector.Release.Manifest.AuthorizationKind = "oauth2"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Release.Manifest.Implementation = contracts.Implementation{
		Kind: contracts.ImplementationKindRemoteStreamableHTTP,
		RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
			ProtocolVersion: "2026-07-28", BindingRef: "notion.nango", ContractVersion: 1,
			BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	repository := newMemoryRepository(connector)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	provider := &failingAuthorizationProvider{err: errors.New("status 503")}
	application.config.Authorization = provider
	application.config.AuthorizationProjections = &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, State: contracts.AuthorizationStateDisconnected,
		ServerSynchronized: true,
	}}

	_, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-provider-unavailable"},
		ConnectorKey: connector.Key, AccountID: "account-1",
	}, nil)
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeAuthorizationFailed || !domainError.Retryable {
		t.Fatalf("authorization error = %#v", err)
	}
	var operation *contracts.Operation
	for _, candidate := range repository.operations {
		if candidate.ClientRequestID == "authorization-provider-unavailable" {
			copy := candidate
			operation = &copy
			break
		}
	}
	if operation == nil || operation.State != contracts.OperationStateFailed || operation.Stage != contracts.OperationStageFailed {
		t.Fatalf("authorization operation = %#v", operation)
	}
	if err := application.ExecuteOperation(context.Background(), operation.OperationID); err != nil {
		t.Fatalf("terminal authorization recovery = %v", err)
	}
	if provider.begins.Load() != 1 {
		t.Fatalf("authorization provider calls = %d, want 1", provider.begins.Load())
	}
}

func TestApplicationSerializesConcurrentAuthorizationReceiptCreation(t *testing.T) {
	connector := testConnector("tencent-docs")
	connector.Release.Manifest.AuthorizationKind = "oauth2"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Release.Manifest.Implementation = contracts.Implementation{
		Kind: contracts.ImplementationKindRemoteStreamableHTTP,
		RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
			ProtocolVersion: "2026-07-28", BindingRef: "tencent-docs.primary", ContractVersion: 1,
			BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	repository := newMemoryRepository(connector)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	provider := &blockingAuthorizationProvider{started: make(chan struct{}), release: make(chan struct{})}
	application.config.Authorization = provider
	application.config.AuthorizationProjections = &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, State: contracts.AuthorizationStateDisconnected,
		ServerSynchronized: true,
	}}

	type result struct {
		err error
	}
	results := make(chan result, 2)
	start := func(requestID string) {
		_, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
			Mutation: contracts.Mutation{ClientRequestID: requestID}, ConnectorKey: connector.Key, AccountID: "account-1",
		}, nil)
		results <- result{err: err}
	}
	go start("authorization-1")
	<-provider.started
	go start("authorization-2")
	close(provider.release)
	first, second := <-results, <-results
	var successCount, busyCount int
	for _, result := range []result{first, second} {
		if result.err == nil {
			successCount++
			continue
		}
		var domainError *contracts.DomainError
		if errors.As(result.err, &domainError) && domainError.Code == contracts.ErrorCodeOperationInProgress {
			busyCount++
		}
	}
	if successCount != 1 || busyCount != 1 || provider.begins.Load() != 1 {
		t.Fatalf("authorization results = %#v, %#v; provider begins = %d", first, second, provider.begins.Load())
	}
}

func TestApplicationReplaceAuthorizationInterruptsStuckInitialBegin(t *testing.T) {
	connector := testManagedAuthorizedConnector("dingtalk-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	repository := newMemoryRepository(connector)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	provider := &interruptibleAuthorizationProvider{
		started: make(chan struct{}), canceled: make(chan struct{}),
	}
	application.config.Authorization = provider
	application.config.AuthorizationProjections = &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, State: contracts.AuthorizationStateDisconnected,
	}}

	firstDone := make(chan error, 1)
	go func() {
		_, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
			Mutation: contracts.Mutation{ClientRequestID: "authorization-a"}, ConnectorKey: connector.Key, AccountID: "account-1",
		}, nil)
		firstDone <- err
	}()
	<-provider.started

	second, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "authorization-b"}, ConnectorKey: connector.Key, AccountID: "account-1",
		ReplacementPolicy: contracts.AuthorizationReplacementPolicyReplaceActive,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstErr := <-firstDone; !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("first authorization error = %v, want canceled", firstErr)
	}
	select {
	case <-provider.canceled:
	default:
		t.Fatal("stuck initial authorization was not interrupted")
	}
	if provider.begins.Load() != 2 || second.Operation.ClientRequestID != "authorization-b" ||
		second.Connector.Authorization.State != contracts.AuthorizationStatePending {
		t.Fatalf("replacement result=%#v begins=%d", second, provider.begins.Load())
	}
	var firstOperation contracts.Operation
	for _, operation := range repository.operations {
		if operation.ClientRequestID == "authorization-a" {
			firstOperation = operation
			break
		}
	}
	if firstOperation.State != contracts.OperationStateFailed {
		t.Fatalf("first operation = %#v, want failed", firstOperation)
	}
}

func TestApplicationReplaceAuthorizationCancelsReceiptBeforeStartingNewAttempt(t *testing.T) {
	connector := testManagedAuthorizedConnector("dingtalk-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	repository := newMemoryRepository(connector)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	provider := &cancelingAuthorizationProvider{}
	application.config.Authorization = provider
	application.config.AuthorizationProjections = &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, State: contracts.AuthorizationStateDisconnected,
	}}

	first, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "authorization-a"}, ConnectorKey: connector.Key, AccountID: "account-1",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-b", ExpectedRevision: repository.revision},
		ConnectorKey: connector.Key, AccountID: "account-1",
		ReplacementPolicy: contracts.AuthorizationReplacementPolicyReplaceActive,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstReceipt := repository.operations[first.Operation.OperationID].Execution.AuthorizationSession
	if firstReceipt == nil || firstReceipt.Resolution != contracts.AuthorizationSessionResolutionSuperseded {
		t.Fatalf("first receipt = %#v, want superseded", firstReceipt)
	}
	if len(provider.canceled) != 1 || provider.canceled[0] != first.Operation.OperationID {
		t.Fatalf("provider cancellations = %#v", provider.canceled)
	}
	if provider.begins.Load() != 2 || second.Operation.OperationID == first.Operation.OperationID ||
		second.Operation.ClientRequestID != "authorization-b" ||
		provider.lastReplacementPolicy != contracts.AuthorizationReplacementPolicyReplaceActive {
		t.Fatalf("first=%#v second=%#v begins=%d", first.Operation, second.Operation, provider.begins.Load())
	}
}

func TestApplicationDisconnectAuthorizationCompletesOnlyAfterRuntimeIsDisabled(t *testing.T) {
	connector := testManagedAuthorizedConnector("lark-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateConnected}
	repository := newMemoryRepository(connector)
	runtimeFailure := errors.New("runtime temporarily unavailable")
	runtime := &memoryInstallRuntime{reconcileErrors: map[string]error{connector.Key: runtimeFailure}}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	provider := &countingAuthorizationProvider{}
	projections := &recordingAuthorizationProjectionStore{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, ConnectionID: "connection-1",
		State: contracts.AuthorizationStateConnected,
	}}
	application.config.Authorization = provider
	application.config.AuthorizationProjections = projections
	application.config.RuntimeBindings = AccountRuntimeBindingResolver{Projections: projections}

	accepted, err := application.DisconnectAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "disconnect-lark"}, ConnectorKey: connector.Key, AccountID: "account-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); !errors.Is(err, runtimeFailure) {
		t.Fatalf("first disconnect error = %v", err)
	}
	operation, err := application.GetOperation(context.Background(), accepted.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != contracts.OperationStateRunning || operation.Stage != contracts.OperationStageDisconnecting {
		t.Fatalf("disconnect completed before runtime was disabled: %#v", operation)
	}

	delete(runtime.reconcileErrors, connector.Key)
	application.config.Now = func() time.Time { return time.Date(2026, 8, 3, 0, 1, 0, 0, time.UTC) }
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	operation, err = application.GetOperation(context.Background(), accepted.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != contracts.OperationStateCompleted || runtime.lastReconcile.Enabled {
		t.Fatalf("completed disconnect = %#v, runtime request = %#v", operation, runtime.lastReconcile)
	}
	if provider.disconnects != 2 {
		t.Fatalf("idempotent disconnect attempts = %d, want 2", provider.disconnects)
	}
}

func TestApplicationManagedAuthorizationStartRepairsMissingAccountProjection(t *testing.T) {
	connector := testManagedAuthorizedConnector("lark-cli")
	// This device state predates account-scoped authorization projections.
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateConnected}
	repository := newMemoryRepository(connector)
	projections := &recordingAuthorizationProjectionStore{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = connectedAuthorizationProviderStub{}
	application.config.AuthorizationProjections = projections

	result, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-bind-existing-login"},
		ConnectorKey: connector.Key, AccountID: "account-1",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Connector.Authorization.State != contracts.AuthorizationStateConnected ||
		projections.projection.State != contracts.AuthorizationStateConnected ||
		projections.projection.AccountID != "account-1" {
		t.Fatalf("result=%#v projection=%#v", result, projections.projection)
	}
	if repository.connectors[connector.Key].Authorization.State != contracts.AuthorizationStateConnected {
		t.Fatalf("device authorization = %#v", repository.connectors[connector.Key].Authorization)
	}
}

func TestApplicationScopesPublicOperationReadsToOwnerAccount(t *testing.T) {
	repository := newMemoryRepository()
	now := time.Unix(1, 0).UTC()
	repository.operations["operation-a"] = contracts.Operation{
		OperationID: "operation-a", ClientRequestID: "request-a", ConnectorKey: "github",
		Kind: contracts.OperationKindInstall, Scope: contracts.OperationScope{AccountID: "account-a"},
		State: contracts.OperationStateFailed, CreatedAt: now, UpdatedAt: now,
	}
	repository.operations["operation-private"] = contracts.Operation{
		OperationID: "operation-private", ClientRequestID: "request-private", ConnectorKey: "github",
		Kind: contracts.OperationKindReconcileRuntime, Scope: contracts.OperationScope{AccountID: "account-a"},
		State: contracts.OperationStateFailed, CreatedAt: now, UpdatedAt: now,
	}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

	operation, err := application.GetOperationForScope(context.Background(), contracts.OperationScope{AccountID: "account-a"}, "operation-a")
	if err != nil || operation.OperationID != "operation-a" {
		t.Fatalf("owner operation = %#v, error = %v", operation, err)
	}
	for _, test := range []struct {
		name, accountID, operationID string
	}{
		{name: "different account", accountID: "account-b", operationID: "operation-a"},
		{name: "private operation", accountID: "account-a", operationID: "operation-private"},
		{name: "missing account", operationID: "operation-a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := application.GetOperationForScope(context.Background(), contracts.OperationScope{AccountID: test.accountID}, test.operationID)
			if !errors.Is(err, contracts.ErrNotFound) {
				t.Fatalf("GetOperationForScope error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestApplicationScopesIdempotencyByAccountButKeepsConnectorLifecycleGlobal(t *testing.T) {
	connector := testConnector("github")
	repository := newMemoryRepository(connector)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

	first, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "shared-request"}, ConnectorKey: connector.Key, AccountID: "account-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "different-request", ExpectedRevision: repository.revision},
		ConnectorKey: connector.Key, AccountID: "account-b",
	})
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeOperationInProgress {
		t.Fatalf("cross-account active operation error = %v", err)
	}

	finished := repository.operations[first.Operation.OperationID]
	finished.State = contracts.OperationStateFailed
	repository.operations[finished.OperationID] = finished
	connector = repository.connectors[connector.Key]
	connector.Installation = contracts.Installation{State: contracts.InstallationStateNotInstalled}
	repository.connectors[connector.Key] = connector
	second, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "shared-request", ExpectedRevision: repository.revision},
		ConnectorKey: connector.Key, AccountID: "account-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Operation.OperationID == first.Operation.OperationID || second.Operation.OwnerAccountID != "account-b" {
		t.Fatalf("cross-account request reuse joined wrong operation: first=%#v second=%#v", first.Operation, second.Operation)
	}
}

func TestApplicationManagedAuthorizationStartCanChallengeAnotherAccount(t *testing.T) {
	connector := testManagedAuthorizedConnector("lark-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateConnected}
	repository := newMemoryRepository(connector)
	projections := &recordingAuthorizationProjectionStore{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.AuthorizationProjections = projections

	result, err := application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "authorization-select-account"},
		ConnectorKey: connector.Key, AccountID: "account-2",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationURL == "" || result.Connector.Authorization.State != contracts.AuthorizationStatePending ||
		projections.projection.State != contracts.AuthorizationStatePending {
		t.Fatalf("result=%#v projection=%#v", result, projections.projection)
	}
	if repository.connectors[connector.Key].Authorization.State != contracts.AuthorizationStateConnected {
		t.Fatalf("device authorization = %#v", repository.connectors[connector.Key].Authorization)
	}
}

func TestApplicationManagedAuthorizationContinuationReplaysBeforeProjectionTransitionValidation(t *testing.T) {
	connector := testManagedAuthorizedConnector("lark-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	repository := newMemoryRepository(connector)
	projections := &recordingAuthorizationProjectionStore{}
	provider := &continuingAuthorizationProviderStub{}
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider
	application.config.AuthorizationProjections = projections
	mutation := contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "one-authorization-request", ExpectedRevision: 0},
		ConnectorKey: connector.Key,
		AccountID:    "account-1",
	}

	first, err := application.BeginAuthorization(context.Background(), mutation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.AuthorizationURL != "https://open.feishu.cn/page/cli" ||
		projections.projection.State != contracts.AuthorizationStatePending {
		t.Fatalf("first result=%#v projection=%#v", first, projections.projection)
	}
	continued, err := application.BeginAuthorization(context.Background(), mutation, nil)
	if err != nil {
		t.Fatalf("continue authorization: %v", err)
	}
	if continued.Operation.OperationID != first.Operation.OperationID ||
		continued.AuthorizationURL != "https://accounts.feishu.cn/device" || provider.begins != 2 {
		t.Fatalf("continued=%#v first=%#v provider begins=%d", continued, first, provider.begins)
	}
	if len(scheduler.operationIDs) != 0 {
		t.Fatalf("pending authorization scheduled runtime operations: %v", scheduler.operationIDs)
	}

	snapshot, err := application.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.BeginAuthorization(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "different-authorization-request", ExpectedRevision: snapshot.Revision},
		ConnectorKey: connector.Key,
		AccountID:    "account-1",
	}, nil)
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeOperationInProgress {
		t.Fatalf("different authorization error = %#v, want operation in progress", err)
	}
}

func TestApplicationResolvedAuthorizationReplayDoesNotRestartProvider(t *testing.T) {
	connector := testManagedAuthorizedConnector("lark-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	repository := newMemoryRepository(connector)
	projections := &recordingAuthorizationProjectionStore{}
	provider := &continuingAuthorizationProviderStub{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider
	application.config.AuthorizationProjections = projections
	mutation := contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "resolved-authorization-request", ExpectedRevision: 0},
		ConnectorKey: connector.Key,
		AccountID:    "account-1",
	}

	first, err := application.BeginAuthorization(context.Background(), mutation, nil)
	if err != nil {
		t.Fatal(err)
	}
	operation := repository.operations[first.Operation.OperationID]
	operation.Execution.AuthorizationSession.Resolution = contracts.AuthorizationSessionResolutionProviderConnected
	repository.operations[operation.OperationID] = operation
	projections.projection = contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, ConnectionID: "connection-1",
		State: contracts.AuthorizationStateConnected, ServerSynchronized: true,
	}

	replayed, err := application.BeginAuthorization(context.Background(), mutation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.begins != 1 {
		t.Fatalf("provider begins = %d, want 1", provider.begins)
	}
	if replayed.AuthorizationURL != "" || replayed.Connector.Authorization.State != contracts.AuthorizationStateConnected {
		t.Fatalf("replayed result = %#v", replayed)
	}
}

func TestApplicationConnectedProjectionConvergesReceiptWithoutProviderPolling(t *testing.T) {
	connector := testConnector("tencent-docs")
	connector.Release.Manifest.AuthorizationKind = "oauth2"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Release.Manifest.Implementation = contracts.Implementation{
		Kind: contracts.ImplementationKindRemoteStreamableHTTP,
		RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
			ProtocolVersion: "2026-07-28", BindingRef: "tencent-docs.primary", ContractVersion: 1,
			BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	repository := newMemoryRepository(connector)
	repository.operations["authorization-1"] = contracts.Operation{
		OperationID: "authorization-1", ClientRequestID: "request-1", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindStartAuthorization, Scope: contracts.OperationScope{AccountID: "account-1"},
		State: contracts.OperationStateCompleted, Stage: contracts.OperationStageCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-1", ConnectorKey: connector.Key, SessionID: "session-1",
			ActionType: "redirect", State: contracts.AuthorizationStatePending,
			Resolution: contracts.AuthorizationSessionResolutionUnresolved,
		}},
	}
	provider := &countingAuthorizationObserver{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider
	application.config.AuthorizationProjections = &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: connector.Key, ConnectionID: "connection-1",
		State: contracts.AuthorizationStateConnected, ServerSynchronized: true,
	}}

	intents, err := application.ReconcileAuthorizations(context.Background(), contracts.OperationScope{AccountID: "account-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].OperationID != "authorization-1" || intents[0].Resolution != contracts.AuthorizationSessionResolutionAccountStateConverged {
		t.Fatalf("reconcile intents = %#v", intents)
	}
	if provider.observations != 0 {
		t.Fatalf("provider observations = %d, want 0", provider.observations)
	}
	receipt := repository.operations["authorization-1"].Execution.AuthorizationSession
	if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionUnresolved {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestInstalledReleaseRemainsRunnableAfterCatalogAdvances(t *testing.T) {
	installedRelease := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	currentRelease := testReleaseWithImplementation("github", "2.0.0", contracts.ImplementationKindManagedStdio)
	currentRelease.ReleaseDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	currentRelease.ManifestDigest = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	connector := testConnector("github")
	connector.Release = currentRelease
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: installedRelease.Version,
		InstalledReleaseID: installedRelease.ReleaseID, InstalledReleaseDigest: installedRelease.ReleaseDigest}
	repository := newMemoryRepository(connector)
	repository.operations["install-evidence"] = contracts.Operation{
		OperationID: "install-evidence", ClientRequestID: "install-request", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindInstall, State: contracts.OperationStateCompleted, Stage: contracts.OperationStageCompleted,
		Target: &contracts.OperationTarget{ConnectorKey: connector.Key, Version: installedRelease.Version,
			ReleaseID: installedRelease.ReleaseID, ReleaseDigest: installedRelease.ReleaseDigest, Release: &installedRelease},
		CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(),
	}
	host := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, host, contracts.CatalogSnapshot{})

	if err := application.ReconcileInstalledRuntimes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host.reconciles != 1 || host.lastReconcile.Connector.Release.ReleaseDigest != installedRelease.ReleaseDigest {
		t.Fatalf("restart reconcile = %#v, count=%d", host.lastReconcile, host.reconciles)
	}
}

func TestInstallCompletionUsesFrozenReleaseAfterCatalogAdvances(t *testing.T) {
	installRelease := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	currentRelease := testReleaseWithImplementation("github", "2.0.0", contracts.ImplementationKindManagedStdio)
	currentRelease.ReleaseDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	currentRelease.ManifestDigest = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	connector := testConnector("github")
	connector.Release = currentRelease
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalling}
	repository := newMemoryRepository(connector)
	operation := contracts.Operation{
		OperationID: "install-1", ClientRequestID: "install-request", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindInstall, State: contracts.OperationStateAccepted, Stage: contracts.OperationStageAccepted,
		Target: &contracts.OperationTarget{ConnectorKey: connector.Key, Version: installRelease.Version,
			ReleaseID: installRelease.ReleaseID, ReleaseDigest: installRelease.ReleaseDigest,
			ArtifactSHA256: installRelease.Artifact.SHA256, Release: &installRelease},
		HostGeneration: contracts.HostGeneration{BootEpoch: "boot-1", Generation: 1},
		CreatedAt:      time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(),
	}
	repository.operations[operation.OperationID] = operation
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

	if err := application.ExecuteOperation(context.Background(), operation.OperationID); err != nil {
		t.Fatal(err)
	}
	stored := repository.connectors[connector.Key]
	if stored.Installation.InstalledReleaseDigest != installRelease.ReleaseDigest ||
		stored.Release.ReleaseDigest != currentRelease.ReleaseDigest {
		t.Fatalf("connector after frozen install = %#v", stored)
	}
}

func TestApplicationRecoveryObservesActivatedRuntimeBeforeCompleting(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	installationHost := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, installationHost, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	release := repository.connectors["github"].Release
	operation := repository.operations[accepted.Operation.OperationID]
	operation.State = contracts.OperationStateRunning
	operation.Stage = contracts.OperationStageInstalled
	operation.Execution.ReleaseInstallation = &contracts.ReleaseInstallationReceipt{
		OperationID:    operation.OperationID,
		ConnectorKey:   release.ConnectorKey,
		Version:        release.Version,
		ReleaseID:      release.ReleaseID,
		ReleaseDigest:  release.ReleaseDigest,
		ArtifactSHA256: release.Artifact.SHA256,
		Artifact: contracts.PreparedArtifactReceipt{OperationID: operation.OperationID, ConnectorKey: release.ConnectorKey,
			Version: release.Version, ReleaseDigest: release.ReleaseDigest, ArtifactSHA256: release.Artifact.SHA256,
			InventoryDigest: strings.Repeat("e", 64), PreparedPath: "/prepared/" + release.ReleaseDigest},
	}
	repository.operations[operation.OperationID] = operation
	installationHost.activeDigest = release.ReleaseDigest

	if err := application.ExecuteOperation(context.Background(), operation.OperationID); err != nil {
		t.Fatal(err)
	}
	completed := repository.operations[operation.OperationID]
	if completed.State != contracts.OperationStateCompleted || installationHost.activations != 0 {
		t.Fatalf("operation = %#v, activations = %d", completed, installationHost.activations)
	}
	if repository.connectors["github"].Installation.InstalledReleaseDigest != release.ReleaseDigest {
		t.Fatalf("installation = %#v", repository.connectors["github"].Installation)
	}
}

func TestApplicationRecoveryAdoptsInstallAndUninstallIntoCurrentBootEpoch(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0}, ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	install := repository.operations[accepted.Operation.OperationID]
	install.HostGeneration.BootEpoch = "previous-boot"
	repository.operations[install.OperationID] = install
	uninstall := install
	uninstall.OperationID = "recover-uninstall"
	uninstall.ClientRequestID = "request-2"
	uninstall.Kind = contracts.OperationKindUninstall
	repository.operations[uninstall.OperationID] = uninstall

	if err := application.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, operationID := range []string{install.OperationID, uninstall.OperationID} {
		operation := repository.operations[operationID]
		if operation.HostGeneration.BootEpoch == "previous-boot" || operation.HostGeneration.BootEpoch == "" {
			t.Fatalf("operation %s was not adopted: %#v", operationID, operation.HostGeneration)
		}
	}
	if len(scheduler.operationIDs) != 3 { // Install acceptance plus both recovery schedules.
		t.Fatalf("scheduled operations = %#v", scheduler.operationIDs)
	}
}

func TestApplicationWritesChangedEventsInsideRepositoryTransactions(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.events) != 1 || repository.events[0].OperationID != accepted.Operation.OperationID ||
		repository.events[0].Revision != accepted.Revision {
		t.Fatalf("events = %#v", repository.events)
	}
}

func TestApplicationDoesNotExecuteOperationHeldByAnotherWorker(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	installationHost := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, installationHost, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	operation := repository.operations[accepted.Operation.OperationID]
	expiresAt := time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC)
	operation.LeaseOwner = "other-worker"
	operation.LeaseExpiresAt = &expiresAt
	repository.operations[operation.OperationID] = operation

	if err := application.ExecuteOperation(context.Background(), operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if installationHost.prepares != 0 || installationHost.activations != 0 {
		t.Fatalf("prepares = %d, activations = %d", installationHost.prepares, installationHost.activations)
	}
}

func TestApplicationSingleFlightsConcurrentOperationExecution(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	scheduler := &memoryScheduler{}
	installer := newBlockingInstaller()
	application := newTestApplication(t, repository, scheduler, installer, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- application.ExecuteOperation(context.Background(), accepted.Operation.OperationID)
	}()
	select {
	case <-installer.started:
	case <-time.After(time.Second):
		t.Fatal("first operation did not reach installer")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- application.ExecuteOperation(context.Background(), accepted.Operation.OperationID)
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second execution returned before the first completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(installer.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first execution error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second execution error = %v", err)
	}
	if installs := installer.installs.Load(); installs != 1 {
		t.Fatalf("installer calls = %d, want 1", installs)
	}
}

func TestApplicationSharesConcurrentOperationFailureAndClearsFlight(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	scheduler := &memoryScheduler{}
	cause := errors.New("installer unavailable")
	installer := newBlockingInstallerWithError(cause)
	application := newTestApplication(t, repository, scheduler, installer, contracts.CatalogSnapshot{})
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- application.ExecuteOperation(context.Background(), accepted.Operation.OperationID)
	}()
	select {
	case <-installer.started:
	case <-time.After(time.Second):
		t.Fatal("first operation did not reach installer")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- application.ExecuteOperation(context.Background(), accepted.Operation.OperationID)
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second execution returned before the first completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(installer.release)
	firstErr := <-firstDone
	secondErr := <-secondDone
	for name, err := range map[string]error{"first": firstErr, "second": secondErr} {
		var domainError *contracts.DomainError
		if !errors.As(err, &domainError) || !errors.Is(err, cause) {
			t.Errorf("%s error = %#v, want install domain error caused by %v", name, err, cause)
		}
	}
	if installs := installer.installs.Load(); installs != 1 {
		t.Fatalf("installer calls = %d, want 1", installs)
	}

	operation, err := repository.Operation(context.Background(), accepted.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != contracts.OperationStateRunning {
		t.Fatalf("operation state = %q, want retryable running debt", operation.State)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err == nil {
		t.Fatal("retryable operation unexpectedly completed")
	}
}

func TestApplicationRecordsFailureAfterLeaseRenewalCancelsExecution(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	repository.renewOperationLeaseErr = errors.New("lease store temporarily unavailable")
	repository.rejectCanceledTransactionContext = true
	installer := newBlockingInstaller()
	application := newTestApplication(t, repository, &memoryScheduler{}, installer, contracts.CatalogSnapshot{})
	application.config.LeaseDuration = 30 * time.Millisecond
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "lease-cancel-install", ExpectedRevision: 0}, ConnectorKey: "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err == nil {
		t.Fatal("ExecuteOperation() error = nil, want lease cancellation")
	}
	operation, err := repository.Operation(context.Background(), accepted.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != contracts.OperationStateRunning || operation.Stage != contracts.OperationStageInstalling {
		t.Fatalf("operation after canceled execution = %#v", operation)
	}
}

func TestApplicationRejectsConcurrentConnectorOperation(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	if _, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "install-1", ExpectedRevision: 0},
		ConnectorKey: "github",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := application.Uninstall(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "uninstall-1", ExpectedRevision: 1},
		ConnectorKey: "github",
	})
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeOperationInProgress {
		t.Fatalf("error = %#v", err)
	}
}

func TestApplicationEnsureRuntimeReconcileCreatesOrJoinsCurrentScope(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{
		State:                  contracts.InstallationStateInstalled,
		InstalledVersion:       connector.Release.Version,
		InstalledReleaseID:     connector.Release.ReleaseID,
		InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(connector)
	repository.revision = 7
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	scope := contracts.OperationScope{AccountID: "account-1"}

	created, err := application.EnsureRuntimeReconcile(context.Background(), scope, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	running := repository.operations[created.Operation.OperationID]
	running.State = contracts.OperationStateRunning
	repository.operations[running.OperationID] = running
	joined, err := application.EnsureRuntimeReconcile(context.Background(), scope, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Operation.OperationID != created.Operation.OperationID {
		t.Fatalf("joined operation = %q, want %q", joined.Operation.OperationID, created.Operation.OperationID)
	}
	if !created.Created || joined.Created {
		t.Fatalf("created=%t joined=%t", created.Created, joined.Created)
	}
	if repository.revision != 8 || len(repository.operations) != 1 {
		t.Fatalf("revision=%d operations=%#v", repository.revision, repository.operations)
	}
	if len(scheduler.operationIDs) != 1 || scheduler.operationIDs[0] != created.Operation.OperationID {
		t.Fatalf("scheduled operations = %#v", scheduler.operationIDs)
	}
	completed := repository.operations[created.Operation.OperationID]
	completed.State = contracts.OperationStateCompleted
	completed.Stage = contracts.OperationStageCompleted
	repository.operations[completed.OperationID] = completed
	followup, err := application.EnsureRuntimeReconcile(context.Background(), scope, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !followup.Created || followup.Operation.OperationID == created.Operation.OperationID || repository.revision != 9 {
		t.Fatalf("followup=%#v revision=%d", followup, repository.revision)
	}
}

func TestApplicationEnsureRuntimeReconcileDoesNotJoinDifferentScope(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{
		State:                  contracts.InstallationStateInstalled,
		InstalledVersion:       connector.Release.Version,
		InstalledReleaseID:     connector.Release.ReleaseID,
		InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(connector)
	repository.operations["old-account-reconcile"] = contracts.Operation{
		OperationID: "old-account-reconcile", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindReconcileRuntime, Scope: contracts.OperationScope{AccountID: "account-old"},
		State: contracts.OperationStateRunning, Stage: contracts.OperationStageAccepted,
	}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

	_, err := application.EnsureRuntimeReconcile(context.Background(), contracts.OperationScope{AccountID: "account-new"}, connector.Key)
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeOperationInProgress {
		t.Fatalf("error = %#v, want operation in progress", err)
	}
	if len(repository.operations) != 1 {
		t.Fatalf("operations = %#v", repository.operations)
	}
}

func TestApplicationRefreshRejectsUnknownImplementation(t *testing.T) {
	repository := newMemoryRepository()
	scheduler := &memoryScheduler{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{
		SourceRevision: "catalog-2",
		Categories:     []contracts.CatalogCategory{{CategoryID: "development", Kind: "category", ItemCount: 1}},
		Entries: []contracts.CatalogEntry{{SectionID: "development", CategoryID: "development",
			Release: testReleaseWithImplementation("future-connector", "2.0.0", "future_runtime")}},
	})
	accepted, err := application.RefreshCatalog(context.Background(), contracts.Mutation{
		ClientRequestID:  "refresh-1",
		ExpectedRevision: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err == nil {
		t.Fatal("ExecuteOperation() expected strict manifest rejection")
	}
}

func TestApplicationRejectsStaleRevisionBeforeMutation(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	repository.revision = 4
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	_, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: "request-1", ExpectedRevision: 3},
		ConnectorKey: "github",
	})
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeRevisionConflict {
		t.Fatalf("error = %#v", err)
	}
	if len(repository.operations) != 0 {
		t.Fatalf("operations = %#v", repository.operations)
	}
}

func TestApplicationCatalogPageReturnsUnavailableWithoutLastGoodSnapshot(t *testing.T) {
	repository := newMemoryRepository()
	repository.catalogView.Freshness = contracts.CatalogFreshness{State: contracts.CatalogFreshnessUnavailable, LastFailure: "request_timeout"}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

	_, err := application.ListCatalogPageForScope(context.Background(), contracts.OperationScope{}, contracts.CatalogPageQuery{
		SectionID: "development", PageSize: 20,
	})
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) {
		t.Fatalf("error = %v, want DomainError", err)
	}
	if domainError.Code != contracts.ErrorCodeUnavailable || !domainError.Retryable {
		t.Fatalf("domain error = %#v", domainError)
	}
}

func TestApplicationRefreshPreservesManifestFailureCode(t *testing.T) {
	repository := newMemoryRepository()
	application := newTestApplicationWithCatalogSource(
		t,
		repository,
		&memoryScheduler{},
		&memoryInstallRuntime{},
		failingCatalogSource{refreshError: invalidManifest("permission scope is invalid", nil)},
	)
	accepted, err := application.RefreshCatalog(context.Background(), contracts.Mutation{
		ClientRequestID: "refresh-invalid-manifest", ExpectedRevision: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = application.ExecuteOperation(context.Background(), accepted.Operation.OperationID)
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeInvalidManifest {
		t.Fatalf("error = %#v, want invalid manifest", err)
	}
	operation, err := application.GetOperation(context.Background(), accepted.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != contracts.OperationStateFailed || operation.FailureCode != string(contracts.ErrorCodeInvalidManifest) {
		t.Fatalf("operation = %#v", operation)
	}
}

func TestApplicationReconcilesCompletedAuthorizationSession(t *testing.T) {
	connector := testConnector("gmail")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	repository := newMemoryRepository(connector)
	repository.operations["authorization-1"] = contracts.Operation{
		OperationID: "authorization-1", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindStartAuthorization, State: contracts.OperationStateCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-1", ConnectorKey: connector.Key,
			SessionID: "session-1", AuthorizationURL: "https://example.test/authorize",
		}},
		UpdatedAt: time.Date(2026, 8, 3, 0, 1, 0, 0, time.UTC),
	}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = observingAuthorizationProvider{observation: contracts.AuthorizationObservation{State: contracts.AuthorizationObservationConnected}}
	if _, err := application.ReconcileAuthorizations(context.Background(), contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	updated, err := repository.Connector(context.Background(), connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Authorization.State != contracts.AuthorizationStateConnected || len(repository.events) != 1 {
		t.Fatalf("connector=%#v events=%#v", updated, repository.events)
	}
}

func TestApplicationRecoveryFinishesCancelingAuthorizationReceipt(t *testing.T) {
	connector := testManagedAuthorizedConnector("dingtalk-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	repository := newMemoryRepository(connector)
	operation := contracts.Operation{
		OperationID: "authorization-a", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindStartAuthorization, Scope: contracts.OperationScope{AccountID: "account-1"},
		State: contracts.OperationStateCompleted, Stage: contracts.OperationStageCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-a", ConnectorKey: connector.Key,
			SessionID: "session-a", State: contracts.AuthorizationStatePending,
			Resolution: contracts.AuthorizationSessionResolutionCanceling,
		}},
	}
	repository.operations[operation.OperationID] = operation
	provider := &cancelingAuthorizationProvider{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider

	intents, err := application.ReconcileAuthorizations(context.Background(), operation.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 0 || len(provider.canceled) != 1 || provider.canceled[0] != operation.OperationID {
		t.Fatalf("intents=%#v cancellations=%#v", intents, provider.canceled)
	}
	receipt := repository.operations[operation.OperationID].Execution.AuthorizationSession
	if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionSuperseded {
		t.Fatalf("recovered receipt = %#v, want superseded", receipt)
	}
}

func TestApplicationRecoveryFinishesExplicitCancellationWithoutProviderCanceler(t *testing.T) {
	for _, resolution := range []contracts.AuthorizationSessionResolution{
		contracts.AuthorizationSessionResolutionUnresolved,
		contracts.AuthorizationSessionResolutionCanceling,
	} {
		t.Run(string(resolution), func(t *testing.T) {
			connector := testManagedAuthorizedConnector("recover-explicit-cancel-" + string(resolution))
			connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
			repository := newMemoryRepository(connector)
			operation := contracts.Operation{
				OperationID: "authorization-explicit-cancel", ClientRequestID: "authorization-start",
				ConnectorKey: connector.Key, Kind: contracts.OperationKindStartAuthorization,
				Scope: contracts.OperationScope{AccountID: "account-1"}, State: contracts.OperationStateCompleted,
				Stage:  contracts.OperationStageCompleted,
				Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
				Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
					OperationID: "authorization-explicit-cancel", ConnectorKey: connector.Key,
					SessionID: "session-explicit-cancel", State: contracts.AuthorizationStatePending,
					Resolution: resolution, CancellationClientRequestID: "cancel-request",
				}},
			}
			repository.operations[operation.OperationID] = operation
			application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

			intents, err := application.ReconcileAuthorizations(context.Background(), operation.Scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(intents) != 0 {
				t.Fatalf("explicit cancellation intents = %#v", intents)
			}
			receipt := repository.operations[operation.OperationID].Execution.AuthorizationSession
			if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionSuperseded ||
				receipt.CancellationClientRequestID != "cancel-request" {
				t.Fatalf("recovered explicit cancellation receipt = %#v", receipt)
			}
			if current := repository.connectors[connector.Key]; current.Authorization.State != contracts.AuthorizationStateDisconnected {
				t.Fatalf("connector after explicit cancellation recovery = %#v", current.Authorization)
			}
		})
	}
}

func TestApplicationRecoveryKeepsReplacementCancellationProviderFenced(t *testing.T) {
	connector := testManagedAuthorizedConnector("recover-replacement-cancel")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	repository := newMemoryRepository(connector)
	operation := contracts.Operation{
		OperationID: "authorization-replacement-cancel", ClientRequestID: "authorization-start",
		ConnectorKey: connector.Key, Kind: contracts.OperationKindStartAuthorization,
		Scope: contracts.OperationScope{AccountID: "account-1"}, State: contracts.OperationStateCompleted,
		Stage:  contracts.OperationStageCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-replacement-cancel", ConnectorKey: connector.Key,
			SessionID: "session-replacement-cancel", State: contracts.AuthorizationStatePending,
			Resolution: contracts.AuthorizationSessionResolutionCanceling,
		}},
	}
	repository.operations[operation.OperationID] = operation
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})

	intents, err := application.ReconcileAuthorizations(context.Background(), operation.Scope)
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeUnavailable || len(intents) != 0 {
		t.Fatalf("replacement cancellation recovery = intents %#v error %#v", intents, err)
	}
	receipt := repository.operations[operation.OperationID].Execution.AuthorizationSession
	if receipt == nil || receipt.Resolution != contracts.AuthorizationSessionResolutionCanceling ||
		receipt.CancellationClientRequestID != "" {
		t.Fatalf("replacement cancellation receipt = %#v", receipt)
	}
	if current := repository.connectors[connector.Key]; current.Authorization.State != contracts.AuthorizationStatePending {
		t.Fatalf("replacement cancellation reset without provider fence = %#v", current.Authorization)
	}
}

func TestApplicationAuthorizationObservationExpiresAfterTenMinutes(t *testing.T) {
	connector := testConnector("gmail-timeout")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	repository := newMemoryRepository(connector)
	repository.operations["authorization-timeout"] = contracts.Operation{
		OperationID: "authorization-timeout", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindStartAuthorization, State: contracts.OperationStateCompleted,
		Target:    operationTarget(contracts.OperationKindStartAuthorization, connector),
		UpdatedAt: time.Date(2026, 8, 2, 23, 49, 59, 0, time.UTC),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-timeout", ConnectorKey: connector.Key,
			SessionID: "session-timeout", State: contracts.AuthorizationStatePending,
		}},
	}
	provider := &countingAuthorizationObserver{}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = provider

	intents, err := application.ReconcileAuthorizations(context.Background(), contracts.OperationScope{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.observations != 0 {
		t.Fatalf("expired authorization was observed %d times", provider.observations)
	}
	if len(intents) != 1 || intents[0].Resolution != contracts.AuthorizationSessionResolutionProviderFailed {
		t.Fatalf("expiry intents = %#v", intents)
	}
	updated, err := repository.Connector(context.Background(), connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Authorization.State != contracts.AuthorizationStateFailed || updated.Authorization.FailureCode != "connector_authorization_timeout" {
		t.Fatalf("expired authorization = %#v", updated.Authorization)
	}
}

func TestApplicationAuthorizationRecoveryProjectsWithoutSchedulingRuntime(t *testing.T) {
	connector := testManagedAuthorizedConnector("gmail")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
	repository := newMemoryRepository(connector)
	repository.operations["authorization-1"] = contracts.Operation{
		OperationID: "authorization-1", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindStartAuthorization, Scope: contracts.OperationScope{AccountID: "account-1"},
		State: contracts.OperationStateCompleted, Stage: contracts.OperationStageCompleted,
		Target: operationTarget(contracts.OperationKindStartAuthorization, connector),
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-1", ConnectorKey: connector.Key,
			SessionID: "session-1", State: contracts.AuthorizationStatePending,
			Resolution: contracts.AuthorizationSessionResolutionUnresolved,
		}},
	}
	scheduler := &memoryScheduler{}
	projections := &recordingAuthorizationProjectionStore{}
	application := newTestApplication(t, repository, scheduler, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	application.config.Authorization = observingAuthorizationProvider{observation: contracts.AuthorizationObservation{
		State: contracts.AuthorizationObservationConnected, ConnectionID: "connection-1",
	}}
	application.config.AuthorizationProjections = projections

	intents, err := application.ReconcileAuthorizations(context.Background(), contracts.OperationScope{AccountID: "account-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].OperationID != "authorization-1" {
		t.Fatalf("intents = %#v", intents)
	}
	if len(scheduler.operationIDs) != 0 {
		t.Fatalf("recovery scheduled runtime operations = %#v", scheduler.operationIDs)
	}
	if projections.projection.State != contracts.AuthorizationStateConnected || projections.projection.ConnectionID != "connection-1" {
		t.Fatalf("projection = %#v", projections.projection)
	}
}

func newTestApplication(
	t *testing.T,
	repository *memoryRepository,
	scheduler *memoryScheduler,
	installationHost interface {
		ReleaseInstallationManager
		ImplementationCommands
	},
	catalog contracts.CatalogSnapshot,
) *service {
	return newTestApplicationWithCatalogSource(
		t,
		repository,
		scheduler,
		installationHost,
		catalogSourceFunc(func(context.Context) (contracts.CatalogSnapshot, error) { return catalog, nil }),
	)
}

func newTestApplicationWithCatalogSource(
	t *testing.T,
	repository *memoryRepository,
	scheduler *memoryScheduler,
	installationHost interface {
		ReleaseInstallationManager
		ImplementationCommands
	},
	catalogSource CatalogSource,
) *service {
	t.Helper()
	nextID := 0
	application, err := newService(Config{
		Repository:             repository,
		CatalogSource:          catalogSource,
		ReleaseInstallations:   installationHost,
		ImplementationCommands: installationHost,
		Authorization:          authorizationProviderStub{},
		Compatibility:          compatibilityEvaluatorStub{},
		Scheduler:              scheduler,
		ImplementationRegistry: NewImplementationRegistry(map[string]ImplementationValidator{contracts.ImplementationKindManagedStdio: nil}),
		Now:                    func() time.Time { return time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) },
		RuntimeRetryJitter:     func(maximum time.Duration) time.Duration { return maximum },
		NewID: func() (string, error) {
			nextID++
			return fmt.Sprintf("operation-%d", nextID), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func testConnector(key string) contracts.Connector {
	return contracts.Connector{
		Key:           key,
		Release:       testReleaseWithImplementation(key, "1.0.0", "mcp_stdio"),
		Installation:  contracts.Installation{State: contracts.InstallationStateNotInstalled},
		Authorization: contracts.Authorization{State: contracts.AuthorizationStateNotRequired},
		Compatibility: contracts.Compatibility{State: contracts.CompatibilityStateSupported},
	}
}

func testManagedAuthorizedConnector(key string) contracts.Connector {
	connector := testConnector(key)
	connector.Release.Manifest.AuthorizationKind = "oauth2"
	connector.Release.Manifest.RequiredCapabilities = []string{"tools"}
	connector.Release.Manifest.Implementation.ManagedStdio.Runtime.VersionRange = ">=20.0.0 <21.0.0"
	connector.Release.Manifest.Implementation.ManagedStdio.CLI = &contracts.ManagedCLIInterface{Entrypoint: "bin/lark-cli", TimeoutMS: 120_000}
	connector.Release.Manifest.Implementation.ManagedStdio.CredentialBroker = &contracts.ManagedCredentialBroker{
		Protocol: contracts.CredentialBrokerProtocolV1, Entrypoint: "authorization/broker.mjs", TimeoutMS: 30_000,
		AllowedHosts: []string{"open.larksuite.com"},
	}
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest}
	return connector
}

func testReleaseWithImplementation(key, version, implementationKind string) contracts.Release {
	implementation := contracts.Implementation{Kind: implementationKind, Builtin: &contracts.BuiltinImplementation{ProviderID: key, MCP: true}}
	if implementationKind == "mcp_stdio" || implementationKind == contracts.ImplementationKindManagedStdio {
		implementation = contracts.Implementation{Kind: contracts.ImplementationKindManagedStdio, ManagedStdio: &contracts.ManagedStdioImplementation{
			Runtime: contracts.RuntimeRequirement{Language: "node", Profile: "connector-node-static", ABI: "node20-darwin-arm64"},
			MCP:     &contracts.ManagedMCPInterface{Entrypoint: "bin/connector.js"},
		}}
	}
	return contracts.Release{
		SchemaVersion:  "1",
		ReleaseID:      key + "@" + version,
		ConnectorKey:   key,
		Version:        version,
		ReleaseDigest:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Manifest: contracts.Manifest{
			SchemaVersion:     "1",
			DisplayName:       key,
			IconURL:           testConnectorIconURL,
			Implementation:    implementation,
			AuthorizationKind: "none",
		},
		Artifact:    testArtifact(),
		PublishedAt: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		Status:      contracts.ReleaseStatusAvailable,
	}
}

type catalogSourceFunc func(context.Context) (contracts.CatalogSnapshot, error)

func (source catalogSourceFunc) FetchSnapshot(ctx context.Context) (contracts.CatalogSnapshot, error) {
	return source(ctx)
}

type catalogSourceStub struct {
	snapshot contracts.CatalogSnapshot
}

type failingCatalogSource struct {
	refreshError error
}

func (source failingCatalogSource) FetchSnapshot(context.Context) (contracts.CatalogSnapshot, error) {
	return contracts.CatalogSnapshot{}, source.refreshError
}

func (source catalogSourceStub) FetchSnapshot(context.Context) (contracts.CatalogSnapshot, error) {
	return source.snapshot, nil
}

type memoryScheduler struct {
	operationIDs []string
}

func (scheduler *memoryScheduler) Schedule(_ context.Context, operationID string) error {
	scheduler.operationIDs = append(scheduler.operationIDs, operationID)
	return nil
}

type memoryInstallRuntime struct {
	prepares                int
	removes                 int
	activations             int
	deactivations           int
	activeDigest            string
	reconciles              int
	reconcileRequests       []contracts.RuntimeReconcileRequest
	lastReconcile           contracts.RuntimeReconcileRequest
	lastDeactivation        contracts.RuntimeDeactivationRequest
	lastPrepare             contracts.PrepareArtifactRequest
	lastCredentialGrant     string
	deactivationErr         error
	failClosed              int
	cliInstalls             int
	cliRemoves              int
	installationInspections int
	installationResult      contracts.ReleaseInstallationObservation
	installationInspectErr  error
	installationCommitErr   error
	reconcileErrors         map[string]error
}

func (*memoryInstallRuntime) Close(context.Context) error { return nil }

func (host *memoryInstallRuntime) Reconcile(_ context.Context, request contracts.RuntimeReconcileRequest) (contracts.RuntimeReceipt, error) {
	host.reconciles++
	host.reconcileRequests = append(host.reconcileRequests, request)
	host.lastReconcile = request
	host.lastCredentialGrant = string(request.CredentialBrokerGrant)
	if err := host.reconcileErrors[request.Connector.Key]; err != nil {
		return contracts.RuntimeReceipt{}, err
	}
	readiness := contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady,
		Interfaces: []contracts.InterfaceReadiness{{Kind: "mcp", State: contracts.RuntimeReadinessReady}}}
	summary := &contracts.ConnectorSummary{Key: request.Connector.Key, Name: request.Connector.Key,
		Interfaces: []contracts.ConnectorInterfaceSummary{{Kind: "mcp", ServerName: "connector", Status: string(contracts.RuntimeReadinessReady)}}}
	if !request.Enabled {
		readiness = contracts.RuntimeReadiness{State: contracts.RuntimeReadinessBlocked, ReasonCode: contracts.RuntimeReadinessReasonRuntimeDisabled}
		summary = nil
	}
	return contracts.RuntimeReceipt{OperationID: request.OperationID, ConnectionID: request.ConnectionID,
		ConnectorKey: request.Connector.Key, ReleaseDigest: request.Connector.Release.ReleaseDigest, Generation: request.Generation,
		Readiness: readiness, Summary: summary}, nil
}

func (host *memoryInstallRuntime) InspectReleaseInstallation(_ context.Context, request contracts.InspectReleaseInstallationRequest) (contracts.ReleaseInstallationObservation, error) {
	host.installationInspections++
	if host.installationInspectErr != nil {
		return contracts.ReleaseInstallationObservation{}, host.installationInspectErr
	}
	result := host.installationResult
	if result.State == "" {
		result.State = contracts.ReleaseInstallationPresent
	}
	if result.ConnectorKey == "" {
		result.ConnectorKey = request.Release.ConnectorKey
	}
	if result.ReleaseDigest == "" {
		result.ReleaseDigest = request.Release.ReleaseDigest
	}
	return result, nil
}

func (host *memoryInstallRuntime) DeactivateRuntime(_ context.Context, request contracts.RuntimeDeactivationRequest) error {
	host.deactivations++
	host.lastDeactivation = request
	if host.deactivationErr != nil {
		return host.deactivationErr
	}
	host.activeDigest = ""
	return nil
}

func (host *memoryInstallRuntime) FailClosed(context.Context, time.Time) error {
	host.failClosed++
	return nil
}

func (host *memoryInstallRuntime) Prepare(_ context.Context, request contracts.PrepareArtifactRequest) (contracts.PreparedArtifactReceipt, error) {
	host.prepares++
	host.lastPrepare = request
	return contracts.PreparedArtifactReceipt{
		OperationID:     request.OperationID,
		ConnectorKey:    request.Release.ConnectorKey,
		Version:         request.Release.Version,
		ReleaseDigest:   request.Release.ReleaseDigest,
		ArtifactSHA256:  request.Release.Artifact.SHA256,
		InventoryDigest: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		PreparedPath:    "/prepared/" + request.Release.ReleaseDigest,
	}, nil
}

func (host *memoryInstallRuntime) InstallRelease(
	ctx context.Context,
	request contracts.InstallReleaseRequest,
) (contracts.ReleaseInstallationReceipt, error) {
	prepared, err := host.Prepare(ctx, contracts.PrepareArtifactRequest(request))
	if err != nil {
		return contracts.ReleaseInstallationReceipt{}, err
	}
	receipt := contracts.ReleaseInstallationReceipt{OperationID: request.OperationID, ConnectorKey: request.Release.ConnectorKey,
		Version: request.Release.Version, ReleaseID: request.Release.ReleaseID, ReleaseDigest: request.Release.ReleaseDigest,
		ArtifactSHA256: request.Release.Artifact.SHA256, Artifact: prepared}
	if releaseCLIInstallation(request.Release) != nil {
		installed, err := host.InstallCLI(ctx, contracts.InstallCLIRequest(request))
		if err != nil {
			return contracts.ReleaseInstallationReceipt{}, err
		}
		receipt.CLIInstallation = &installed
	}
	return receipt, nil
}

func (host *memoryInstallRuntime) UninstallRelease(ctx context.Context, request contracts.UninstallReleaseRequest) error {
	if releaseCLIInstallation(request.Release) != nil {
		if err := host.RemoveCLI(ctx, contracts.RemoveCLIRequest{OperationID: request.OperationID, Scope: request.Scope,
			Generation: request.Generation, ConnectorKey: request.Release.ConnectorKey,
			ReleaseDigest: request.Release.ReleaseDigest}); err != nil {
			return err
		}
	}
	return host.Remove(ctx, contracts.RemoveArtifactRequest{OperationID: request.OperationID, Scope: request.Scope,
		Generation: request.Generation, ConnectorKey: request.Release.ConnectorKey, Version: request.Release.Version,
		ReleaseDigest: request.Release.ReleaseDigest})
}

func (host *memoryInstallRuntime) CommitReleaseInstallation(context.Context, contracts.CommitReleaseInstallationRequest) error {
	return host.installationCommitErr
}

type runtimeBindingResolverStub struct {
	binding contracts.RuntimeBinding
}

type runtimeBindingResolverFunc func(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error)

type recordingAuthorizationProjectionStore struct {
	projection contracts.AuthorizationProjection
}

func (store *recordingAuthorizationProjectionStore) AuthorizationProjection(context.Context, string, string) (contracts.AuthorizationProjection, error) {
	if store.projection.AccountID == "" {
		return contracts.AuthorizationProjection{}, contracts.ErrNotFound
	}
	return store.projection, nil
}

func (store *recordingAuthorizationProjectionStore) SaveAuthorizationProjection(_ context.Context, projection contracts.AuthorizationProjection) error {
	store.projection = projection
	return nil
}

func (resolver *runtimeBindingResolverStub) ResolveRuntimeBinding(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
	return resolver.binding, nil
}

func (resolver runtimeBindingResolverFunc) ResolveRuntimeBinding(ctx context.Context, request contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
	return resolver(ctx, request)
}

func (host *memoryInstallRuntime) Remove(context.Context, contracts.RemoveArtifactRequest) error {
	host.removes++
	return nil
}

func (host *memoryInstallRuntime) InstallCLI(_ context.Context, request contracts.InstallCLIRequest) (contracts.CLIInstallationReceipt, error) {
	host.cliInstalls++
	install := releaseCLIInstallation(request.Release)
	return contracts.CLIInstallationReceipt{SchemaVersion: "tutti.connector.cli-installation.v1", OperationID: request.OperationID,
		ConnectorKey: request.Release.ConnectorKey, ReleaseDigest: request.Release.ReleaseDigest,
		RuntimeProfile: "connector-node-static", RuntimeABI: request.Release.Manifest.Implementation.ManagedStdio.Runtime.ABI,
		NodeVersion: "22.22.3", NodeSHA256: "1111111111111111111111111111111111111111111111111111111111111111",
		Package: install.Package, PackageVersion: install.Version, PackageIntegrity: install.Integrity, LaunchKind: install.Launch.Kind,
		InstallRoot: "/installed/" + request.Release.ReleaseDigest, StoreRoot: "/store",
		Entrypoint:       "node_modules/@larksuite/cli/bin/lark-cli",
		EntrypointSHA256: "2222222222222222222222222222222222222222222222222222222222222222",
		EntrypointSize:   7, LockSHA256: "3333333333333333333333333333333333333333333333333333333333333333"}, nil
}

func (*memoryInstallRuntime) ResolveCLI(context.Context, contracts.Release) (contracts.CLIInstallationReceipt, error) {
	return contracts.CLIInstallationReceipt{}, nil
}

func (host *memoryInstallRuntime) RemoveCLI(context.Context, contracts.RemoveCLIRequest) error {
	host.cliRemoves++
	return nil
}

type blockingInstaller struct {
	memoryInstallRuntime
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	installs atomic.Int32
	err      error
}

func newBlockingInstaller() *blockingInstaller {
	return newBlockingInstallerWithError(nil)
}

func newBlockingInstallerWithError(err error) *blockingInstaller {
	return &blockingInstaller{
		started: make(chan struct{}),
		release: make(chan struct{}),
		err:     err,
	}
}

func (installer *blockingInstaller) InstallRelease(ctx context.Context, request contracts.InstallReleaseRequest) (contracts.ReleaseInstallationReceipt, error) {
	installer.installs.Add(1)
	installer.once.Do(func() { close(installer.started) })
	select {
	case <-installer.release:
		if installer.err != nil {
			return contracts.ReleaseInstallationReceipt{}, installer.err
		}
		return contracts.ReleaseInstallationReceipt{
			OperationID: request.OperationID, ConnectorKey: request.Release.ConnectorKey,
			Version: request.Release.Version, ReleaseID: request.Release.ReleaseID,
			ReleaseDigest: request.Release.ReleaseDigest, ArtifactSHA256: request.Release.Artifact.SHA256,
			Artifact: contracts.PreparedArtifactReceipt{OperationID: request.OperationID, ConnectorKey: request.Release.ConnectorKey,
				Version: request.Release.Version, ReleaseDigest: request.Release.ReleaseDigest,
				ArtifactSHA256:  request.Release.Artifact.SHA256,
				InventoryDigest: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				PreparedPath:    "/prepared/" + request.Release.ReleaseDigest},
		}, nil
	case <-ctx.Done():
		return contracts.ReleaseInstallationReceipt{}, ctx.Err()
	}
}

type authorizationProviderStub struct{}

func (authorizationProviderStub) Begin(_ context.Context, request contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	return contracts.AuthorizationSession{
		OperationID:      request.OperationID,
		ConnectorKey:     request.Connector.Key,
		SessionID:        "session-1",
		ActionType:       "redirect",
		AuthorizationURL: "https://example.test/authorize",
		State:            contracts.AuthorizationStatePending,
	}, nil
}

func (authorizationProviderStub) Disconnect(context.Context, contracts.AuthorizationDisconnectRequest) error {
	return nil
}

type failingAuthorizationProvider struct {
	err    error
	begins atomic.Int32
}

func (provider *failingAuthorizationProvider) Begin(context.Context, contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	provider.begins.Add(1)
	return contracts.AuthorizationSession{}, provider.err
}

func (*failingAuthorizationProvider) Disconnect(context.Context, contracts.AuthorizationDisconnectRequest) error {
	return nil
}

type connectedAuthorizationProviderStub struct {
	authorizationProviderStub
}

type blockingAuthorizationProvider struct {
	authorizationProviderStub
	started chan struct{}
	release chan struct{}
	begins  atomic.Int32
}

type interruptibleAuthorizationProvider struct {
	authorizationProviderStub
	started  chan struct{}
	canceled chan struct{}
	begins   atomic.Int32
	cancels  atomic.Int32
}

func (provider *interruptibleAuthorizationProvider) Begin(
	ctx context.Context,
	request contracts.AuthorizationStartRequest,
) (contracts.AuthorizationSession, error) {
	if provider.begins.Add(1) == 1 {
		close(provider.started)
		<-ctx.Done()
		close(provider.canceled)
		return contracts.AuthorizationSession{}, ctx.Err()
	}
	return provider.authorizationProviderStub.Begin(ctx, request)
}

func (provider *interruptibleAuthorizationProvider) Cancel(
	context.Context,
	contracts.AuthorizationCancelRequest,
) error {
	provider.cancels.Add(1)
	return nil
}

type cancelingAuthorizationProvider struct {
	authorizationProviderStub
	begins                atomic.Int32
	canceled              []string
	lastReplacementPolicy contracts.AuthorizationReplacementPolicy
}

func (provider *cancelingAuthorizationProvider) Begin(
	ctx context.Context,
	request contracts.AuthorizationStartRequest,
) (contracts.AuthorizationSession, error) {
	provider.begins.Add(1)
	provider.lastReplacementPolicy = request.ReplacementPolicy
	session, err := provider.authorizationProviderStub.Begin(ctx, request)
	session.SessionID = request.OperationID + "/session"
	return session, err
}

func (provider *cancelingAuthorizationProvider) Cancel(
	_ context.Context,
	request contracts.AuthorizationCancelRequest,
) error {
	provider.canceled = append(provider.canceled, request.OperationID)
	return nil
}

func (provider *blockingAuthorizationProvider) Begin(
	_ context.Context,
	request contracts.AuthorizationStartRequest,
) (contracts.AuthorizationSession, error) {
	if provider.begins.Add(1) == 1 {
		close(provider.started)
	}
	<-provider.release
	return provider.authorizationProviderStub.Begin(context.Background(), request)
}

func (connectedAuthorizationProviderStub) Begin(_ context.Context, request contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	return contracts.AuthorizationSession{
		OperationID:  request.OperationID,
		ConnectorKey: request.Connector.Key,
		SessionID:    "session-connected",
		ConnectionID: "existing-cli-login",
		State:        contracts.AuthorizationStateConnected,
	}, nil
}

type continuingAuthorizationProviderStub struct {
	authorizationProviderStub
	begins int
}

func (provider *continuingAuthorizationProviderStub) Begin(
	_ context.Context,
	request contracts.AuthorizationStartRequest,
) (contracts.AuthorizationSession, error) {
	provider.begins++
	authorizationURL := "https://open.feishu.cn/page/cli"
	if provider.begins > 1 {
		authorizationURL = "https://accounts.feishu.cn/device"
	}
	return contracts.AuthorizationSession{
		OperationID:      request.OperationID,
		ConnectorKey:     request.Connector.Key,
		SessionID:        request.OperationID + "/credential-broker",
		ActionType:       "redirect",
		AuthorizationURL: authorizationURL,
		State:            contracts.AuthorizationStatePending,
	}, nil
}

type countingAuthorizationProvider struct {
	authorizationProviderStub
	disconnects int
}

func (provider *countingAuthorizationProvider) Disconnect(context.Context, contracts.AuthorizationDisconnectRequest) error {
	provider.disconnects++
	return nil
}

type observingAuthorizationProvider struct {
	authorizationProviderStub
	observation contracts.AuthorizationObservation
}

type countingAuthorizationObserver struct {
	authorizationProviderStub
	observations int
}

func (provider *countingAuthorizationObserver) Observe(context.Context, contracts.AuthorizationObserveRequest) (contracts.AuthorizationObservation, error) {
	provider.observations++
	return contracts.AuthorizationObservation{State: contracts.AuthorizationObservationPending}, nil
}

func (provider observingAuthorizationProvider) Observe(context.Context, contracts.AuthorizationObserveRequest) (contracts.AuthorizationObservation, error) {
	return provider.observation, nil
}

type compatibilityEvaluatorStub struct{}

func (compatibilityEvaluatorStub) Evaluate(contracts.Manifest) contracts.Compatibility {
	return contracts.Compatibility{State: contracts.CompatibilityStateSupported}
}

type memoryRepository struct {
	revision                         uint64
	catalogView                      contracts.CatalogView
	catalogGeneration                uint64
	connectors                       map[string]contracts.Connector
	operations                       map[string]contracts.Operation
	runtimeConvergences              map[string]contracts.RuntimeConvergence
	runtimeConvergenceCalls          int
	runtimeConvergencesCalls         int
	installedReleasesCalls           int
	events                           []contracts.ChangedEvent
	transactionErr                   error
	transactionCalls                 int
	failTransactionCall              int
	failTransactionErr               error
	renewOperationLeaseErr           error
	rejectCanceledTransactionContext bool
}

func newMemoryRepository(connectors ...contracts.Connector) *memoryRepository {
	repository := &memoryRepository{
		catalogView:         contracts.CatalogView{Freshness: contracts.CatalogFreshness{State: contracts.CatalogFreshnessFresh, SnapshotID: "test-catalog"}, ListingsBySection: map[string][]contracts.CatalogListing{}},
		connectors:          map[string]contracts.Connector{},
		operations:          map[string]contracts.Operation{},
		runtimeConvergences: map[string]contracts.RuntimeConvergence{},
	}
	for _, connector := range connectors {
		repository.connectors[connector.Key] = connector
	}
	return repository
}

func (repository *memoryRepository) Snapshot(_ context.Context) (contracts.Snapshot, error) {
	connectors := make([]contracts.Connector, 0, len(repository.connectors))
	for _, connector := range repository.connectors {
		connectors = append(connectors, connector)
	}
	sort.Slice(connectors, func(left, right int) bool { return connectors[left].Key < connectors[right].Key })
	operations := make([]contracts.Operation, 0, len(repository.operations))
	for _, operation := range repository.operations {
		if operation.Kind == contracts.OperationKindReconcileRuntime {
			continue
		}
		operation.Execution = contracts.OperationExecution{}
		operations = append(operations, operation)
	}
	return contracts.Snapshot{
		CatalogFreshness: repository.catalogView.Freshness,
		Connectors:       connectors,
		Operations:       operations,
		Revision:         repository.revision,
	}, nil
}

func (repository *memoryRepository) CatalogView(context.Context) (contracts.CatalogView, error) {
	view := repository.catalogView
	view.Revision = repository.revision
	return view, nil
}

func (repository *memoryRepository) BeginCatalogRefresh(context.Context, time.Time) (uint64, error) {
	repository.catalogGeneration++
	repository.catalogView.Freshness.State = contracts.CatalogFreshnessRefreshing
	return repository.catalogGeneration, nil
}

func (repository *memoryRepository) FailCatalogRefresh(_ context.Context, generation uint64, failureCode string, now time.Time) error {
	if generation != repository.catalogGeneration {
		return nil
	}
	repository.catalogView.Freshness.LastFailure = failureCode
	if repository.catalogView.Freshness.SnapshotID == "" {
		repository.catalogView.Freshness.State = contracts.CatalogFreshnessUnavailable
		return nil
	}
	repository.catalogView.Freshness.State = contracts.CatalogFreshnessStale
	if repository.catalogView.Freshness.StaleSince == nil {
		staleSince := now.UTC()
		repository.catalogView.Freshness.StaleSince = &staleSince
	}
	return nil
}

func (repository *memoryRepository) UnresolvedAuthorizationSessionOperations(_ context.Context, scope contracts.OperationScope) ([]contracts.Operation, error) {
	var operations []contracts.Operation
	for _, operation := range repository.operations {
		if operation.Kind == contracts.OperationKindStartAuthorization && operation.State == contracts.OperationStateCompleted &&
			operation.Scope == scope && operation.Execution.AuthorizationSession != nil &&
			!operation.Execution.AuthorizationSession.IsResolved() {
			operations = append(operations, operation)
		}
	}
	return operations, nil
}

func (repository *memoryRepository) ResolveAuthorizationSession(
	_ context.Context,
	operationID string,
	resolution contracts.AuthorizationSessionResolution,
) error {
	operation, ok := repository.operations[operationID]
	if !ok {
		return contracts.ErrNotFound
	}
	if operation.Execution.AuthorizationSession != nil && !operation.Execution.AuthorizationSession.IsResolved() {
		operation.Execution.AuthorizationSession.Resolution = resolution
		repository.operations[operationID] = operation
	}
	return nil
}

func (repository *memoryRepository) Connector(_ context.Context, connectorKey string) (contracts.Connector, error) {
	connector, ok := repository.connectors[connectorKey]
	if !ok {
		return contracts.Connector{}, contracts.ErrNotFound
	}
	return connector, nil
}

func (repository *memoryRepository) Operation(_ context.Context, operationID string) (contracts.Operation, error) {
	operation, ok := repository.operations[operationID]
	if !ok {
		return contracts.Operation{}, contracts.ErrNotFound
	}
	return operation, nil
}

func (repository *memoryRepository) OperationForScope(
	_ context.Context,
	scope contracts.OperationScope,
	operationID string,
) (contracts.Operation, error) {
	operation, ok := repository.operations[operationID]
	if !ok || !contracts.OperationVisibleToScope(operation, scope) {
		return contracts.Operation{}, contracts.ErrNotFound
	}
	return operation, nil
}

func (repository *memoryRepository) ClaimOperation(
	_ context.Context,
	operationID string,
	owner string,
	now time.Time,
	leaseExpiresAt time.Time,
) (contracts.Operation, bool, error) {
	operation, ok := repository.operations[operationID]
	if !ok {
		return contracts.Operation{}, false, contracts.ErrNotFound
	}
	if operation.State == contracts.OperationStateCompleted || operation.State == contracts.OperationStateFailed {
		return operation, false, nil
	}
	if operation.LeaseOwner != "" && operation.LeaseExpiresAt != nil && operation.LeaseExpiresAt.After(now) {
		return operation, false, nil
	}
	expiresAt := leaseExpiresAt
	operation.LeaseOwner = owner
	operation.LeaseToken++
	operation.LeaseExpiresAt = &expiresAt
	repository.operations[operationID] = operation
	return operation, true, nil
}

func (repository *memoryRepository) RenewOperationLease(_ context.Context, operationID, owner string, token uint64, now, leaseExpiresAt time.Time) error {
	if repository.renewOperationLeaseErr != nil {
		return repository.renewOperationLeaseErr
	}
	operation, ok := repository.operations[operationID]
	if !ok {
		return contracts.ErrNotFound
	}
	if operation.LeaseOwner != owner || operation.LeaseToken != token || operation.LeaseExpiresAt == nil || !operation.LeaseExpiresAt.After(now) {
		return contracts.ErrOperationLeaseLost
	}
	expiresAt := leaseExpiresAt
	operation.LeaseExpiresAt = &expiresAt
	repository.operations[operationID] = operation
	return nil
}

func (repository *memoryRepository) ReleaseOperationLease(_ context.Context, operationID, owner string, token uint64) error {
	operation, ok := repository.operations[operationID]
	if !ok {
		return contracts.ErrNotFound
	}
	if operation.LeaseOwner == owner && operation.LeaseToken == token {
		operation.LeaseOwner = ""
		operation.LeaseExpiresAt = nil
		repository.operations[operationID] = operation
	}
	return nil
}

func (repository *memoryRepository) InstalledRelease(_ context.Context, connectorKey, releaseDigest string) (contracts.Release, error) {
	for _, operation := range repository.operations {
		if operation.Kind == contracts.OperationKindInstall &&
			(operation.State == contracts.OperationStateCompleted || operation.Execution.ReleaseInstallation != nil) && operation.ConnectorKey == connectorKey &&
			operation.Target != nil && operation.Target.Release != nil && operation.Target.ReleaseDigest == releaseDigest {
			return *operation.Target.Release, nil
		}
	}
	connector, ok := repository.connectors[connectorKey]
	if ok && connector.Release.ReleaseDigest == releaseDigest {
		return connector.Release, nil
	}
	return contracts.Release{}, contracts.ErrNotFound
}

func (repository *memoryRepository) InstalledReleases(
	ctx context.Context,
	refs []contracts.InstalledReleaseRef,
) (map[contracts.InstalledReleaseRef]contracts.Release, error) {
	repository.installedReleasesCalls++
	result := make(map[contracts.InstalledReleaseRef]contracts.Release, len(refs))
	for _, ref := range refs {
		release, err := repository.InstalledRelease(ctx, ref.ConnectorKey, ref.ReleaseDigest)
		if errors.Is(err, contracts.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result[ref] = release
	}
	return result, nil
}

func (repository *memoryRepository) RuntimeConvergence(
	_ context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) (contracts.RuntimeConvergence, error) {
	repository.runtimeConvergenceCalls++
	convergence, ok := repository.runtimeConvergences[memoryRuntimeConvergenceKey(scope, connectorKey)]
	if !ok {
		return contracts.RuntimeConvergence{}, contracts.ErrNotFound
	}
	return convergence, nil
}

func (repository *memoryRepository) RuntimeConvergences(
	_ context.Context,
	scope contracts.OperationScope,
	limit int,
) ([]contracts.RuntimeConvergence, error) {
	repository.runtimeConvergencesCalls++
	result := make([]contracts.RuntimeConvergence, 0)
	for _, convergence := range repository.runtimeConvergences {
		if convergence.Desired.Scope == scope {
			result = append(result, convergence)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Desired.ConnectorKey < result[right].Desired.ConnectorKey
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (repository *memoryRepository) DueRuntimeConvergences(
	_ context.Context,
	scope contracts.OperationScope,
	bootEpoch string,
	now time.Time,
	limit int,
) ([]contracts.RuntimeConvergence, error) {
	var result []contracts.RuntimeConvergence
	for _, convergence := range repository.runtimeConvergences {
		if convergence.Desired.Scope != scope ||
			(convergence.Desired.Generation == convergence.Observed.DesiredGeneration && convergence.Observed.BootEpoch == bootEpoch) ||
			convergence.Attempt >= contracts.RuntimeFailureBudget ||
			convergence.NextAttemptAt.After(now) ||
			(convergence.LeaseOwner != "" && convergence.LeaseExpiresAt != nil && convergence.LeaseExpiresAt.After(now)) {
			continue
		}
		result = append(result, convergence)
		if limit > 0 && len(result) == limit {
			break
		}
	}
	return result, nil
}

func (repository *memoryRepository) ClaimRuntimeConvergence(
	_ context.Context,
	scope contracts.OperationScope,
	connectorKey, bootEpoch, owner string,
	now, leaseExpiresAt time.Time,
) (contracts.RuntimeConvergence, bool, error) {
	key := memoryRuntimeConvergenceKey(scope, connectorKey)
	convergence, ok := repository.runtimeConvergences[key]
	if !ok {
		return contracts.RuntimeConvergence{}, false, contracts.ErrNotFound
	}
	if convergence.Desired.Generation == convergence.Observed.DesiredGeneration && convergence.Observed.BootEpoch == bootEpoch ||
		convergence.Attempt >= contracts.RuntimeFailureBudget ||
		convergence.NextAttemptAt.After(now) ||
		(convergence.LeaseOwner != "" && convergence.LeaseOwner != owner && convergence.LeaseExpiresAt != nil && convergence.LeaseExpiresAt.After(now)) {
		return convergence, false, nil
	}
	expiresAt := leaseExpiresAt
	convergence.LeaseOwner = owner
	convergence.LeaseToken++
	convergence.LeaseExpiresAt = &expiresAt
	repository.runtimeConvergences[key] = convergence
	return convergence, true, nil
}

func (repository *memoryRepository) RenewRuntimeConvergenceLease(
	_ context.Context,
	scope contracts.OperationScope,
	connectorKey, owner string,
	token uint64,
	now, leaseExpiresAt time.Time,
) error {
	key := memoryRuntimeConvergenceKey(scope, connectorKey)
	convergence, ok := repository.runtimeConvergences[key]
	if !ok {
		return contracts.ErrNotFound
	}
	if convergence.LeaseOwner != owner || convergence.LeaseToken != token ||
		convergence.LeaseExpiresAt == nil || !convergence.LeaseExpiresAt.After(now) {
		return contracts.ErrOperationLeaseLost
	}
	expiresAt := leaseExpiresAt
	convergence.LeaseExpiresAt = &expiresAt
	repository.runtimeConvergences[key] = convergence
	return nil
}

func (repository *memoryRepository) ReleaseRuntimeConvergenceLease(
	_ context.Context,
	scope contracts.OperationScope,
	connectorKey, owner string,
	token uint64,
) error {
	key := memoryRuntimeConvergenceKey(scope, connectorKey)
	convergence, ok := repository.runtimeConvergences[key]
	if !ok {
		return contracts.ErrNotFound
	}
	if convergence.LeaseOwner == owner && convergence.LeaseToken == token {
		convergence.LeaseOwner = ""
		convergence.LeaseExpiresAt = nil
		repository.runtimeConvergences[key] = convergence
	}
	return nil
}

func (repository *memoryRepository) CompleteRuntimeConvergence(
	_ context.Context,
	scope contracts.OperationScope,
	connectorKey, owner string,
	token, desiredGeneration uint64,
	observed contracts.RuntimeObserved,
	now time.Time,
) error {
	key := memoryRuntimeConvergenceKey(scope, connectorKey)
	convergence, ok := repository.runtimeConvergences[key]
	if !ok {
		return contracts.ErrNotFound
	}
	if convergence.Desired.Generation != desiredGeneration || convergence.LeaseOwner != owner || convergence.LeaseToken != token ||
		convergence.LeaseExpiresAt == nil || !convergence.LeaseExpiresAt.After(now) {
		return contracts.ErrOperationLeaseLost
	}
	convergence.Observed = observed
	applyRuntimeFailureBudgetReadiness(&convergence)
	convergence.NextAttemptAt = time.Time{}
	convergence.LeaseOwner = ""
	convergence.LeaseExpiresAt = nil
	convergence.LastErrorCode = ""
	convergence.LastError = ""
	convergence.UpdatedAt = now
	repository.runtimeConvergences[key] = convergence
	return nil
}

func (repository *memoryRepository) RetryRuntimeConvergence(
	_ context.Context,
	scope contracts.OperationScope,
	connectorKey, owner string,
	token, desiredGeneration uint64,
	nextAttemptAt time.Time,
	errorCode, errorMessage string,
	now time.Time,
) error {
	key := memoryRuntimeConvergenceKey(scope, connectorKey)
	convergence, ok := repository.runtimeConvergences[key]
	if !ok {
		return contracts.ErrNotFound
	}
	if convergence.Desired.Generation != desiredGeneration || convergence.LeaseOwner != owner || convergence.LeaseToken != token ||
		convergence.LeaseExpiresAt == nil || !convergence.LeaseExpiresAt.After(now) {
		return contracts.ErrOperationLeaseLost
	}
	convergence.Attempt++
	applyRuntimeFailureBudgetReadiness(&convergence)
	convergence.NextAttemptAt = nextAttemptAt
	convergence.LeaseOwner = ""
	convergence.LeaseExpiresAt = nil
	convergence.LastErrorCode = errorCode
	convergence.LastError = errorMessage
	convergence.UpdatedAt = now
	repository.runtimeConvergences[key] = convergence
	return nil
}

func (repository *memoryRepository) Transaction(ctx context.Context, fn func(Transaction) error) error {
	if repository.rejectCanceledTransactionContext && ctx.Err() != nil {
		return ctx.Err()
	}
	repository.transactionCalls++
	if repository.failTransactionCall == repository.transactionCalls {
		if repository.failTransactionErr != nil {
			return repository.failTransactionErr
		}
		return errors.New("simulated transaction failure")
	}
	if repository.transactionErr != nil {
		err := repository.transactionErr
		repository.transactionErr = nil
		return err
	}
	transaction := &memoryTransaction{
		revision:            repository.revision,
		catalogView:         repository.catalogView,
		catalogGeneration:   repository.catalogGeneration,
		connectors:          cloneConnectors(repository.connectors),
		operations:          cloneOperations(repository.operations),
		runtimeConvergences: cloneRuntimeConvergences(repository.runtimeConvergences),
		events:              append([]contracts.ChangedEvent(nil), repository.events...),
	}
	if err := fn(transaction); err != nil {
		return err
	}
	repository.revision = transaction.revision
	repository.catalogView = transaction.catalogView
	repository.connectors = transaction.connectors
	repository.operations = transaction.operations
	repository.runtimeConvergences = transaction.runtimeConvergences
	repository.events = transaction.events
	return nil
}

func (repository *memoryRepository) RecoverableOperations(context.Context) ([]contracts.Operation, error) {
	var operations []contracts.Operation
	for _, operation := range repository.operations {
		if operation.State == contracts.OperationStateAccepted || operation.State == contracts.OperationStateRunning {
			operations = append(operations, operation)
		}
	}
	return operations, nil
}

type memoryTransaction struct {
	revision            uint64
	catalogView         contracts.CatalogView
	catalogGeneration   uint64
	connectors          map[string]contracts.Connector
	operations          map[string]contracts.Operation
	runtimeConvergences map[string]contracts.RuntimeConvergence
	events              []contracts.ChangedEvent
}

func (transaction *memoryTransaction) Revision() uint64 { return transaction.revision }

func (transaction *memoryTransaction) AdvanceRevision() uint64 {
	transaction.revision++
	return transaction.revision
}

func (transaction *memoryTransaction) Connectors() ([]contracts.Connector, error) {
	connectors := make([]contracts.Connector, 0, len(transaction.connectors))
	for _, connector := range transaction.connectors {
		connectors = append(connectors, connector)
	}
	return connectors, nil
}

func (transaction *memoryTransaction) Connector(connectorKey string) (contracts.Connector, error) {
	connector, ok := transaction.connectors[connectorKey]
	if !ok {
		return contracts.Connector{}, contracts.ErrNotFound
	}
	return connector, nil
}

func (transaction *memoryTransaction) Operation(operationID string) (contracts.Operation, error) {
	operation, ok := transaction.operations[operationID]
	if !ok {
		return contracts.Operation{}, contracts.ErrNotFound
	}
	return operation, nil
}

func (transaction *memoryTransaction) OperationByClientRequestID(ownerAccountID, clientRequestID string) (*contracts.Operation, error) {
	for _, operation := range transaction.operations {
		operation = contracts.NormalizeOperationOwnership(operation)
		if operation.OwnerAccountID == ownerAccountID && operation.ClientRequestID == clientRequestID {
			copy := operation
			return &copy, nil
		}
	}
	return nil, nil
}

func (transaction *memoryTransaction) ActiveOperation(connectorKey string) (*contracts.Operation, error) {
	for _, operation := range transaction.operations {
		if (connectorKey == "" || operation.ConnectorKey == "" || operation.ConnectorKey == connectorKey) &&
			(operation.State == contracts.OperationStateAccepted || operation.State == contracts.OperationStateRunning) {
			copy := operation
			return &copy, nil
		}
	}
	return nil, nil
}

func (transaction *memoryTransaction) CatalogFreshness() (contracts.CatalogFreshness, error) {
	return transaction.catalogView.Freshness, nil
}

func (transaction *memoryTransaction) ReplaceCatalogSnapshot(generation uint64, snapshot contracts.CatalogSnapshot, acceptedAt time.Time) (bool, error) {
	if generation != transaction.catalogGeneration {
		return false, nil
	}
	view := contracts.CatalogView{
		Freshness: contracts.CatalogFreshness{State: contracts.CatalogFreshnessFresh, SnapshotID: fmt.Sprintf("catalog-%d", generation),
			SourceRevision: snapshot.SourceRevision, AcceptedAt: timePointer(acceptedAt.UTC())},
		Categories: append([]contracts.CatalogCategory(nil), snapshot.Categories...), ListingsBySection: map[string][]contracts.CatalogListing{},
	}
	for _, entry := range snapshot.Entries {
		connector := newCatalogConnector(entry.Release)
		if projection, ok := transaction.connectors[entry.Release.ConnectorKey]; ok {
			connector.Installation = projection.Installation
			connector.Authorization = projection.Authorization
			connector.Compatibility = projection.Compatibility
			connector.Revision = projection.Revision
		}
		view.ListingsBySection[entry.SectionID] = append(view.ListingsBySection[entry.SectionID], contracts.CatalogListing{
			CategoryID: entry.CategoryID, Featured: entry.Featured, Connector: connector,
		})
	}
	transaction.catalogView = view
	return true, nil
}

func (transaction *memoryTransaction) SaveConnector(connector contracts.Connector) error {
	transaction.connectors[connector.Key] = connector
	for sectionID, listings := range transaction.catalogView.ListingsBySection {
		for index := range listings {
			if listings[index].Connector.Key == connector.Key {
				catalogRelease := listings[index].Connector.Release
				listings[index].Connector = connector
				listings[index].Connector.Release = catalogRelease
			}
		}
		transaction.catalogView.ListingsBySection[sectionID] = listings
	}
	return nil
}

func (transaction *memoryTransaction) DeleteConnector(connectorKey string) error {
	delete(transaction.connectors, connectorKey)
	return nil
}

func (transaction *memoryTransaction) SaveOperation(operation contracts.Operation) error {
	operation = contracts.NormalizeOperationOwnership(operation)
	transaction.operations[operation.OperationID] = operation
	return nil
}

func (transaction *memoryTransaction) RuntimeConvergence(scope contracts.OperationScope, connectorKey string) (contracts.RuntimeConvergence, error) {
	convergence, ok := transaction.runtimeConvergences[memoryRuntimeConvergenceKey(scope, connectorKey)]
	if !ok {
		return contracts.RuntimeConvergence{}, contracts.ErrNotFound
	}
	return convergence, nil
}

func (transaction *memoryTransaction) SaveRuntimeConvergence(convergence contracts.RuntimeConvergence) error {
	transaction.runtimeConvergences[memoryRuntimeConvergenceKey(convergence.Desired.Scope, convergence.Desired.ConnectorKey)] = convergence
	return nil
}

func (transaction *memoryTransaction) DeleteRuntimeConvergence(scope contracts.OperationScope, connectorKey string) error {
	delete(transaction.runtimeConvergences, memoryRuntimeConvergenceKey(scope, connectorKey))
	return nil
}

func (transaction *memoryTransaction) EnqueueConnectorMarketChanged(event contracts.ChangedEvent) error {
	transaction.events = append(transaction.events, event)
	return nil
}

func cloneConnectors(source map[string]contracts.Connector) map[string]contracts.Connector {
	cloned := make(map[string]contracts.Connector, len(source))
	for key, connector := range source {
		cloned[key] = connector
	}
	return cloned
}

func cloneOperations(source map[string]contracts.Operation) map[string]contracts.Operation {
	cloned := make(map[string]contracts.Operation, len(source))
	for key, operation := range source {
		cloned[key] = operation
	}
	return cloned
}

func cloneRuntimeConvergences(source map[string]contracts.RuntimeConvergence) map[string]contracts.RuntimeConvergence {
	cloned := make(map[string]contracts.RuntimeConvergence, len(source))
	for key, convergence := range source {
		cloned[key] = convergence
	}
	return cloned
}

func memoryRuntimeConvergenceKey(scope contracts.OperationScope, connectorKey string) string {
	return strings.TrimSpace(scope.AccountID) + "\x00" + strings.TrimSpace(connectorKey)
}
