package application

import (
	"context"
	"errors"
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"math"
	"strings"
	"time"
)

func (application *service) prepareInstallRuntimeDesired(
	ctx context.Context,
	operationID string,
	release contracts.Release,
	binding contracts.RuntimeBinding,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted {
			return nil
		}
		connector, err := tx.Connector(operation.ConnectorKey)
		if err != nil {
			return err
		}
		revision := tx.AdvanceRevision()
		connector.Installation.CandidateVersion = release.Version
		connector.Installation.CandidateReleaseID = release.ReleaseID
		connector.Installation.CandidateReleaseDigest = release.ReleaseDigest
		connector.Installation.FailureCode = ""
		connector.Revision = revision
		operation.State = contracts.OperationStateRunning
		operation.Stage = contracts.OperationStageRuntimePending
		operation.FailureCode = ""
		operation.UpdatedAt = application.config.Now().UTC()
		if _, _, err := upsertRuntimeDesired(
			tx, operation.Scope, connector.Key, release.ReleaseDigest, binding, nextGeneration(revision), false, operation.UpdatedAt,
		); err != nil {
			return err
		}
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connector.Key,
			OperationID:  operation.OperationID,
			Revision:     revision,
		})
	})
}

func (application *service) finalizeInstallAfterRuntime(
	ctx context.Context,
	operationID string,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted {
			return nil
		}
		connector, err := tx.Connector(operation.ConnectorKey)
		if err != nil {
			return err
		}
		if operation.Target == nil || connector.Installation.CandidateReleaseDigest != operation.Target.ReleaseDigest {
			return contracts.NewDomainError(contracts.ErrorCodeRevisionConflict, "connector install candidate changed before completion", true, nil)
		}
		convergence, err := tx.RuntimeConvergence(operation.Scope, connector.Key)
		if err != nil {
			return err
		}
		if convergence.Desired.ReleaseDigest != operation.Target.ReleaseDigest ||
			convergence.Observed.DesiredGeneration != convergence.Desired.Generation ||
			convergence.Observed.BootEpoch != application.config.BootEpoch {
			return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector runtime candidate is not observed", true, nil)
		}
		revision := tx.AdvanceRevision()
		connector.Installation.State = contracts.InstallationStateInstalled
		connector.Installation.InstalledVersion = connector.Installation.CandidateVersion
		connector.Installation.InstalledReleaseID = connector.Installation.CandidateReleaseID
		connector.Installation.InstalledReleaseDigest = connector.Installation.CandidateReleaseDigest
		connector.Installation.CandidateVersion = ""
		connector.Installation.CandidateReleaseID = ""
		connector.Installation.CandidateReleaseDigest = ""
		connector.Installation.FailureCode = ""
		connector.Revision = revision
		operation.State = contracts.OperationStateCompleted
		operation.Stage = contracts.OperationStageCompleted
		operation.FailureCode = ""
		operation.UpdatedAt = application.config.Now().UTC()
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connector.Key, OperationID: operation.OperationID, Revision: revision,
		})
	})
}

func (application *service) prepareUninstallRuntimeDisabled(
	ctx context.Context,
	operationID string,
	release contracts.Release,
	binding contracts.RuntimeBinding,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		connector, err := tx.Connector(operation.ConnectorKey)
		if err != nil {
			return err
		}
		if connector.Installation.State != contracts.InstallationStateUninstalling ||
			connector.Installation.InstalledReleaseDigest != release.ReleaseDigest {
			return contracts.NewDomainError(contracts.ErrorCodeRevisionConflict, "connector uninstall target changed", true, nil)
		}
		binding.Enabled = false
		now := application.config.Now().UTC()
		revision := tx.AdvanceRevision()
		if _, _, err := upsertRuntimeDesired(
			tx, operation.Scope, connector.Key, release.ReleaseDigest, binding, nextGeneration(revision), false, now,
		); err != nil {
			return err
		}
		connector.Revision = revision
		operation.State = contracts.OperationStateRunning
		operation.Stage = contracts.OperationStageDeactivating
		operation.UpdatedAt = now
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connector.Key, OperationID: operation.OperationID, Revision: revision,
		})
	})
}

