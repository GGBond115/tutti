package application

import (
	"context"
	"errors"
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"strings"
	"time"
)

// ReconcileRuntime reapplies device-installed truth for the mutation's
// explicit account scope. It does not mutate installation state.
func (application *service) ReconcileRuntime(ctx context.Context, mutation contracts.ConnectorMutation) (contracts.MutationResult, error) {
	return application.acceptConnectorOperation(ctx, mutation, contracts.OperationKindReconcileRuntime, func(connector contracts.Connector) (contracts.Connector, error) {
		return validateRuntimeReconcileConnector(connector)
	})
}

// EnsureRuntimeReconcile is the level-triggered, host-internal runtime repair
// command. Unlike ReconcileRuntime, it does not accept an externally observed
// market revision: it atomically creates a reconcile from current durable
// state, or joins the active reconcile for the same Connector and account.
func (application *service) EnsureRuntimeReconcile(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) (contracts.EnsureRuntimeReconcileResult, error) {
	connectorKey = strings.TrimSpace(connectorKey)
	scope.AccountID = strings.TrimSpace(scope.AccountID)
	if connectorKey == "" {
		return contracts.EnsureRuntimeReconcileResult{}, invalidRequest("connectorKey is required")
	}
	var result contracts.MutationResult
	created := false
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		active, err := tx.ActiveOperation(connectorKey)
		if err != nil {
			return err
		}
		if active != nil {
			if active.Kind != contracts.OperationKindReconcileRuntime || active.Scope != scope {
				return contracts.NewDomainError(
					contracts.ErrorCodeOperationInProgress,
					fmt.Sprintf("operation %s is already in progress", active.OperationID),
					true,
					nil,
				)
			}
			connector, err := tx.Connector(connectorKey)
			if err != nil {
				return err
			}
			result = contracts.MutationResult{Connector: &connector, Operation: *active, Revision: tx.Revision()}
			return nil
		}
		connector, err := tx.Connector(connectorKey)
		if err != nil {
			return err
		}
		connector, err = validateRuntimeReconcileConnector(connector)
		if err != nil {
			return err
		}
		now := application.config.Now().UTC()
		operationID, err := application.config.NewID()
		if err != nil {
			return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector operation id could not be generated", true, err)
		}
		revision := tx.AdvanceRevision()
		connector.Revision = revision
		operation := contracts.Operation{
			OperationID:     operationID,
			ClientRequestID: "ensure-runtime-reconcile/" + operationID,
			ConnectorKey:    connectorKey,
			Kind:            contracts.OperationKindReconcileRuntime,
			Scope:           scope,
			State:           contracts.OperationStateAccepted,
			Stage:           contracts.OperationStageAccepted,
			Target:          operationTarget(contracts.OperationKindReconcileRuntime, connector),
			HostGeneration:  contracts.HostGeneration{BootEpoch: application.config.BootEpoch, Generation: revision},
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connectorKey,
			OperationID:  operationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = contracts.MutationResult{Connector: &connector, Operation: operation, Revision: revision}
		created = true
		return nil
	})
	if err != nil {
		return contracts.EnsureRuntimeReconcileResult{}, err
	}
	if created || result.Operation.State == contracts.OperationStateAccepted {
		if err := application.config.Scheduler.Schedule(ctx, result.Operation.OperationID); err != nil {
			return contracts.EnsureRuntimeReconcileResult{}, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector operation could not be scheduled", true, err)
		}
	}
	return contracts.EnsureRuntimeReconcileResult{MutationResult: result, Created: created}, nil
}

func validateRuntimeReconcileConnector(connector contracts.Connector) (contracts.Connector, error) {
	if connector.Installation.State != contracts.InstallationStateInstalled ||
		strings.TrimSpace(connector.Installation.InstalledReleaseDigest) == "" {
		return contracts.Connector{}, invalidTransition("runtime", string(connector.Installation.State), string(contracts.InstallationStateInstalled))
	}
	return connector, nil
}

func (application *service) GetAuthorizationProjection(
	ctx context.Context,
	accountID, connectorKey string,
) (contracts.AuthorizationProjection, error) {
	if application.config.AuthorizationProjections == nil {
		return contracts.AuthorizationProjection{}, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "account authorization projections are not registered", false, nil)
	}
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(connectorKey) == "" {
		return contracts.AuthorizationProjection{}, invalidRequest("accountId and connectorKey are required")
	}
	return application.config.AuthorizationProjections.AuthorizationProjection(ctx, accountID, connectorKey)
}

