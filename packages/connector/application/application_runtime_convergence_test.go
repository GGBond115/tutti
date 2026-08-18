package application

import (
	"context"
	"errors"
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"testing"
	"time"
)

func TestEnsureRuntimeDesiredIsLevelTriggeredAndConvergesObservedState(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{
		State:                  contracts.InstallationStateInstalled,
		InstalledVersion:       connector.Release.Version,
		InstalledReleaseID:     connector.Release.ReleaseID,
		InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(connector)
	runtime := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})

	first, err := application.EnsureRuntimeDesired(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.EnsureRuntimeDesired(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if first.Desired.Generation != 2 || second.Desired.Generation != 2 || repository.revision != 1 {
		t.Fatalf("desired generations = %d, %d; revision = %d", first.Desired.Generation, second.Desired.Generation, repository.revision)
	}
	if err := application.ConvergeRuntime(context.Background(), contracts.OperationScope{}, connector.Key); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.RuntimeConvergence(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Observed.DesiredGeneration != first.Desired.Generation ||
		stored.Observed.BootEpoch != application.config.BootEpoch ||
		stored.Observed.ReleaseDigest != connector.Release.ReleaseDigest ||
		stored.Observed.Readiness.State != contracts.RuntimeReadinessReady {
		t.Fatalf("observed convergence = %#v", stored.Observed)
	}
	if runtime.reconciles != 1 || runtime.lastReconcile.Generation.Generation != first.Desired.Generation {
		t.Fatalf("runtime reconciles = %d, request = %#v", runtime.reconciles, runtime.lastReconcile)
	}
	due, err := application.DueRuntimeConvergences(context.Background(), contracts.OperationScope{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("converged runtime remained due: %#v", due)
	}
}

func TestRuntimeConvergenceFailureRemainsRetryableDebt(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{
		State:                  contracts.InstallationStateInstalled,
		InstalledVersion:       connector.Release.Version,
		InstalledReleaseID:     connector.Release.ReleaseID,
		InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(connector)
	runtimeFailure := errors.New("runtime temporarily unavailable")
	runtime := &memoryInstallRuntime{reconcileErrors: map[string]error{connector.Key: runtimeFailure}}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	if _, err := application.EnsureRuntimeDesired(context.Background(), contracts.OperationScope{}, connector.Key); err != nil {
		t.Fatal(err)
	}
	if err := application.ConvergeRuntime(context.Background(), contracts.OperationScope{}, connector.Key); !errors.Is(err, runtimeFailure) {
		t.Fatalf("converge error = %v", err)
	}
	stored, err := repository.RuntimeConvergence(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Attempt != 1 || stored.Observed.DesiredGeneration != 0 || stored.LastErrorCode != string(contracts.ErrorCodeInstallFailed) ||
		!stored.NextAttemptAt.After(application.config.Now()) {
		t.Fatalf("retryable convergence = %#v", stored)
	}
}

func TestRuntimeConvergencePersistsFullJitterAndStopsAtFailureBudget(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest}
	repository := newMemoryRepository(connector)
	runtimeFailure := errors.New("runtime permanently unavailable")
	runtime := &memoryInstallRuntime{reconcileErrors: map[string]error{connector.Key: runtimeFailure}}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	now := application.config.Now()
	application.config.Now = func() time.Time { return now }
	application.config.RuntimeRetryJitter = func(maximum time.Duration) time.Duration { return maximum / 2 }
	initial, err := application.EnsureRuntimeDesired(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := uint32(1); attempt <= contracts.RuntimeFailureBudget; attempt++ {
		attemptStartedAt := now
		if err := application.ConvergeRuntime(context.Background(), contracts.OperationScope{}, connector.Key); !errors.Is(err, runtimeFailure) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
		stored, readErr := repository.RuntimeConvergence(context.Background(), contracts.OperationScope{}, connector.Key)
		if readErr != nil {
			t.Fatal(readErr)
		}
		wantDeadline := attemptStartedAt.Add(runtimeConvergenceBackoff(attempt) / 2)
		if stored.Attempt != attempt || !stored.NextAttemptAt.Equal(wantDeadline) {
			t.Fatalf("attempt %d persisted convergence = %#v, want deadline %s", attempt, stored, wantDeadline)
		}
		if attempt == contracts.RuntimeFailureDegradedThreshold &&
			(stored.Observed.Readiness.State != contracts.RuntimeReadinessDegraded ||
				stored.Observed.Readiness.ReasonCode != contracts.RuntimeReadinessReasonFailureBudgetDegraded) {
			t.Fatalf("degraded threshold readiness = %#v", stored.Observed.Readiness)
		}
		if attempt == contracts.RuntimeFailureBudget &&
			(stored.Observed.Readiness.State != contracts.RuntimeReadinessFailed ||
				stored.Observed.Readiness.ReasonCode != contracts.RuntimeReadinessReasonFailureBudgetExhausted) {
			t.Fatalf("exhausted threshold readiness = %#v", stored.Observed.Readiness)
		}
		now = stored.NextAttemptAt
	}
	if err := application.ConvergeRuntime(context.Background(), contracts.OperationScope{}, connector.Key); err != nil {
		t.Fatalf("suppressed convergence returned error = %v", err)
	}
	if runtime.reconciles != int(contracts.RuntimeFailureBudget) {
		t.Fatalf("runtime starts after exhaustion = %d, want %d", runtime.reconciles, contracts.RuntimeFailureBudget)
	}
	due, err := application.DueRuntimeConvergences(context.Background(), contracts.OperationScope{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("exhausted generation remained due = %#v", due)
	}
	reset, err := application.ensureRuntimeDesired(context.Background(), contracts.OperationScope{}, connector.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if reset.Desired.Generation <= initial.Desired.Generation || reset.Attempt != 0 {
		t.Fatalf("new desired generation did not reset failure budget = %#v", reset)
	}
	due, err = application.DueRuntimeConvergences(context.Background(), contracts.OperationScope{}, 10)
	if err != nil || len(due) != 1 || due[0].Desired.Generation != reset.Desired.Generation {
		t.Fatalf("reset generation due = %#v, error = %v", due, err)
	}
}

func TestInvalidateRuntimeObservationEnqueuesWithoutReconcilingInline(t *testing.T) {
	connector := testConnector("github")
	connector.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	repository := newMemoryRepository(connector)
	runtime := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	convergence, err := application.EnsureRuntimeDesired(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ConvergeRuntime(context.Background(), contracts.OperationScope{}, connector.Key); err != nil {
		t.Fatal(err)
	}
	if err := application.InvalidateRuntimeObservation(
		context.Background(), contracts.OperationScope{}, connector.Key, convergence.Desired.Generation,
	); err != nil {
		t.Fatal(err)
	}
	stored, err := application.RuntimeConvergenceState(context.Background(), contracts.OperationScope{}, connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Desired.Generation != convergence.Desired.Generation+1 ||
		stored.Observed.DesiredGeneration != convergence.Desired.Generation || runtime.reconciles != 1 {
		t.Fatalf("invalidated convergence = %#v; runtime reconciles = %d", stored, runtime.reconciles)
	}
	due, err := application.DueRuntimeConvergences(context.Background(), contracts.OperationScope{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Desired.Generation != stored.Desired.Generation {
		t.Fatalf("due convergence = %#v", due)
	}
}

func TestRuntimeConvergenceSnapshotUsesOneBoundedScopeRead(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	for index := 0; index < 100; index++ {
		connectorKey := fmt.Sprintf("connector-%03d", index)
		repository.runtimeConvergences[memoryRuntimeConvergenceKey(contracts.OperationScope{}, connectorKey)] = contracts.RuntimeConvergence{
			Desired: contracts.RuntimeDesired{ConnectorKey: connectorKey, Generation: 1},
		}
	}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	repository.runtimeConvergenceCalls = 0
	repository.runtimeConvergencesCalls = 0
	snapshot, err := application.RuntimeConvergenceSnapshot(context.Background(), contracts.OperationScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 100 || repository.runtimeConvergencesCalls != 1 || repository.runtimeConvergenceCalls != 0 {
		t.Fatalf("snapshot rows=%d batch reads=%d point reads=%d",
			len(snapshot), repository.runtimeConvergencesCalls, repository.runtimeConvergenceCalls)
	}
}

func TestRuntimeConvergenceSnapshotFailsClosedAboveBound(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	for index := 0; index <= maxRuntimeConvergenceSnapshot; index++ {
		connectorKey := fmt.Sprintf("connector-%04d", index)
		repository.runtimeConvergences[memoryRuntimeConvergenceKey(contracts.OperationScope{}, connectorKey)] = contracts.RuntimeConvergence{
			Desired: contracts.RuntimeDesired{ConnectorKey: connectorKey, Generation: 1},
		}
	}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	if _, err := application.RuntimeConvergenceSnapshot(context.Background(), contracts.OperationScope{}); err == nil {
		t.Fatal("oversized runtime convergence snapshot unexpectedly succeeded")
	}
}

func TestUpdateKeepsCurrentReleaseUntilCandidateRuntimeIsObserved(t *testing.T) {
	connector := testConnector("github")
	oldRelease := connector.Release
	connector.Installation = contracts.Installation{
		State: contracts.InstallationStateInstalled, InstalledVersion: oldRelease.Version,
		InstalledReleaseID: oldRelease.ReleaseID, InstalledReleaseDigest: oldRelease.ReleaseDigest,
	}
	connector.Release.Version = "2.0.0"
	connector.Release.ReleaseID = "github@2.0.0"
	connector.Release.ReleaseDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	connector.Release.ManifestDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	repository := newMemoryRepository(connector)
	// Retain immutable evidence for the previously committed release.
	repository.operations["old-install"] = contracts.Operation{
		OperationID: "old-install", ConnectorKey: connector.Key, Kind: contracts.OperationKindInstall,
		State: contracts.OperationStateCompleted, Target: &contracts.OperationTarget{
			ConnectorKey: connector.Key, ReleaseDigest: oldRelease.ReleaseDigest, Release: &oldRelease,
		},
	}
	runtimeFailure := errors.New("candidate runtime unavailable")
	runtime := &memoryInstallRuntime{reconcileErrors: map[string]error{connector.Key: runtimeFailure}}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	now := application.config.Now()
	application.config.Now = func() time.Time { return now }
	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "update-github"}, ConnectorKey: connector.Key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); !errors.Is(err, runtimeFailure) {
		t.Fatalf("update error = %v", err)
	}
	during, _ := repository.Connector(context.Background(), connector.Key)
	operation, _ := repository.Operation(context.Background(), accepted.Operation.OperationID)
	if during.Installation.State != contracts.InstallationStateUpdating ||
		during.Installation.InstalledReleaseDigest != oldRelease.ReleaseDigest ||
		during.Installation.CandidateReleaseDigest != connector.Release.ReleaseDigest ||
		operation.State != contracts.OperationStateRunning || operation.Stage != contracts.OperationStageRuntimePending {
		t.Fatalf("update debt lost current/candidate truth: connector=%#v operation=%#v", during, operation)
	}
	delete(runtime.reconcileErrors, connector.Key)
	now = now.Add(10 * time.Second)
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	completed, _ := repository.Connector(context.Background(), connector.Key)
	operation, _ = repository.Operation(context.Background(), accepted.Operation.OperationID)
	if completed.Installation.State != contracts.InstallationStateInstalled ||
		completed.Installation.InstalledReleaseDigest != connector.Release.ReleaseDigest ||
		completed.Installation.CandidateReleaseDigest != "" || operation.State != contracts.OperationStateCompleted {
		t.Fatalf("update did not promote observed candidate: connector=%#v operation=%#v", completed, operation)
	}
}

func TestAuthorizedUpdateInspectsPreparedCandidateBeforePromotion(t *testing.T) {
	connector := testManagedAuthorizedConnector("lark-cli")
	connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateConnected}
	oldRelease := connector.Release
	connector.Release.Version = "2.0.0"
	connector.Release.ReleaseID = "lark-cli@2.0.0"
	connector.Release.ReleaseDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	connector.Release.ManifestDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	repository := newMemoryRepository(connector)
	repository.operations["old-install"] = contracts.Operation{
		OperationID: "old-install", ConnectorKey: connector.Key, Kind: contracts.OperationKindInstall,
		State: contracts.OperationStateCompleted, Target: &contracts.OperationTarget{
			ConnectorKey: connector.Key, ReleaseDigest: oldRelease.ReleaseDigest, Release: &oldRelease,
		},
	}
	runtime := &memoryInstallRuntime{}
	application := newTestApplication(t, repository, &memoryScheduler{}, runtime, contracts.CatalogSnapshot{})
	inspector := &candidateAuthorizationInspector{}
	application.config.Authorization = inspector

	accepted, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "update-authorized-lark"}, ConnectorKey: connector.Key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ExecuteOperation(context.Background(), accepted.Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	if len(inspector.requests) != 1 {
		t.Fatalf("authorization inspections = %d, want 1", len(inspector.requests))
	}
	inspected := inspector.requests[0].Connector
	if inspected.Installation.State != contracts.InstallationStateUpdating ||
		inspected.Installation.CandidateReleaseDigest != connector.Release.ReleaseDigest ||
		inspected.Release.ReleaseDigest != connector.Release.ReleaseDigest {
		t.Fatalf("inspected candidate connector = %#v", inspected)
	}
	completed, err := repository.Connector(context.Background(), connector.Key)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Installation.State != contracts.InstallationStateInstalled ||
		completed.Installation.InstalledReleaseDigest != connector.Release.ReleaseDigest {
		t.Fatalf("completed authorized update = %#v", completed.Installation)
	}
}

type candidateAuthorizationInspector struct {
	authorizationProviderStub
	requests []contracts.AuthorizationInspectRequest
}

func (inspector *candidateAuthorizationInspector) InspectAuthorization(
	_ context.Context,
	request contracts.AuthorizationInspectRequest,
) (contracts.AuthorizationObservation, error) {
	inspector.requests = append(inspector.requests, request)
	return contracts.AuthorizationObservation{
		State: contracts.AuthorizationObservationConnected, ConnectorKey: request.Connector.Key,
		ReleaseDigest: request.Connector.Release.ReleaseDigest, ConnectionID: defaultConnectorConnectionID,
	}, nil
}