// EnsureRuntimeDesired derives and durably records the current runtime intent
// without issuing or persisting a credential grant. Repeating the same intent
// is a no-op; a changed intent advances only the Connector's convergence
// generation.
func (application *service) EnsureRuntimeDesired(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) (contracts.RuntimeConvergence, error) {
	return application.ensureRuntimeDesired(ctx, scope, connectorKey, false)
}

func (application *service) ensureRuntimeDesired(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
	forceNewGeneration bool,
) (contracts.RuntimeConvergence, error) {
	connectorKey = strings.TrimSpace(connectorKey)
	scope.AccountID = strings.TrimSpace(scope.AccountID)
	if connectorKey == "" {
		return contracts.RuntimeConvergence{}, invalidRequest("connectorKey is required")
	}
	connector, release, err := application.runtimeConnectorAndRelease(ctx, connectorKey)
	if err != nil {
		return contracts.RuntimeConvergence{}, err
	}
	binding, err := application.resolveRuntimeBinding(ctx, contracts.Operation{
		OperationID:  "plan-runtime/" + connectorKey,
		ConnectorKey: connectorKey,
		Scope:        scope,
	}, connector, release, contracts.RuntimeBindingPurposePlan)
	if err != nil {
		return contracts.RuntimeConvergence{}, err
	}
	defer clear(binding.CredentialBrokerGrant)
	if len(binding.CredentialBrokerGrant) != 0 {
		return contracts.RuntimeConvergence{}, invalidOperationReceipt("runtime planning returned a credential grant")
	}
	return application.saveRuntimeDesired(ctx, scope, connectorKey, release.ReleaseDigest, binding, forceNewGeneration)
}

// DueRuntimeConvergences returns private, level-triggered work for the active
// scope. Callers use it only as a scheduling hint; ClaimRuntimeConvergence
// rechecks every due predicate atomically.
func (application *service) DueRuntimeConvergences(
	ctx context.Context,
	scope contracts.OperationScope,
	limit int,
) ([]contracts.RuntimeConvergence, error) {
	return application.config.Repository.DueRuntimeConvergences(
		ctx, scope, application.config.BootEpoch, application.config.Now().UTC(), limit,
	)
}

// RuntimeConvergenceState exposes one private convergence row to the daemon's
// physical anti-entropy worker. It is not part of public Connector snapshots.
func (application *service) RuntimeConvergenceState(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) (contracts.RuntimeConvergence, error) {
	return application.config.Repository.RuntimeConvergence(ctx, scope, strings.TrimSpace(connectorKey))
}

const maxRuntimeConvergenceSnapshot = 4096

// RuntimeConvergenceSnapshot returns one bounded, scope-wide private read for
// physical anti-entropy. The extra row turns silent truncation into an explicit
// fail-closed error.
func (application *service) RuntimeConvergenceSnapshot(
	ctx context.Context,
	scope contracts.OperationScope,
) ([]contracts.RuntimeConvergence, error) {
	convergences, err := application.config.Repository.RuntimeConvergences(
		ctx, scope, maxRuntimeConvergenceSnapshot+1,
	)
	if err != nil {
		return nil, err
	}
	if len(convergences) > maxRuntimeConvergenceSnapshot {
		return nil, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "runtime convergence snapshot exceeds limit", true, nil)
	}
	return convergences, nil
}

func (application *service) RuntimeBootEpoch() string {
	if application == nil {
		return ""
	}
	return application.config.BootEpoch
}

