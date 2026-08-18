package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (application *service) ExecuteOperation(ctx context.Context, operationID string) error {
	execution, owner := application.beginOperationExecution(operationID)
	if !owner {
		select {
		case <-execution.done:
			return execution.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var executeErr error
	defer func() {
		application.finishOperationExecution(operationID, execution, executeErr)
	}()
	executeErr = application.executeOperation(ctx, operationID)
	return executeErr
}

func (application *service) beginOperationExecution(operationID string) (*operationExecution, bool) {
	application.executionMu.Lock()
	defer application.executionMu.Unlock()
	if execution, ok := application.inFlight[operationID]; ok {
		return execution, false
	}
	execution := &operationExecution{done: make(chan struct{})}
	application.inFlight[operationID] = execution
	return execution, true
}

func (application *service) finishOperationExecution(
	operationID string,
	execution *operationExecution,
	err error,
) {
	application.executionMu.Lock()
	defer application.executionMu.Unlock()
	execution.err = err
	delete(application.inFlight, operationID)
	close(execution.done)
}

func (application *service) executeOperation(ctx context.Context, operationID string) error {
	now := application.config.Now().UTC()
	operation, claimed, err := application.config.Repository.ClaimOperation(
		ctx,
		operationID,
		application.config.WorkerID,
		now,
		now.Add(application.config.LeaseDuration),
	)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	executionContext, cancelExecution := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go application.renewOperationLease(executionContext, cancelExecution, operation, heartbeatDone)
	defer func() {
		cancelExecution()
		<-heartbeatDone
		_ = application.config.Repository.ReleaseOperationLease(
			context.WithoutCancel(ctx),
			operationID,
			application.config.WorkerID,
			operation.LeaseToken,
		)
	}()
	if operation.State == contracts.OperationStateCompleted || operation.State == contracts.OperationStateFailed {
		return nil
	}
	operation, err = application.markOperationRunning(executionContext, operation.OperationID)
	if err != nil {
		return err
	}

	var executeErr error
	switch operation.Kind {
	case contracts.OperationKindRefreshCatalog:
		executeErr = application.executeRefresh(executionContext, operation)
	case contracts.OperationKindInstall:
		executeErr = application.executeInstall(executionContext, operation)
	case contracts.OperationKindUninstall:
		executeErr = application.executeUninstall(executionContext, operation)
	case contracts.OperationKindReconcileRuntime:
		executeErr = application.executeRuntimeReconcile(executionContext, operation)
	case contracts.OperationKindDisconnectAuthorization:
		executeErr = application.executeDisconnectAuthorization(executionContext, operation)
	case contracts.OperationKindStartAuthorization:
		_, executeErr = application.beginAuthorizationSession(executionContext, operation, nil, "")
	default:
		executeErr = invalidRequest(fmt.Sprintf("operation kind %q is not executable", operation.Kind))
	}
	if executeErr != nil {
		if operation.Kind != contracts.OperationKindRefreshCatalog && operation.Kind != contracts.OperationKindStartAuthorization &&
			isRetryableError(executeErr) {
			return executeErr
		}
		code := contracts.ErrorCodeInstallFailed
		if operation.Kind == contracts.OperationKindRefreshCatalog {
			code = errorCodeOr(executeErr, contracts.ErrorCodeUpstreamUnavailable)
		}
		if operation.Kind == contracts.OperationKindStartAuthorization ||
			operation.Kind == contracts.OperationKindDisconnectAuthorization {
			code = contracts.ErrorCodeAuthorizationFailed
		}
		terminalContext, cancelTerminal := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		terminalErr := application.failOperation(terminalContext, operation.OperationID, code)
		cancelTerminal()
		if terminalErr != nil {
			return errors.Join(executeErr, fmt.Errorf("record connector operation failure: %w", terminalErr))
		}
		return executeErr
	}
	return nil
}

func (application *service) renewOperationLease(ctx context.Context, cancel context.CancelFunc, operation contracts.Operation, done chan<- error) {
	interval := application.config.LeaseDuration / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			now := application.config.Now().UTC()
			renewContext, renewCancel := context.WithTimeout(context.WithoutCancel(ctx), interval)
			err := application.config.Repository.RenewOperationLease(renewContext, operation.OperationID,
				application.config.WorkerID, operation.LeaseToken, now, now.Add(application.config.LeaseDuration))
			renewCancel()
			if err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

func (application *service) Recover(ctx context.Context) error {
	operations, err := application.config.Repository.RecoverableOperations(ctx)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operationTouchesImplementationHost(operation.Kind) && operation.HostGeneration.BootEpoch != application.config.BootEpoch {
			operation, err = application.adoptRuntimeOperation(ctx, operation.OperationID)
			if err != nil {
				return err
			}
		}
		if operation.LeaseExpiresAt != nil && operation.LeaseExpiresAt.After(application.config.Now().UTC()) &&
			operation.LeaseOwner != "" && operation.LeaseOwner != application.config.WorkerID {
			delay := operation.LeaseExpiresAt.Sub(application.config.Now().UTC())
			operationID := operation.OperationID
			go func() {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					_ = application.config.Scheduler.Schedule(ctx, operationID)
				}
			}()
			continue
		}
		if err := application.config.Scheduler.Schedule(ctx, operation.OperationID); err != nil {
			return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector operation recovery could not be scheduled", true, err)
		}
	}
	return nil
}

func operationTouchesImplementationHost(kind contracts.OperationKind) bool {
	switch kind {
	case contracts.OperationKindInstall, contracts.OperationKindUninstall, contracts.OperationKindReconcileRuntime:
		return true
	default:
		return false
	}
}

func (application *service) adoptRuntimeOperation(ctx context.Context, operationID string) (contracts.Operation, error) {
	var adopted contracts.Operation
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		operation, err := tx.Operation(operationID)
		if err != nil {
			return err
		}
		if operation.State == contracts.OperationStateCompleted || operation.State == contracts.OperationStateFailed {
			adopted = operation
			return nil
		}
		revision := tx.AdvanceRevision()
		operation.HostGeneration = contracts.HostGeneration{BootEpoch: application.config.BootEpoch, Generation: revision}
		operation.UpdatedAt = application.config.Now().UTC()
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{ConnectorKey: operation.ConnectorKey, OperationID: operation.OperationID, Revision: revision}); err != nil {
			return err
		}
		adopted = operation
		return nil
	})
	return adopted, err
}