// ObserveAuthorization persists server-observed account authorization and
// schedules a durable runtime reconcile for an installed connector.
func (application *service) ObserveAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	projection contracts.AuthorizationProjection,
) (contracts.MutationResult, error) {
	if err := application.saveAuthorizationProjection(ctx, mutation, projection); err != nil {
		return contracts.MutationResult{}, err
	}
	return application.ReconcileRuntime(ctx, mutation)
}

// ProjectAuthorization persists server-observed account authorization without
// scheduling runtime work. Daemon hosts pair it with EnsureRuntimeReconcile
// while holding their account lifecycle fence.
func (application *service) ProjectAuthorization(
	ctx context.Context,
	scope contracts.OperationScope,
	projection contracts.AuthorizationProjection,
) error {
	return application.saveAuthorizationProjection(ctx, contracts.ConnectorMutation{
		ConnectorKey: projection.ConnectorKey,
		AccountID:    scope.AccountID,
	}, projection)
}

func (application *service) saveAuthorizationProjection(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	projection contracts.AuthorizationProjection,
) error {
	if application.config.AuthorizationProjections == nil {
		return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "account authorization projections are not registered", false, nil)
	}
	projection.AccountID = strings.TrimSpace(projection.AccountID)
	projection.ConnectorKey = strings.TrimSpace(projection.ConnectorKey)
	projection.ConnectionID = strings.TrimSpace(projection.ConnectionID)
	if projection.AccountID == "" || projection.ConnectorKey == "" ||
		projection.AccountID != strings.TrimSpace(mutation.AccountID) ||
		projection.ConnectorKey != strings.TrimSpace(mutation.ConnectorKey) {
		return invalidRequest("authorization projection does not match the mutation scope")
	}
	if projection.ConnectionID != "" && !runtimeConnectionIDPattern.MatchString(projection.ConnectionID) {
		return invalidRequest("authorization projection connectionId is invalid")
	}
	switch projection.State {
	case contracts.AuthorizationStateNotRequired, contracts.AuthorizationStateDisconnected, contracts.AuthorizationStatePending,
		contracts.AuthorizationStateConnected, contracts.AuthorizationStateExpired, contracts.AuthorizationStateFailed:
	default:
		return invalidRequest("authorization projection state is invalid")
	}
	if projection.UpdatedAt.IsZero() {
		projection.UpdatedAt = application.config.Now().UTC()
	}
	return application.config.AuthorizationProjections.SaveAuthorizationProjection(ctx, projection)
}

func (application *service) projectAuthorizationAndScheduleRuntime(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey, connectionID string,
	state contracts.AuthorizationState,
	failureCode string,
) error {
	_, err := application.projectAuthorization(ctx, scope, connectorKey, connectionID, state, failureCode)
	if err != nil || application.config.AuthorizationProjections == nil || strings.TrimSpace(scope.AccountID) == "" {
		return err
	}
	if state == contracts.AuthorizationStatePending {
		return nil
	}
	return application.ReconcileRuntimeDesired(ctx, scope, connectorKey)
}