// InvalidateRuntimeObservation turns a matching cached Observed receipt back
// into level-triggered work without executing the runtime command inline. The
// expected generation is a CAS guard against an exit event racing newer intent.
func (application *service) InvalidateRuntimeObservation(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
	expectedGeneration uint64,
) error {
	connectorKey = strings.TrimSpace(connectorKey)
	if connectorKey == "" || expectedGeneration == 0 {
		return invalidRequest("runtime invalidation identity is required")
	}
	now := application.config.Now().UTC()
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		convergence, err := tx.RuntimeConvergence(scope, connectorKey)
		if err != nil {
			return err
		}
		if convergence.Desired.Generation != expectedGeneration ||
			convergence.Observed.DesiredGeneration != expectedGeneration ||
			convergence.Observed.BootEpoch != application.config.BootEpoch {
			return nil
		}
		if expectedGeneration == math.MaxUint64 {
			return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "runtime desired generation is exhausted", false, nil)
		}
		convergence.Desired.Generation++
		convergence.Desired.UpdatedAt = now
		convergence.Attempt++
		applyRuntimeFailureBudgetReadiness(&convergence)
		convergence.NextAttemptAt = now
		convergence.LeaseOwner = ""
		convergence.LeaseExpiresAt = nil
		convergence.LeaseToken++
		convergence.LastErrorCode = string(contracts.ErrorCodeUnavailable)
		convergence.LastError = "physical runtime route was lost"
		convergence.UpdatedAt = now
		if err := tx.SaveRuntimeConvergence(convergence); err != nil {
			return err
		}
		revision := tx.AdvanceRevision()
		connector, err := tx.Connector(connectorKey)
		if err != nil {
			return err
		}
		connector.Revision = revision
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: connectorKey, Revision: revision})
	})
}

func applyRuntimeFailureBudgetReadiness(convergence *contracts.RuntimeConvergence) {
	if convergence == nil {
		return
	}
	switch {
	case convergence.Attempt >= contracts.RuntimeFailureBudget:
		convergence.Observed.Readiness.State = contracts.RuntimeReadinessFailed
		convergence.Observed.Readiness.ReasonCode = contracts.RuntimeReadinessReasonFailureBudgetExhausted
		convergence.Observed.Readiness.Interfaces = nil
	case convergence.Attempt >= contracts.RuntimeFailureDegradedThreshold:
		convergence.Observed.Readiness.State = contracts.RuntimeReadinessDegraded
		convergence.Observed.Readiness.ReasonCode = contracts.RuntimeReadinessReasonFailureBudgetDegraded
	}
}

// ResetRuntimeFailureBudget records that an exact current-generation physical
// route survived until a level-triggered anti-entropy observation. Watch edge
// hints do not call this method, so an activation event cannot instantly erase
// an early-exit history.
func (application *service) ResetRuntimeFailureBudget(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
	expectedGeneration uint64,
) error {
	connectorKey = strings.TrimSpace(connectorKey)
	if connectorKey == "" || expectedGeneration == 0 {
		return invalidRequest("runtime failure budget reset identity is required")
	}
	now := application.config.Now().UTC()
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		convergence, err := tx.RuntimeConvergence(scope, connectorKey)
		if err != nil {
			return err
		}
		if convergence.Desired.Generation != expectedGeneration ||
			convergence.Observed.DesiredGeneration != expectedGeneration ||
			convergence.Observed.BootEpoch != application.config.BootEpoch || convergence.Attempt == 0 {
			return nil
		}
		convergence.Attempt = 0
		convergence.NextAttemptAt = time.Time{}
		convergence.LastErrorCode = ""
		convergence.LastError = ""
		convergence.Observed.Readiness.State = contracts.RuntimeReadinessReady
		convergence.Observed.Readiness.ReasonCode = ""
		convergence.UpdatedAt = now
		return tx.SaveRuntimeConvergence(convergence)
	})
}

// ReconcileRuntimeDesired synchronously proves that the latest Desired is
// observed by this boot. If another worker owns the lease, it waits for that
// worker rather than creating a second public operation.
func (application *service) ReconcileRuntimeDesired(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) error {
	if _, err := application.EnsureRuntimeDesired(ctx, scope, connectorKey); err != nil {
		return err
	}
	return application.awaitRuntimeDesired(ctx, scope, connectorKey)
}

