package daemon

import (
	"context"
	"errors"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	marketdata "github.com/tutti-os/tutti/packages/connector/store-sqlite"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type activationGateDelegate struct {
	reconciles              int
	reconcileFailures       int
	installationInspections int
	installationState       contracts.ReleaseInstallationObservationState
	deactivations           int
	failClosed              int
	lastReconcile           contracts.RuntimeReconcileRequest
	physicalMu              sync.Mutex
	physicalRevision        uint64
	physicalRoute           *contracts.PhysicalRoute
}

func (delegate *activationGateDelegate) InspectReleaseInstallation(
	_ context.Context,
	request contracts.InspectReleaseInstallationRequest,
) (contracts.ReleaseInstallationObservation, error) {
	delegate.installationInspections++
	state := delegate.installationState
	if state == "" {
		state = contracts.ReleaseInstallationPresent
	}
	return contracts.ReleaseInstallationObservation{State: state, ConnectorKey: request.Release.ConnectorKey,
		ReleaseDigest: request.Release.ReleaseDigest}, nil
}

func (*activationGateDelegate) InstallRelease(context.Context, contracts.InstallReleaseRequest) (contracts.ReleaseInstallationReceipt, error) {
	return contracts.ReleaseInstallationReceipt{}, errors.New("not implemented")
}
func (*activationGateDelegate) CommitReleaseInstallation(context.Context, contracts.CommitReleaseInstallationRequest) error {
	return nil
}
func (*activationGateDelegate) UninstallRelease(context.Context, contracts.UninstallReleaseRequest) error {
	return nil
}

func (delegate *activationGateDelegate) Reconcile(_ context.Context, request contracts.RuntimeReconcileRequest) (contracts.RuntimeReceipt, error) {
	delegate.reconciles++
	delegate.lastReconcile = request
	if delegate.reconcileFailures > 0 {
		delegate.reconcileFailures--
		return contracts.RuntimeReceipt{}, errors.New("simulated runtime reconcile failure")
	}
	delegate.physicalMu.Lock()
	delegate.physicalRevision++
	if request.Enabled {
		route := contracts.PhysicalRoute{ConnectorKey: request.Connector.Key, ConnectionID: request.ConnectionID,
			ReleaseDigest: request.Connector.Release.ReleaseDigest, Generation: request.Generation,
			State: contracts.PhysicalRouteStateReady}
		delegate.physicalRoute = &route
	} else {
		delegate.physicalRoute = nil
	}
	delegate.physicalMu.Unlock()
	return contracts.RuntimeReceipt{OperationID: request.OperationID, ConnectionID: request.ConnectionID,
		ConnectorKey: request.Connector.Key, ReleaseDigest: request.Connector.Release.ReleaseDigest, Generation: request.Generation,
		Readiness: contracts.RuntimeReadiness{State: contracts.RuntimeReadinessReady,
			Interfaces: []contracts.InterfaceReadiness{{Kind: "mcp", State: contracts.RuntimeReadinessReady}}},
		Summary: &contracts.ConnectorSummary{Key: request.Connector.Key, Name: request.Connector.Key,
			Interfaces: []contracts.ConnectorInterfaceSummary{{Kind: "mcp", ServerName: "connector", Status: string(contracts.RuntimeReadinessReady)}}}}, nil
}

type runtimeBindingResolverFunc func(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error)

func (resolve runtimeBindingResolverFunc) ResolveRuntimeBinding(ctx context.Context, request contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
	return resolve(ctx, request)
}

type connectedAuthorizationObserver struct{}

func (connectedAuthorizationObserver) Begin(context.Context, contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	return contracts.AuthorizationSession{}, errors.New("not implemented")
}

func (connectedAuthorizationObserver) Disconnect(context.Context, contracts.AuthorizationDisconnectRequest) error {
	return errors.New("not implemented")
}

func (connectedAuthorizationObserver) Observe(_ context.Context, request contracts.AuthorizationObserveRequest) (contracts.AuthorizationObservation, error) {
	return contracts.AuthorizationObservation{
		AccountID: request.Scope.AccountID, ConnectorKey: request.Connector.Key,
		ConnectionID: "connection-1", State: contracts.AuthorizationObservationConnected,
	}, nil
}
func (delegate *activationGateDelegate) DeactivateRuntime(context.Context, contracts.RuntimeDeactivationRequest) error {
	delegate.deactivations++
	delegate.physicalMu.Lock()
	delegate.physicalRevision++
	delegate.physicalRoute = nil
	delegate.physicalMu.Unlock()
	return nil
}
func (delegate *activationGateDelegate) FailClosed(context.Context, time.Time) error {
	delegate.failClosed++
	delegate.physicalMu.Lock()
	delegate.physicalRevision++
	delegate.physicalRoute = nil
	delegate.physicalMu.Unlock()
	return nil
}
func (delegate *activationGateDelegate) Close(context.Context) error { return nil }

func (delegate *activationGateDelegate) Snapshot(context.Context) (contracts.PhysicalRouteSnapshot, error) {
	delegate.physicalMu.Lock()
	defer delegate.physicalMu.Unlock()
	snapshot := contracts.PhysicalRouteSnapshot{Revision: delegate.physicalRevision}
	if delegate.physicalRoute != nil {
		snapshot.Routes = []contracts.PhysicalRoute{*delegate.physicalRoute}
	}
	return snapshot, nil
}

func (delegate *activationGateDelegate) Watch(ctx context.Context) (contracts.PhysicalRouteWatch, error) {
	delegate.physicalMu.Lock()
	revision := delegate.physicalRevision
	delegate.physicalMu.Unlock()
	events := make(chan contracts.PhysicalRouteEvent)
	go func() {
		<-ctx.Done()
		close(events)
	}()
	return contracts.PhysicalRouteWatch{Revision: revision, Events: events}, nil
}

func TestActivationGateStagesRecoveryUntilInitialCatalogRefresh(t *testing.T) {
	delegate := &activationGateDelegate{}
	gate := newActivationGateHost(delegate)
	request := contracts.RuntimeReconcileRequest{OperationID: "recover-1", ConnectionID: "workspace-1", Enabled: true,
		Generation: contracts.HostGeneration{BootEpoch: "boot-1", Generation: 7}, Connector: contracts.Connector{Key: "github",
			Release: contracts.Release{ReleaseDigest: "release-1"}}}
	receipt, err := gate.Reconcile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if delegate.reconciles != 0 || receipt.Generation != request.Generation {
		t.Fatalf("closed gate delegated recovery: reconciles=%d receipt=%#v", delegate.reconciles, receipt)
	}
	gate.setOpen(contracts.OperationScope{}, true)
	if _, err := gate.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if delegate.reconciles != 1 {
		t.Fatalf("open gate reconciles = %d, want 1", delegate.reconciles)
	}
}

func TestActivationGateNeverStagesWorkspaceDeactivation(t *testing.T) {
	delegate := &activationGateDelegate{}
	gate := newActivationGateHost(delegate)
	if err := gate.DeactivateRuntime(context.Background(), contracts.RuntimeDeactivationRequest{ConnectionID: "workspace-1", ConnectorKey: "github"}); err != nil {
		t.Fatal(err)
	}
	if delegate.deactivations != 1 {
		t.Fatalf("deactivations = %d, want 1", delegate.deactivations)
	}
}

func TestActivationGateRejectsInactiveAccountScope(t *testing.T) {
	delegate := &activationGateDelegate{}
	gate := newActivationGateHost(delegate)
	activeScope := contracts.OperationScope{AccountID: "account-new"}
	gate.setOpen(activeScope, true)
	request := contracts.RuntimeReconcileRequest{OperationID: "late-old-account", Scope: contracts.OperationScope{AccountID: "account-old"},
		ConnectionID: "connection-old", Enabled: true, Connector: contracts.Connector{Key: "tencent-docs"}}
	if _, err := gate.Reconcile(context.Background(), request); err == nil {
		t.Fatal("inactive account runtime request was accepted")
	}
	if delegate.reconciles != 0 {
		t.Fatalf("inactive account delegated reconciles = %d", delegate.reconciles)
	}
}

type countingCatalogSource struct {
	release         contracts.Release
	refreshes       int
	refreshErr      error
	refreshObserved chan struct{}
}

func (source *countingCatalogSource) FetchSnapshot(context.Context) (contracts.CatalogSnapshot, error) {
	source.refreshes++
	if source.refreshObserved != nil {
		select {
		case source.refreshObserved <- struct{}{}:
		default:
		}
	}
	if source.refreshErr != nil {
		return contracts.CatalogSnapshot{}, source.refreshErr
	}
	return contracts.CatalogSnapshot{SourceRevision: "source-1",
		Categories: []contracts.CatalogCategory{{CategoryID: "development", Kind: "category", ItemCount: 1}},
		Entries:    []contracts.CatalogEntry{{SectionID: "development", CategoryID: "development", Release: source.release}}}, nil
}

func TestStartBootstrapsRuntimeAndRefreshesCatalog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := marketdata.Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := &countingCatalogSource{
		release: hostTestRelease(), refreshErr: errors.New("offline"), refreshObserved: make(chan struct{}, 1),
	}
	runtime := &activationGateDelegate{}
	host, err := NewHost(HostConfig{
		Repository: store, CatalogSource: source, ReleaseInstallations: runtime, ImplementationCommands: runtime,
		PhysicalRoutes: runtime,
		Authorization:  unavailableAuthorization{}, Compatibility: rejectingCompatibility{},
		ImplementationRegistry: application.NewImplementationRegistry(nil), Outbox: store, Lifecycle: store,
		Publisher: discardChangedEventPublisher{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(ctx, contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.refreshObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("catalog refresh did not start after Host.Start")
	}
	if !host.bootstrapped {
		t.Fatal("Host.Start returned before initial runtime bootstrap")
	}
	closeContext, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	if err := host.Close(closeContext); err != nil {
		t.Fatal(err)
	}
}

type discardChangedEventPublisher struct{}

func (discardChangedEventPublisher) PublishConnectorMarketChanged(context.Context, contracts.ChangedEvent) error {
	return nil
}

type recordingPublicationController struct {
	values []bool
}

func (controller *recordingPublicationController) ApplyCapabilityPublication(_ context.Context, _ contracts.OperationScope, enabled bool) error {
	controller.values = append(controller.values, enabled)
	return nil
}

func TestBootstrapRestoresInstalledRuntimeWithoutRefreshingCatalog(t *testing.T) {
	ctx := context.Background()
	store, err := marketdata.Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	release := hostTestRelease()
	// Presentation policy may evolve after installation. Runtime recovery must
	// not depend on the currently accepted icon shape.
	release.Manifest.IconURL = "https://legacy.example/icon.svg"
	connector := contracts.Connector{
		Key:     release.ConnectorKey,
		Release: release,
		Installation: contracts.Installation{
			State:                  contracts.InstallationStateInstalled,
			InstalledVersion:       release.Version,
			InstalledReleaseID:     release.ReleaseID,
			InstalledReleaseDigest: release.ReleaseDigest,
		},
		Authorization: contracts.Authorization{State: contracts.AuthorizationStateNotRequired},
		Compatibility: contracts.Compatibility{State: contracts.CompatibilityStateSupported},
	}
	installedRelease := release
	operation := contracts.Operation{
		OperationID:     "install-1",
		ClientRequestID: "install-request-1",
		ConnectorKey:    connector.Key,
		Kind:            contracts.OperationKindInstall,
		State:           contracts.OperationStateCompleted,
		Stage:           contracts.OperationStageCompleted,
		Target: &contracts.OperationTarget{
			ConnectorKey:   release.ConnectorKey,
			Version:        release.Version,
			ReleaseID:      release.ReleaseID,
			ReleaseDigest:  release.ReleaseDigest,
			ArtifactSHA256: release.Artifact.SHA256,
			Release:        &installedRelease,
		},
		CreatedAt: time.Unix(1, 0).UTC(),
		UpdatedAt: time.Unix(1, 0).UTC(),
	}
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		connector.Revision = tx.AdvanceRevision()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		return tx.SaveOperation(operation)
	}); err != nil {
		t.Fatal(err)
	}
	cleanup, err := store.CleanupLifecycle(ctx, contracts.LifecycleCleanupRequest{
		TerminalOperationsUpdatedThrough: time.Now().UTC(),
		PublishedEventsPublishedThrough:  time.Now().UTC(),
		BatchSize:                        10,
	})
	if err != nil || cleanup.TerminalOperationsDeleted != 1 {
		t.Fatalf("pre-restart lifecycle cleanup = %#v, error = %v", cleanup, err)
	}
	if _, err := store.Operation(ctx, operation.OperationID); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("terminal operation survived cleanup: %v", err)
	}

	source := &countingCatalogSource{release: release, refreshErr: errors.New("catalog returned 403")}
	runtime := &activationGateDelegate{reconcileFailures: 1}
	publication := &recordingPublicationController{}
	bindings := runtimeBindingResolverFunc(func(_ context.Context, request contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
		connectionID := "device-github"
		if request.Scope.AccountID != "" {
			connectionID = "account-" + request.Scope.AccountID
		}
		return contracts.RuntimeBinding{ConnectionID: connectionID, Enabled: true}, nil
	})
	host, err := NewHost(HostConfig{
		Repository:             store,
		CatalogSource:          source,
		ReleaseInstallations:   runtime,
		ImplementationCommands: runtime,
		PhysicalRoutes:         runtime,
		RuntimeBindings:        bindings,
		Authorization:          unavailableAuthorization{},
		Compatibility:          rejectingCompatibility{},
		ImplementationRegistry: application.NewImplementationRegistry(nil),
		Outbox:                 store,
		Lifecycle:              store,
		Publisher:              discardChangedEventPublisher{},
		Publication:            publication,
	})
	if err != nil {
		t.Fatal(err)
	}
	host.catalogRefreshInitialDelay = time.Hour
	if err := host.Start(ctx, contracts.OperationScope{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Errorf("close connector host: %v", err)
		}
	})

	if len(host.runtimeRecoveryPending) != 1 || len(publication.values) == 0 || !publication.values[len(publication.values)-1] {
		t.Fatalf("partial bootstrap pending=%#v publication=%#v", host.runtimeRecoveryPending, publication.values)
	}
	if err := host.ActivateScope(ctx, contracts.OperationScope{}); err != nil {
		t.Fatalf("degraded runtime recovery failed: %v", err)
	}
	if source.refreshes != 0 || runtime.reconciles != 2 {
		t.Fatalf("bootstrap refreshes=%d reconciles=%d, want 0 and 2", source.refreshes, runtime.reconciles)
	}
	if err := host.ActivateScope(ctx, contracts.OperationScope{AccountID: "account-1"}); err != nil {
		t.Fatalf("account bootstrap failed: %v", err)
	}
	if runtime.reconciles != 3 || runtime.lastReconcile.ConnectionID != "account-account-1" {
		t.Fatalf("account reconcile = %#v, count = %d", runtime.lastReconcile, runtime.reconciles)
	}
	if err := host.ActivateScope(ctx, contracts.OperationScope{AccountID: "account-1"}); err != nil {
		t.Fatalf("idempotent account bootstrap failed: %v", err)
	}
	if runtime.reconciles != 3 {
		t.Fatalf("unchanged account scope reconciled %d times", runtime.reconciles)
	}
	accountScope := contracts.OperationScope{AccountID: "account-1"}
	if err := host.ReconcileRuntimeForScope(ctx, accountScope, connector.Key); err != nil {
		t.Fatalf("observed runtime repair failed: %v", err)
	}
	if runtime.reconciles != 4 {
		t.Fatalf("observed runtime repair reconciles = %d, want 4", runtime.reconciles)
	}
	if len(publication.values) == 0 || !publication.values[len(publication.values)-1] {
		t.Fatalf("publication transitions = %#v, want final open", publication.values)
	}

	if err := host.refreshAndWait(ctx); err == nil || !strings.Contains(err.Error(), "refresh failed") {
		t.Fatalf("refresh error = %v, want catalog failure", err)
	}
	if source.refreshes != 1 || runtime.reconciles != 4 {
		t.Fatalf("refreshes=%d reconciles=%d, want catalog retry isolated from runtime", source.refreshes, runtime.reconciles)
	}

	if err := host.FenceForScope(ctx, accountScope); err != nil {
		t.Fatalf("account fence failed: %v", err)
	}
	if len(publication.values) == 0 || publication.values[len(publication.values)-1] || runtime.failClosed == 0 {
		t.Fatalf("fence publication=%#v failClosed=%d", publication.values, runtime.failClosed)
	}
	if err := host.ReconcileRuntimeForScope(ctx, accountScope, connector.Key); err != nil {
		t.Fatalf("closed-gate runtime repair failed: %v", err)
	}
	if runtime.reconciles != 4 {
		t.Fatalf("closed-gate runtime repair reconciles = %d, want 4", runtime.reconciles)
	}
	if err := host.ActivateScope(ctx, accountScope); err != nil {
		t.Fatalf("same-account bootstrap after fence failed: %v", err)
	}
	if runtime.reconciles != 5 || !publication.values[len(publication.values)-1] {
		t.Fatalf("same-account recovery reconciles=%d publication=%#v", runtime.reconciles, publication.values)
	}
	if runtime.installationInspections != 3 {
		t.Fatalf("installation inspections = %d, want one per full bootstrap", runtime.installationInspections)
	}

	if err := host.FenceForScope(ctx, accountScope); err != nil {
		t.Fatal(err)
	}
	runtime.installationState = contracts.ReleaseInstallationAbsent
	if err := host.ActivateScope(ctx, accountScope); err != nil {
		t.Fatalf("bootstrap with explicitly absent installation failed: %v", err)
	}
	calibrated, err := store.Connector(ctx, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if calibrated.Installation.State != contracts.InstallationStateFailed ||
		calibrated.Installation.FailureCode != application.InstallationFailureCodePhysicallyAbsent || runtime.reconciles != 5 {
		t.Fatalf("calibrated connector=%#v reconciles=%d", calibrated, runtime.reconciles)
	}
}