// projectAuthorization persists authorization truth without creating runtime
// work. Recovery callers use it before the daemon creates and awaits exactly
// one reconcile under the account lifecycle fence.
func (application *service) projectAuthorization(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey, connectionID string,
	state contracts.AuthorizationState,
	failureCode string,
) (bool, error) {
	if application.config.AuthorizationProjections == nil || strings.TrimSpace(scope.AccountID) == "" {
		return false, nil
	}
	connectionID = strings.TrimSpace(connectionID)
	if state == contracts.AuthorizationStateConnected && connectionID == "" {
		return false, invalidOperationReceipt("connected authorization did not provide a connection id")
	}
	connector, err := application.config.Repository.Connector(ctx, strings.TrimSpace(connectorKey))
	if err != nil {
		return false, err
	}
	release, err := application.installedReleaseEvidence(ctx, connector)
	if err != nil {
		return false, err
	}
	remote := release.Manifest.Implementation.RemoteStreamableHTTP != nil
	if remote {
		snapshotStore, ok := application.config.AuthorizationProjections.(AuthorizationSnapshotStore)
		if !ok || application.config.AuthorizationSnapshots == nil {
			return false, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "remote connector authorization snapshot is unavailable", true, nil)
		}
		authoritative, snapshotErr := application.config.AuthorizationSnapshots.AuthorizationSnapshot(ctx, scope.AccountID)
		if snapshotErr != nil {
			return false, fmt.Errorf("refresh remote connector authorization snapshot: %w", snapshotErr)
		}
		if _, applyErr := snapshotStore.ApplyAuthorizationSnapshot(ctx, scope.AccountID, authoritative); applyErr != nil {
			return false, fmt.Errorf("apply remote connector authorization snapshot: %w", applyErr)
		}
		if application.config.AuthorizationReadiness != nil {
			application.config.AuthorizationReadiness.SetReady(scope.AccountID, true)
		}
		return true, nil
	}
	mutation := contracts.ConnectorMutation{
		ConnectorKey: strings.TrimSpace(connectorKey),
		AccountID:    strings.TrimSpace(scope.AccountID),
	}
	projection := contracts.AuthorizationProjection{
		AccountID: strings.TrimSpace(scope.AccountID), ConnectorKey: strings.TrimSpace(connectorKey),
		ConnectionID: connectionID, State: state, FailureCode: strings.TrimSpace(failureCode), UpdatedAt: application.config.Now().UTC(),
	}
	return false, application.saveAuthorizationProjection(ctx, mutation, projection)
}

func (application *service) ReconcileInstalledRuntimes(ctx context.Context) error {
	return application.ReconcileInstalledRuntimesForScope(ctx, contracts.OperationScope{})
}

// ReconcileInstalledRuntimesForScope rebuilds runtime intent for an explicit
// account authority after daemon or guest restart.
func (application *service) ReconcileInstalledRuntimesForScope(ctx context.Context, scope contracts.OperationScope) error {
	return application.reconcileInstalledRuntimesForScope(ctx, scope, false)
}

// ReconcileRemoteAuthorizedRuntimesForScope is the level-triggered repair
// path for account snapshot convergence. It deliberately excludes local and
// authorization-free runtimes so the five-minute calibration cannot restart
// unrelated CLI or stdio processes.
func (application *service) ReconcileRemoteAuthorizedRuntimesForScope(ctx context.Context, scope contracts.OperationScope) error {
	return application.reconcileInstalledRuntimesForScope(ctx, scope, true)
}

func (application *service) InstalledRemoteAuthorizedConnectorKeys(ctx context.Context) ([]string, error) {
	snapshot, err := application.config.Repository.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0)
	for _, connector := range snapshot.Connectors {
		if connector.Installation.State != contracts.InstallationStateInstalled {
			continue
		}
		release, err := application.installedReleaseEvidence(ctx, connector)
		if err != nil {
			// Keep a currently-known remote connector eligible so a broken local
			// release evidence record cannot hide a fail-closed authorization
			// reconcile. Unrelated local connectors are skipped independently.
			release = connector.Release
		}
		if release.Manifest.Implementation.RemoteStreamableHTTP != nil && release.Manifest.AuthorizationKind != "none" {
			keys = append(keys, connector.Key)
		}
	}
	return keys, nil
}

// RuntimeReconcileFailures contains Connector-local recovery failures. The
// caller may safely publish routes committed by other Connectors while retrying
// these keys independently. Snapshot and generation-commit failures are not
// included because they require the global runtime fence to remain closed.
type RuntimeReconcileFailures struct {
	failures []error
	keys     []string
}

func (failures *RuntimeReconcileFailures) Error() string {
	return errors.Join(failures.failures...).Error()
}

func (failures *RuntimeReconcileFailures) Unwrap() []error {
	return append([]error(nil), failures.failures...)
}

func (failures *RuntimeReconcileFailures) ConnectorKeys() []string {
	return append([]string(nil), failures.keys...)
}

func (failures *RuntimeReconcileFailures) add(connectorKey, stage string, err error) {
	failures.keys = append(failures.keys, connectorKey)
	failures.failures = append(failures.failures, fmt.Errorf("%s: %s: %w", connectorKey, stage, err))
}

func (failures *RuntimeReconcileFailures) errOrNil() error {
	if len(failures.failures) == 0 {
		return nil
	}
	return failures
}