func (application *service) acceptConnectorOperation(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	kind contracts.OperationKind,
	transition func(contracts.Connector) (contracts.Connector, error),
) (contracts.MutationResult, error) {
	if err := validateConnectorMutation(mutation); err != nil {
		return contracts.MutationResult{}, err
	}
	var result contracts.MutationResult
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		existing, err := tx.OperationByClientRequestID(mutation.AccountID, mutation.ClientRequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := verifyIdempotentOperation(*existing, kind, mutation.ConnectorKey, mutation.AccountID); err != nil {
				return err
			}
			connector, err := tx.Connector(mutation.ConnectorKey)
			if err != nil {
				return err
			}
			result = contracts.MutationResult{Connector: &connector, Operation: *existing, Revision: tx.Revision()}
			return nil
		}
		if kind == contracts.OperationKindInstall {
			freshness, err := tx.CatalogFreshness()
			if err != nil {
				return err
			}
			admissible := freshness.State == contracts.CatalogFreshnessFresh ||
				(freshness.State == contracts.CatalogFreshnessRefreshing && freshness.SnapshotID != "" && freshness.StaleSince == nil)
			if freshness.SnapshotID == "" || !admissible {
				return contracts.NewDomainError(contracts.ErrorCodeUpstreamUnavailable,
					"connector install requires a fresh catalog snapshot", true, nil)
			}
		}
		connector, err := tx.Connector(mutation.ConnectorKey)
		if err != nil {
			return err
		}
		if mutation.ExpectedConnectorRevision != nil {
			if connector.Revision != *mutation.ExpectedConnectorRevision {
				return contracts.NewDomainError(
					contracts.ErrorCodeRevisionConflict,
					fmt.Sprintf("expected connector revision %d but current connector revision is %d", *mutation.ExpectedConnectorRevision, connector.Revision),
					true,
					nil,
				)
			}
		} else if err := verifyRevision(tx, mutation.ExpectedRevision); err != nil {
			return err
		}
		if err := rejectActiveOperation(tx, mutation.ConnectorKey); err != nil {
			return err
		}
		connector, err = transition(connector)
		if err != nil {
			return err
		}
		now := application.config.Now().UTC()
		revision := tx.AdvanceRevision()
		operationID, err := application.config.NewID()
		if err != nil {
			return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector operation id could not be generated", true, err)
		}
		connector.Revision = revision
		operation := contracts.Operation{
			OperationID:     operationID,
			ClientRequestID: mutation.ClientRequestID,
			OwnerAccountID:  strings.TrimSpace(mutation.AccountID),
			Visibility:      contracts.OperationVisibilityAccount,
			ConnectorKey:    mutation.ConnectorKey,
			Kind:            kind,
			Scope:           contracts.OperationScope{AccountID: strings.TrimSpace(mutation.AccountID)},
			State:           contracts.OperationStateAccepted,
			Stage:           contracts.OperationStageAccepted,
			Target:          operationTarget(kind, connector),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if kind == contracts.OperationKindInstall || kind == contracts.OperationKindUninstall || kind == contracts.OperationKindReconcileRuntime {
			operation.HostGeneration = contracts.HostGeneration{BootEpoch: application.config.BootEpoch, Generation: revision}
		}
		if err := tx.SaveConnector(connector); err != nil {
			return err
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connector.Key,
			OperationID:  operation.OperationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = contracts.MutationResult{Connector: &connector, Operation: operation, Revision: revision}
		return nil
	})
	if err != nil {
		return contracts.MutationResult{}, err
	}
	if kind != contracts.OperationKindStartAuthorization &&
		(result.Operation.State == contracts.OperationStateAccepted || result.Operation.State == contracts.OperationStateRunning) {
		if err := application.config.Scheduler.Schedule(ctx, result.Operation.OperationID); err != nil {
			return contracts.MutationResult{}, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector operation could not be scheduled", true, err)
		}
	}
	return result, nil
}