func (application *service) reconcileRuntimeDesiredAfterFence(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) error {
	if _, err := application.ensureRuntimeDesired(ctx, scope, connectorKey, true); err != nil {
		return err
	}
	return application.awaitRuntimeDesired(ctx, scope, connectorKey)
}

// ReconcileRuntimeAfterInvalidation advances the Desired generation even when
// its payload is unchanged. Runtime-exit and route-loss observers use this to
// invalidate an otherwise matching Observed receipt.
func (application *service) ReconcileRuntimeAfterInvalidation(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) error {
	return application.reconcileRuntimeDesiredAfterFence(ctx, scope, connectorKey)
}

func (application *service) awaitRuntimeDesired(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := application.ConvergeRuntime(ctx, scope, connectorKey); err != nil {
			return err
		}
		convergence, err := application.config.Repository.RuntimeConvergence(ctx, scope, connectorKey)
		if err != nil {
			return err
		}
		if convergence.Observed.DesiredGeneration == convergence.Desired.Generation &&
			convergence.Observed.BootEpoch == application.config.BootEpoch {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ConvergeRuntime applies one durable Desired generation. Failures remain
// retryable convergence debt instead of becoming public terminal Operations.
func (application *service) ConvergeRuntime(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) (executeErr error) {
	now := application.config.Now().UTC()
	convergence, claimed, err := application.config.Repository.ClaimRuntimeConvergence(
		ctx, scope, connectorKey, application.config.BootEpoch, application.config.WorkerID,
		now, now.Add(application.config.LeaseDuration),
	)
	if err != nil || !claimed {
		return err
	}
	executionContext, cancelExecution := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go application.renewRuntimeConvergenceLease(executionContext, cancelExecution, convergence, heartbeatDone)
	defer func() {
		cancelExecution()
		heartbeatErr := <-heartbeatDone
		if heartbeatErr != nil {
			executeErr = errors.Join(executeErr, heartbeatErr)
		}
		_ = application.config.Repository.ReleaseRuntimeConvergenceLease(
			context.WithoutCancel(ctx), convergence.Desired.Scope, convergence.Desired.ConnectorKey,
			application.config.WorkerID, convergence.LeaseToken,
		)
	}()

	connector, release, err := application.runtimeConnectorAndReleaseForDigest(
		executionContext, convergence.Desired.ConnectorKey, convergence.Desired.ReleaseDigest, convergence.Desired.Enabled,
	)
	if err != nil {
		return application.retryRuntimeConvergence(ctx, convergence, err)
	}
	if release.ReleaseDigest != convergence.Desired.ReleaseDigest {
		return application.retryRuntimeConvergence(ctx, convergence,
			contracts.NewDomainError(contracts.ErrorCodeRevisionConflict, "installed connector changed during runtime convergence", true, nil))
	}
	operationID := fmt.Sprintf("runtime/%s/%s/%d", application.config.BootEpoch,
		convergence.Desired.ConnectorKey, convergence.Desired.Generation)
	operation := contracts.Operation{OperationID: operationID, ConnectorKey: connector.Key, Scope: convergence.Desired.Scope}
	binding := contracts.RuntimeBinding{
		ConnectionID: convergence.Desired.ConnectionID, Enabled: false,
		AuthorizationState: convergence.Desired.AuthorizationState,
	}
	if convergence.Desired.Enabled {
		connector, err = application.inspectRuntimeAuthorization(executionContext, convergence, connector)
		if err != nil {
			return application.retryRuntimeConvergence(ctx, convergence, err)
		}
		binding, err = application.resolveRuntimeBinding(executionContext, operation, connector, release, contracts.RuntimeBindingPurposeReconcile)
		if err != nil {
			return application.retryRuntimeConvergence(ctx, convergence, err)
		}
	}
	defer clear(binding.CredentialBrokerGrant)
	if !runtimeBindingMatchesDesired(binding, convergence.Desired) {
		clear(binding.CredentialBrokerGrant)
		_, saveErr := application.saveRuntimeDesired(
			context.WithoutCancel(ctx), convergence.Desired.Scope, connector.Key, release.ReleaseDigest, binding, false,
		)
		return saveErr
	}
	connector.Authorization.State = binding.AuthorizationState
	generation := contracts.HostGeneration{BootEpoch: application.config.BootEpoch, Generation: convergence.Desired.Generation}
	receipt, err := application.reconcileRuntime(executionContext, contracts.RuntimeReconcileRequest{
		OperationID: operationID, Scope: convergence.Desired.Scope, ConnectionID: binding.ConnectionID,
		Connector: connector, Enabled: binding.Enabled, Generation: generation,
		CredentialBrokerGrant: binding.CredentialBrokerGrant,
	})
	if err != nil {
		return application.retryRuntimeConvergence(ctx, convergence,
			contracts.NewDomainError(contracts.ErrorCodeInstallFailed, "connector runtime could not be reconciled", true, err))
	}
	if err := validateRuntimeReceipt(receipt, operationID, binding.ConnectionID, connector.Key,
		release.ReleaseDigest, generation, binding.Enabled); err != nil {
		return application.retryRuntimeConvergence(ctx, convergence, err)
	}
	observedAt := application.config.Now().UTC()
	observed := contracts.RuntimeObserved{
		DesiredGeneration: convergence.Desired.Generation,
		BootEpoch:         application.config.BootEpoch,
		Enabled:           binding.Enabled,
		ConnectionID:      binding.ConnectionID,
		ReleaseDigest:     release.ReleaseDigest,
		Readiness:         receipt.Readiness,
		Summary:           receipt.Summary,
		ObservedAt:        observedAt,
	}
	return application.config.Repository.CompleteRuntimeConvergence(
		context.WithoutCancel(ctx), convergence.Desired.Scope, connector.Key, application.config.WorkerID,
		convergence.LeaseToken, convergence.Desired.Generation, observed, observedAt,
	)
}

func (application *service) inspectRuntimeAuthorization(
	ctx context.Context,
	convergence contracts.RuntimeConvergence,
	connector contracts.Connector,
) (contracts.Connector, error) {
	if connector.Release.Manifest.Implementation.ManagedStdio == nil ||
		connector.Release.Manifest.AuthorizationKind == "none" {
		return connector, nil
	}
	inspector, ok := application.config.Authorization.(AuthorizationInspector)
	if !ok {
		return connector, nil
	}
	observation, err := inspector.InspectAuthorization(ctx, contracts.AuthorizationInspectRequest{
		Scope: convergence.Desired.Scope, Connector: connector,
		AuthorizationGeneration: convergence.Desired.Generation,
		DesktopBootEpoch:        application.config.BootEpoch,
		StateRevision:           connector.Revision,
	})
	if err != nil {
		return contracts.Connector{}, fmt.Errorf("inspect connector authorization: %w", err)
	}
	if observation.ConnectorKey != "" && observation.ConnectorKey != connector.Key ||
		observation.ReleaseDigest != "" && observation.ReleaseDigest != connector.Release.ReleaseDigest {
		return contracts.Connector{}, invalidOperationReceipt("authorization inspector returned a mismatched observation")
	}
	var state contracts.AuthorizationState
	switch observation.State {
	case contracts.AuthorizationObservationConnected:
		state = contracts.AuthorizationStateConnected
	case contracts.AuthorizationObservationDisconnected:
		state = contracts.AuthorizationStateDisconnected
	case contracts.AuthorizationObservationExpired:
		state = contracts.AuthorizationStateExpired
	case contracts.AuthorizationObservationFailed:
		state = contracts.AuthorizationStateFailed
	case contracts.AuthorizationObservationPending:
		state = contracts.AuthorizationStatePending
	default:
		return contracts.Connector{}, invalidOperationReceipt("authorization inspector returned an invalid state")
	}
	if connector.Authorization.State != state || connector.Authorization.FailureCode != observation.FailureCode {
		err = application.config.Repository.Transaction(ctx, func(tx Transaction) error {
			stored, txErr := tx.Connector(connector.Key)
			if txErr != nil {
				return txErr
			}
			revision := tx.AdvanceRevision()
			stored.Authorization = contracts.Authorization{State: state, FailureCode: strings.TrimSpace(observation.FailureCode)}
			stored.Revision = revision
			if txErr := tx.SaveConnector(stored); txErr != nil {
				return txErr
			}
			return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: stored.Key, Revision: revision})
		})
		if err != nil {
			return contracts.Connector{}, err
		}
		connector.Authorization = contracts.Authorization{State: state, FailureCode: strings.TrimSpace(observation.FailureCode)}
	}
	if application.config.AuthorizationProjections != nil && strings.TrimSpace(convergence.Desired.Scope.AccountID) != "" {
		connectionID := strings.TrimSpace(observation.ConnectionID)
		if state == contracts.AuthorizationStateConnected && connectionID == "" {
			return contracts.Connector{}, invalidOperationReceipt("connected authorization inspection returned no connection id")
		}
		if err := application.saveAuthorizationProjection(ctx, contracts.ConnectorMutation{
			ConnectorKey: connector.Key, AccountID: convergence.Desired.Scope.AccountID,
		}, contracts.AuthorizationProjection{
			AccountID: convergence.Desired.Scope.AccountID, ConnectorKey: connector.Key,
			ConnectionID: connectionID, State: state, FailureCode: observation.FailureCode,
			UpdatedAt: application.config.Now().UTC(),
		}); err != nil {
			return contracts.Connector{}, err
		}
	}
	return connector, nil
}

