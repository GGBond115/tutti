package application

import (
	"context"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"strings"
)

func (application *service) executeRuntimeReconcile(ctx context.Context, operation contracts.Operation) error {
	connector, err := application.config.Repository.Connector(ctx, operation.ConnectorKey)
	if err != nil {
		return err
	}
	release, err := application.installedReleaseEvidence(ctx, connector)
	if err != nil {
		return err
	}
	connector.Release = release
	binding, err := application.resolveRuntimeBinding(ctx, operation, connector, release, contracts.RuntimeBindingPurposeReconcile)
	if err != nil {
		return err
	}
	connector.Authorization.State = binding.AuthorizationState
	receipt, err := application.reconcileRuntime(ctx, contracts.RuntimeReconcileRequest{
		OperationID: operation.OperationID, Scope: operation.Scope, ConnectionID: binding.ConnectionID,
		Connector: connector, Enabled: binding.Enabled, Generation: operation.HostGeneration,
		CredentialBrokerGrant: binding.CredentialBrokerGrant,
	})
	if err != nil {
		return contracts.NewDomainError(contracts.ErrorCodeInstallFailed, "connector runtime could not be reconciled", true, err)
	}
	if err := validateRuntimeReceipt(receipt, operation.OperationID, binding.ConnectionID, connector.Key,
		release.ReleaseDigest, operation.HostGeneration, binding.Enabled); err != nil {
		return err
	}
	return application.completeConnectorOperation(ctx, operation.OperationID, func(current contracts.Connector) contracts.Connector { return current })
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