// isIdempotentConnectorOperation distinguishes a continuation of an existing
// command from a new state transition. Authorization providers may expose a
// multi-step flow through repeated BeginAuthorization calls with one stable
// clientRequestId, so account projection guards must not reject that replay as
// a new pending-to-pending transition. acceptConnectorOperation repeats this
// verification inside its mutation transaction before returning the operation.
func (application *service) isIdempotentConnectorOperation(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	kind contracts.OperationKind,
) (bool, error) {
	var replay bool
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		existing, err := tx.OperationByClientRequestID(mutation.AccountID, mutation.ClientRequestID)
		if err != nil {
			return err
		}
		if existing == nil {
			return nil
		}
		if err := verifyIdempotentOperation(*existing, kind, mutation.ConnectorKey, mutation.AccountID); err != nil {
			return err
		}
		replay = true
		return nil
	})
	return replay, err
}

func (application *service) acceptOperation(
	ctx context.Context,
	mutation contracts.Mutation,
	kind contracts.OperationKind,
	connectorKey string,
) (contracts.MutationResult, error) {
	if err := validateMutation(mutation); err != nil {
		return contracts.MutationResult{}, err
	}
	var result contracts.MutationResult
	err := application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		ownerAccountID := strings.TrimSpace(mutation.Scope.AccountID)
		existing, err := tx.OperationByClientRequestID(ownerAccountID, mutation.ClientRequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			if err := verifyIdempotentOperation(*existing, kind, connectorKey, ownerAccountID); err != nil {
				return err
			}
			result = contracts.MutationResult{Operation: *existing, Revision: tx.Revision()}
			return nil
		}
		if err := verifyRevision(tx, mutation.ExpectedRevision); err != nil {
			return err
		}
		if err := rejectActiveOperation(tx, connectorKey); err != nil {
			return err
		}
		now := application.config.Now().UTC()
		revision := tx.AdvanceRevision()
		operationID, err := application.config.NewID()
		if err != nil {
			return contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector operation id could not be generated", true, err)
		}
		operation := contracts.Operation{
			OperationID:     operationID,
			ClientRequestID: mutation.ClientRequestID,
			OwnerAccountID:  ownerAccountID,
			Visibility:      contracts.OperationVisibilityAccount,
			ConnectorKey:    connectorKey,
			Kind:            kind,
			Scope:           contracts.OperationScope{AccountID: ownerAccountID},
			State:           contracts.OperationStateAccepted,
			Stage:           contracts.OperationStageAccepted,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := tx.SaveOperation(operation); err != nil {
			return err
		}
		if err := tx.EnqueueConnectorMarketChanged(contracts.ChangedEvent{
			ConnectorKey: connectorKey,
			OperationID:  operation.OperationID,
			Revision:     revision,
		}); err != nil {
			return err
		}
		result = contracts.MutationResult{Operation: operation, Revision: revision}
		return nil
	})
	if err != nil {
		return contracts.MutationResult{}, err
	}
	if result.Operation.State == contracts.OperationStateAccepted || result.Operation.State == contracts.OperationStateRunning {
		if err := application.config.Scheduler.Schedule(ctx, result.Operation.OperationID); err != nil {
			return contracts.MutationResult{}, contracts.NewDomainError(contracts.ErrorCodeUnavailable, "connector operation could not be scheduled", true, err)
		}
	}
	return result, nil
}

