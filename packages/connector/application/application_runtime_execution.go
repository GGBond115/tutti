package application

import (
	"context"
	"errors"
	"strings"
	"time"

	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
)

func (application *service) executeRuntimeReconcile(ctx context.Context, operation contracts.Operation) error {
	convergence, err := application.config.Repository.RuntimeConvergence(ctx, operation.Scope, operation.ConnectorKey)
	needsNewGeneration := err != nil || operation.Stage != contracts.OperationStageRuntimePending ||
		operation.HostGeneration.BootEpoch != application.config.BootEpoch ||
		convergence.Desired.Generation != operation.HostGeneration.Generation
	if err != nil && !errors.Is(err, contracts.ErrNotFound) {
		return err
	}
	if needsNewGeneration {
		convergence, err = application.ensureRuntimeDesired(ctx, operation.Scope, operation.ConnectorKey, true)
		if err != nil {
			return err
		}
		operation, err = application.updateOperationStage(ctx, operation.OperationID, contracts.OperationStageRuntimePending, func(current *contracts.Operation) {
			current.HostGeneration = contracts.HostGeneration{
				BootEpoch:  application.config.BootEpoch,
				Generation: convergence.Desired.Generation,
			}
		})
		if err != nil {
			return err
		}
	}
	if err := application.awaitRuntimeOperationGeneration(ctx, operation); err != nil {
		return err
	}
	return application.completeConnectorOperation(ctx, operation.OperationID, func(current contracts.Connector) contracts.Connector { return current })
}

func (application *service) awaitRuntimeOperationGeneration(ctx context.Context, operation contracts.Operation) error {
	for {
		err := application.ConvergeRuntime(ctx, operation.Scope, operation.ConnectorKey)
		convergence, readErr := application.config.Repository.RuntimeConvergence(ctx, operation.Scope, operation.ConnectorKey)
		if readErr != nil {
			return errors.Join(err, readErr)
		}
		if convergence.Desired.Generation != operation.HostGeneration.Generation {
			return contracts.NewDomainError(contracts.ErrorCodeRevisionConflict, "connector runtime recovery target changed", false, err)
		}
		exactObservation := convergence.Observed.DesiredGeneration == operation.HostGeneration.Generation &&
			convergence.Observed.BootEpoch == application.config.BootEpoch
		readyForIntent := convergence.Observed.Readiness.State == contracts.RuntimeReadinessReady ||
			!convergence.Desired.Enabled && convergence.Observed.Readiness.State == contracts.RuntimeReadinessBlocked &&
				convergence.Observed.Readiness.ReasonCode == contracts.RuntimeReadinessReasonRuntimeDisabled
		if exactObservation && readyForIntent {
			return nil
		}
		if convergence.Attempt >= contracts.RuntimeFailureBudget {
			return contracts.NewDomainError(contracts.ErrorCodeInstallFailed, "connector runtime recovery exhausted its failure budget", false, err)
		}
		if err != nil {
			return err
		}
		wait := 25 * time.Millisecond
		if delay := convergence.NextAttemptAt.Sub(application.config.Now().UTC()); delay > wait {
			wait = delay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (application *service) resolveRuntimeBinding(
	ctx context.Context,
	operation contracts.Operation,
	connector contracts.Connector,
	release contracts.Release,
	purpose contracts.RuntimeBindingPurpose,
) (contracts.RuntimeBinding, error) {
	binding, err := application.config.RuntimeBindings.ResolveRuntimeBinding(ctx, contracts.RuntimeBindingRequest{
		OperationID: operation.OperationID, Scope: operation.Scope, Purpose: purpose, Connector: connector, Release: release,
	})
	if err != nil {
		clear(binding.CredentialBrokerGrant)
		return contracts.RuntimeBinding{}, err
	}
	binding.ConnectionID = strings.TrimSpace(binding.ConnectionID)
	if binding.ConnectionID == "" || (!binding.Enabled && len(binding.CredentialBrokerGrant) != 0) {
		clear(binding.CredentialBrokerGrant)
		return contracts.RuntimeBinding{}, invalidOperationReceipt("runtime binding resolver returned invalid intent")
	}
	return binding, nil
}

func (application *service) reconcileRuntime(ctx context.Context, request contracts.RuntimeReconcileRequest) (contracts.RuntimeReceipt, error) {
	defer clear(request.CredentialBrokerGrant)
	return application.config.ImplementationCommands.Reconcile(ctx, request)
}

type defaultRuntimeBindingResolver struct{}

func (defaultRuntimeBindingResolver) ResolveRuntimeIntent(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeIntent, error) {
	return contracts.RuntimeIntent{ConnectionID: defaultConnectorConnectionID, Enabled: true, AuthorizationState: contracts.AuthorizationStateNotRequired}, nil
}

func (defaultRuntimeBindingResolver) ResolveRuntimeBinding(context.Context, contracts.RuntimeBindingRequest) (contracts.RuntimeBinding, error) {
	return contracts.RuntimeBinding{ConnectionID: defaultConnectorConnectionID, Enabled: true, AuthorizationState: contracts.AuthorizationStateNotRequired}, nil
}