func TestAuthorizationRecoverySchedulesOneRuntimeBeforeResolvingReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := marketdata.Open(ctx, filepath.Join(t.TempDir(), "tuttid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	release := hostTestRelease()
	release.Manifest.AuthorizationKind = "oauth2"
	release.Manifest.RequiredCapabilities = []string{"tools"}
	release.Manifest.Implementation.ManagedStdio.Runtime.VersionRange = ">=22.0.0 <23.0.0"
	release.Manifest.Implementation.ManagedStdio.CLI = &contracts.ManagedCLIInterface{
		Entrypoint: "bin/github-cli.mjs", TimeoutMS: 120_000,
	}
	release.Manifest.Implementation.ManagedStdio.CredentialBroker = &contracts.ManagedCredentialBroker{
		Protocol: contracts.CredentialBrokerProtocolV1, Entrypoint: "authorization/broker.mjs",
		TimeoutMS: 30_000, AllowedHosts: []string{"api.example.test"},
	}
	connector := contracts.Connector{
		Key: release.ConnectorKey, Release: release,
		Installation: contracts.Installation{
			State: contracts.InstallationStateInstalled, InstalledVersion: release.Version,
			InstalledReleaseID: release.ReleaseID, InstalledReleaseDigest: release.ReleaseDigest,
		},
		Authorization: contracts.Authorization{State: contracts.AuthorizationStatePending},
		Compatibility: contracts.Compatibility{State: contracts.CompatibilityStateSupported},
	}
	authorizationOperation := contracts.Operation{
		OperationID: "authorization-1", ClientRequestID: "authorization-request-1", ConnectorKey: connector.Key,
		Kind: contracts.OperationKindStartAuthorization, Scope: contracts.OperationScope{AccountID: "account-1"},
		State: contracts.OperationStateCompleted, Stage: contracts.OperationStageCompleted,
		Target: &contracts.OperationTarget{
			ConnectorKey: release.ConnectorKey, Version: release.Version, ReleaseID: release.ReleaseID,
			ReleaseDigest: release.ReleaseDigest, Release: &release,
		},
		Execution: contracts.OperationExecution{AuthorizationSession: &contracts.AuthorizationSession{
			OperationID: "authorization-1", ConnectorKey: connector.Key, SessionID: "session-1",
			State: contracts.AuthorizationStatePending, Resolution: contracts.AuthorizationSessionResolutionUnresolved,
		}},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := store.Transaction(ctx, func(tx application.Transaction) error {
		connector.Revision = tx.AdvanceRevision()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		return tx.SaveOperation(authorizationOperation)
	}); err != nil {
		t.Fatal(err)
	}

	runtime := &activationGateDelegate{}
	scope := contracts.OperationScope{AccountID: "account-1"}
	scheduler := NewOperationScheduler(ctx)
	activationGate := newActivationGateHost(runtime)
	activationGate.setOpen(scope, true)
	composition, err := application.New(application.Config{
		Repository: store, CatalogSource: &countingCatalogSource{release: release},
		ReleaseInstallations: runtime, ImplementationCommands: activationGate,
		Authorization: connectedAuthorizationObserver{}, AuthorizationProjections: store,
		RuntimeBindings: runtimeBindingResolverFunc(func(_ context.Context, request contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
			return contracts.RuntimeBinding{ConnectionID: "device-" + request.Connector.Key, Enabled: true}, nil
		}),
		Compatibility: rejectingCompatibility{}, Scheduler: scheduler,
		ImplementationRegistry: application.NewImplementationRegistry(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Bind(composition.Daemon.Recovery); err != nil {
		t.Fatal(err)
	}
	host := &Host{
		scheduler: scheduler, activationGate: activationGate,
		authorizationMaintenance: composition.Daemon.Authorization,
		runtimeMaintenance:       composition.Daemon.Runtime,
		catalogQueries:           composition.Root.Catalog(),
	}

	host.bootstrapMu.Lock()
	err = host.reconcileAuthorizationsLocked(ctx, scope)
	host.bootstrapMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.reconciles != 1 {
		t.Fatalf("runtime reconciles = %d, want 1", runtime.reconciles)
	}
	operation, err := store.Operation(ctx, authorizationOperation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Execution.AuthorizationSession == nil ||
		operation.Execution.AuthorizationSession.Resolution != contracts.AuthorizationSessionResolutionProviderConnected {
		t.Fatalf("authorization receipt = %#v", operation.Execution.AuthorizationSession)
	}
	if err := host.ObserveAuthorizationForScope(ctx, scope, contracts.AuthorizationProjection{
		AccountID: scope.AccountID, ConnectorKey: connector.Key,
		ConnectionID: "connection-2", State: contracts.AuthorizationStateConnected,
	}); err != nil {
		t.Fatal(err)
	}
	if runtime.reconciles != 1 {
		t.Fatalf("unchanged runtime desired reconciled %d times, want 1", runtime.reconciles)
	}
	projection, err := store.AuthorizationProjection(ctx, scope.AccountID, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if projection.ConnectionID != "connection-2" || projection.State != contracts.AuthorizationStateConnected {
		t.Fatalf("live authorization projection = %#v", projection)
	}
}

func hostTestRelease() contracts.Release {
	return contracts.Release{
		SchemaVersion:  "1",
		ReleaseID:      "github@1.0.0",
		ConnectorKey:   "github",
		Version:        "1.0.0",
		ReleaseDigest:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Manifest: contracts.Manifest{
			SchemaVersion:     "1",
			DisplayName:       "GitHub",
			IconURL:           "data:image/png;base64,iVBORw0KGgo=",
			AuthorizationKind: "none",
			Implementation: contracts.Implementation{Kind: contracts.ImplementationKindManagedStdio,
				ManagedStdio: &contracts.ManagedStdioImplementation{
					Runtime: contracts.RuntimeRequirement{Language: "node", Profile: "connector-node-static", ABI: "node22-darwin-arm64"},
					MCP:     &contracts.ManagedMCPInterface{Entrypoint: "bin/github.mjs"},
				}},
		},
		Artifact: contracts.Artifact{
			SHA256:    "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			SizeBytes: 1024,
			MediaType: "application/vnd.tutti.connector+tar+gzip",
		},
		PublishedAt: time.Unix(1, 0).UTC(),
		Status:      contracts.ReleaseStatusAvailable,
	}
}