func validateMutation(mutation contracts.Mutation) error {
	if strings.TrimSpace(mutation.ClientRequestID) == "" {
		return invalidRequest("clientRequestId is required")
	}
	return nil
}

func validateConnectorMutation(mutation contracts.ConnectorMutation) error {
	if err := validateMutation(mutation.Mutation); err != nil {
		return err
	}
	if strings.TrimSpace(mutation.ConnectorKey) == "" {
		return invalidRequest("connectorKey is required")
	}
	return nil
}

func validateAuthorizationMutation(mutation contracts.ConnectorMutation) error {
	if err := validateConnectorMutation(mutation); err != nil {
		return err
	}
	if mutation.ReplacementPolicy != "" &&
		mutation.ReplacementPolicy != contracts.AuthorizationReplacementPolicyReplaceActive {
		return invalidRequest("authorization replacementPolicy is invalid")
	}
	return nil
}

func (application *service) verifyConnectorMutationRevision(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
) error {
	return application.config.Repository.Transaction(ctx, func(tx Transaction) error {
		connector, err := tx.Connector(mutation.ConnectorKey)
		if err != nil {
			return err
		}
		if mutation.ExpectedConnectorRevision != nil {
			if connector.Revision != *mutation.ExpectedConnectorRevision {
				return contracts.NewDomainError(
					contracts.ErrorCodeRevisionConflict,
					fmt.Sprintf("expected connector revision %d but current connector revision is %d", *mutation.ExpectedConnectorRevision, connector.Revision),
					true,
					nil,
				)
			}
			return nil
		}
		return verifyRevision(tx, mutation.ExpectedRevision)
	})
}

func verifyRevision(tx Transaction, expected uint64) error {
	if tx.Revision() == expected {
		return nil
	}
	return contracts.NewDomainError(
		contracts.ErrorCodeRevisionConflict,
		fmt.Sprintf("expected revision %d but current revision is %d", expected, tx.Revision()),
		true,
		nil,
	)
}

func verifyIdempotentOperation(operation contracts.Operation, kind contracts.OperationKind, connectorKey, accountID string) error {
	if operation.Kind != kind || operation.ConnectorKey != connectorKey ||
		operation.Scope.AccountID != strings.TrimSpace(accountID) {
		return invalidRequest("clientRequestId was already used for a different connector-market command")
	}
	return nil
}

func rejectActiveOperation(tx Transaction, connectorKey string) error {
	active, err := tx.ActiveOperation(connectorKey)
	if err != nil {
		return err
	}
	if active == nil {
		return nil
	}
	return contracts.NewDomainError(
		contracts.ErrorCodeOperationInProgress,
		fmt.Sprintf("operation %s is already in progress", active.OperationID),
		true,
		nil,
	)
}

func invalidRequest(message string) error {
	return contracts.NewDomainError(contracts.ErrorCodeInvalidRequest, message, false, nil)
}

func invalidTransition(kind, from, to string) error {
	return contracts.NewDomainError(
		contracts.ErrorCodeOperationInProgress,
		fmt.Sprintf("%s cannot transition from %s to %s", kind, from, to),
		true,
		nil,
	)
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func operationTarget(kind contracts.OperationKind, connector contracts.Connector) *contracts.OperationTarget {
	if kind == contracts.OperationKindInstall || kind == contracts.OperationKindStartAuthorization ||
		kind == contracts.OperationKindDisconnectAuthorization {
		release := connector.Release
		return &contracts.OperationTarget{
			ConnectorKey:   release.ConnectorKey,
			Version:        release.Version,
			ReleaseID:      release.ReleaseID,
			ReleaseDigest:  release.ReleaseDigest,
			ArtifactSHA256: release.Artifact.SHA256,
			Release:        &release,
		}
	}
	if kind == contracts.OperationKindUninstall || kind == contracts.OperationKindReconcileRuntime {
		return &contracts.OperationTarget{
			ConnectorKey:  connector.Key,
			Version:       connector.Installation.InstalledVersion,
			ReleaseID:     connector.Installation.InstalledReleaseID,
			ReleaseDigest: connector.Installation.InstalledReleaseDigest,
		}
	}
	return nil
}