func (application *service) runtimeConnectorAndRelease(
	ctx context.Context,
	connectorKey string,
) (contracts.Connector, contracts.Release, error) {
	connector, err := application.config.Repository.Connector(ctx, strings.TrimSpace(connectorKey))
	if err != nil {
		return contracts.Connector{}, contracts.Release{}, err
	}
	connector, err = validateRuntimeReconcileConnector(connector)
	if err != nil {
		return contracts.Connector{}, contracts.Release{}, err
	}
	release, err := application.installedReleaseEvidence(ctx, connector)
	if err != nil {
		return contracts.Connector{}, contracts.Release{}, err
	}
	connector.Release = release
	return connector, release, nil
}

func (application *service) runtimeConnectorAndReleaseForDigest(
	ctx context.Context,
	connectorKey, releaseDigest string,
	validateRelease bool,
) (contracts.Connector, contracts.Release, error) {
	connector, err := application.config.Repository.Connector(ctx, strings.TrimSpace(connectorKey))
	if err != nil {
		return contracts.Connector{}, contracts.Release{}, err
	}
	releaseDigest = strings.TrimSpace(releaseDigest)
	current := (connector.Installation.State == contracts.InstallationStateInstalled ||
		connector.Installation.State == contracts.InstallationStateUninstalling) &&
		connector.Installation.InstalledReleaseDigest == releaseDigest
	candidate := (connector.Installation.State == contracts.InstallationStateInstalling ||
		connector.Installation.State == contracts.InstallationStateUpdating) &&
		connector.Installation.CandidateReleaseDigest == releaseDigest
	if !current && !candidate {
		return contracts.Connector{}, contracts.Release{}, contracts.NewDomainError(
			contracts.ErrorCodeRevisionConflict, "runtime target is not the current or candidate release", true, nil,
		)
	}
	release, err := application.config.Repository.InstalledRelease(ctx, connector.Key, releaseDigest)
	if errors.Is(err, contracts.ErrNotFound) && connector.Release.ReleaseDigest == releaseDigest {
		release, err = connector.Release, nil
	}
	if err != nil {
		return contracts.Connector{}, contracts.Release{}, err
	}
	if validateRelease {
		if err := contracts.ValidateRuntimeReleaseShape(release); err != nil {
			return contracts.Connector{}, contracts.Release{}, err
		}
	}
	connector.Release = release
	return connector, release, nil
}