func (application *service) reconcileInstalledRuntimesForScope(ctx context.Context, scope contracts.OperationScope, remoteAuthorizedOnly bool) error {
	if application == nil {
		return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector application is unavailable", false, nil)
	}
	snapshot, err := application.config.Repository.Snapshot(ctx)
	if err != nil {
		return err
	}
	reconcileFailures := &RuntimeReconcileFailures{}
	for _, connector := range snapshot.Connectors {
		if connector.Installation.State != contracts.InstallationStateInstalled {
			continue
		}
		installedRelease, evidenceErr := application.installedReleaseEvidence(ctx, connector)
		if evidenceErr != nil {
			reconcileFailures.add(connector.Key, "load installed release", evidenceErr)
			continue
		}
		if remoteAuthorizedOnly && (installedRelease.Manifest.Implementation.RemoteStreamableHTTP == nil ||
			installedRelease.Manifest.AuthorizationKind == "none") {
			continue
		}
		if validationErr := contracts.ValidateRuntimeReleaseShape(installedRelease); validationErr != nil {
			reconcileFailures.add(connector.Key, "validate installed release", validationErr)
			continue
		}
		if err := application.reconcileRuntimeDesiredAfterFence(ctx, scope, connector.Key); err != nil {
			reconcileFailures.add(connector.Key, "reconcile runtime", err)
		}
	}
	return reconcileFailures.errOrNil()
}

func (application *service) FenceInstalledRuntimes(ctx context.Context) error {
	return application.FenceInstalledRuntimesForScope(ctx, contracts.OperationScope{})
}

// FenceInstalledRuntimesForScope removes runtime projections without deleting
// device installation facts.
func (application *service) FenceInstalledRuntimesForScope(ctx context.Context, scope contracts.OperationScope) error {
	if application == nil {
		return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector application is unavailable", false, nil)
	}
	snapshot, err := application.config.Repository.Snapshot(ctx)
	if err != nil {
		return err
	}
	var fenceErrors []error
	for _, connector := range snapshot.Connectors {
		if connector.Installation.State != contracts.InstallationStateInstalled {
			continue
		}
		installedRelease, evidenceErr := application.installedReleaseEvidence(ctx, connector)
		if evidenceErr != nil {
			// Legacy stores may have lost the previous release metadata when a
			// prepared update failed. The implementation host can still fence every
			// route for the connector by identity, without trusting that metadata.
			// This keeps the connector fail-closed without holding the global
			// publication gate closed and blocking a repair install.
			fallbackErr := application.config.ImplementationCommands.DeactivateRuntime(ctx, contracts.RuntimeDeactivationRequest{
				Scope: scope, ConnectionID: defaultConnectorConnectionID, ConnectorKey: connector.Key,
				ReleaseDigest: connector.Installation.InstalledReleaseDigest, AllConnections: true,
				Generation: contracts.HostGeneration{BootEpoch: application.config.BootEpoch, Generation: maxGeneration(connector.Revision)},
				Deadline:   application.config.Now().UTC().Add(5 * time.Second),
			})
			if fallbackErr != nil {
				fenceErrors = append(fenceErrors, errors.Join(evidenceErr, fallbackErr))
			}
			continue
		}
		connector.Release = installedRelease
		operation := contracts.Operation{OperationID: "fence/" + application.config.BootEpoch + "/" + connector.Key,
			ConnectorKey: connector.Key, Scope: scope}
		binding, bindingErr := application.resolveRuntimeBinding(ctx, operation, connector, installedRelease, contracts.RuntimeBindingPurposeDeactivate)
		if bindingErr != nil {
			fenceErrors = append(fenceErrors, bindingErr)
			continue
		}
		clear(binding.CredentialBrokerGrant)
		fenceErrors = append(fenceErrors, application.config.ImplementationCommands.DeactivateRuntime(ctx, contracts.RuntimeDeactivationRequest{
			Scope: scope, ConnectionID: binding.ConnectionID, ConnectorKey: connector.Key,
			ReleaseDigest: connector.Installation.InstalledReleaseDigest,
			Generation:    contracts.HostGeneration{BootEpoch: application.config.BootEpoch, Generation: maxGeneration(connector.Revision)},
			Deadline:      application.config.Now().UTC().Add(5 * time.Second),
		}))
	}
	return errors.Join(fenceErrors...)
}

func maxGeneration(generation uint64) uint64 {
	if generation == 0 {
		return 1
	}
	return generation
}

func nextGeneration(generation uint64) uint64 {
	return maxGeneration(generation) + 1
}