func (application *service) saveRuntimeDesired(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey, releaseDigest string,
	binding contracts.RuntimeBinding,
	forceNewGeneration bool,
) (contracts.RuntimeConvergence, error) {
	var saved contracts.RuntimeConvergence
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		connector, err := tx.Connector(connectorKey)
		if err != nil {
			return err
		}
		if connector.Installation.State != contracts.InstallationStateInstalled ||
			connector.Installation.InstalledReleaseDigest != releaseDigest {
			return contracts.NewDomainError(contracts.ErrorCodeRevisionConflict, "installed connector changed while planning runtime", true, nil)
		}
		convergence, changed, err := upsertRuntimeDesired(
			tx, scope, connectorKey, releaseDigest, binding, nextGeneration(connector.Revision), forceNewGeneration,
			application.config.Now().UTC(),
		)
		if err != nil {
			return err
		}
		saved = convergence
		if !changed {
			return nil
		}
		revision := tx.AdvanceRevision()
		for revision <= connector.Revision {
			revision = tx.AdvanceRevision()
		}
		connector.Revision = revision
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		return tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: connectorKey, Revision: revision})
	})
	return saved, err
}

func upsertRuntimeDesired(
	tx Transaction,
	scope contracts.OperationScope,
	connectorKey, releaseDigest string,
	binding contracts.RuntimeBinding,
	minimumGeneration uint64,
	forceNewGeneration bool,
	now time.Time,
) (contracts.RuntimeConvergence, bool, error) {
	scope.AccountID = strings.TrimSpace(scope.AccountID)
	connectorKey = strings.TrimSpace(connectorKey)
	releaseDigest = strings.TrimSpace(releaseDigest)
	binding.ConnectionID = strings.TrimSpace(binding.ConnectionID)
	if connectorKey == "" || releaseDigest == "" || binding.ConnectionID == "" {
		return contracts.RuntimeConvergence{}, false, invalidOperationReceipt("runtime desired identity is incomplete")
	}
	convergence, err := tx.RuntimeConvergence(scope, connectorKey)
	if err != nil && !errors.Is(err, contracts.ErrNotFound) {
		return contracts.RuntimeConvergence{}, false, err
	}
	if err == nil && !forceNewGeneration && runtimeDesiredMatchesBinding(convergence.Desired, releaseDigest, binding) {
		return convergence, false, nil
	}
	generation := maxGeneration(minimumGeneration)
	if err == nil {
		if convergence.Desired.Generation == math.MaxUint64 {
			return contracts.RuntimeConvergence{}, false, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "runtime desired generation is exhausted", false, nil)
		}
		generation = convergence.Desired.Generation + 1
		if generation < minimumGeneration {
			generation = minimumGeneration
		}
		convergence.LeaseToken++
	}
	convergence.Desired = contracts.RuntimeDesired{
		Scope: scope, ConnectorKey: connectorKey, Generation: generation, Enabled: binding.Enabled,
		ConnectionID: binding.ConnectionID, ReleaseDigest: releaseDigest, AuthorizationState: binding.AuthorizationState,
		UpdatedAt: now,
	}
	convergence.Attempt = 0
	convergence.NextAttemptAt = now
	convergence.LeaseOwner = ""
	convergence.LeaseExpiresAt = nil
	convergence.LastErrorCode = ""
	convergence.LastError = ""
	convergence.UpdatedAt = now
	if err := tx.SaveRuntimeConvergence(convergence); err != nil {
		return contracts.RuntimeConvergence{}, false, err
	}
	return convergence, true, nil
}

func runtimeDesiredMatchesBinding(desired contracts.RuntimeDesired, releaseDigest string, binding contracts.RuntimeBinding) bool {
	return desired.ReleaseDigest == strings.TrimSpace(releaseDigest) && desired.Enabled == binding.Enabled &&
		desired.ConnectionID == strings.TrimSpace(binding.ConnectionID) && desired.AuthorizationState == binding.AuthorizationState
}

func runtimeBindingMatchesDesired(binding contracts.RuntimeBinding, desired contracts.RuntimeDesired) bool {
	return runtimeDesiredMatchesBinding(desired, desired.ReleaseDigest, binding)
}
