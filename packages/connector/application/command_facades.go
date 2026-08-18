package application

import (
	"context"
	"errors"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (facade commandFacades) RefreshCatalog(ctx context.Context, mutation contracts.Mutation) contracts.CommandResult {
	result, err := facade.service.RefreshCatalog(ctx, mutation)
	return normalizeMutationCommandResult(result, err)
}

func (facade commandFacades) Install(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	result, err := facade.service.Install(ctx, mutation)
	return normalizeMutationCommandResult(result, err)
}

func (facade commandFacades) Uninstall(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	result, err := facade.service.Uninstall(ctx, mutation)
	return normalizeMutationCommandResult(result, err)
}

func (facade commandFacades) DisconnectAuthorization(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	result, err := facade.service.DisconnectAuthorization(ctx, mutation)
	return normalizeMutationCommandResult(result, err)
}

func (facade commandFacades) BeginAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	secret []byte,
) contracts.AuthorizationCommandResult {
	result, err := facade.service.BeginAuthorization(ctx, mutation, secret)
	if err != nil {
		return contracts.AuthorizationCommandResult{CommandResult: normalizeCommandFailure(err)}
	}
	operation := result.Operation
	connector := result.Connector
	expiresAt := result.AuthorizationExpiresAt
	outcome := contracts.CommandAccepted
	if operation.State == contracts.OperationStateCompleted {
		outcome = contracts.CommandCompleted
	}
	return contracts.AuthorizationCommandResult{
		CommandResult: contracts.CommandResult{
			Outcome: outcome, Revision: result.Revision,
			Connector: &connector, Operation: &operation,
		},
		AuthorizationURL: result.AuthorizationURL, AuthorizationView: result.AuthorizationView,
		AuthorizationExpiresAt: &expiresAt,
	}
}

func (facade commandFacades) CancelAuthorization(
	ctx context.Context,
	command contracts.CancelAuthorizationCommand,
) contracts.CommandResult {
	revision, err := facade.service.cancelAuthorizationCommand(ctx, command)
	if err != nil {
		return normalizeCommandFailure(err)
	}
	return contracts.CommandResult{Outcome: contracts.CommandCompleted, Revision: revision}
}

func normalizeMutationCommandResult(result contracts.MutationResult, err error) contracts.CommandResult {
	if err != nil {
		return normalizeCommandFailure(err)
	}
	operation := result.Operation
	outcome := contracts.CommandAccepted
	if operation.State == contracts.OperationStateCompleted {
		outcome = contracts.CommandCompleted
	}
	return contracts.CommandResult{
		Outcome: outcome, Revision: result.Revision,
		Connector: result.Connector, Operation: &operation,
	}
}

func normalizeCommandFailure(err error) contracts.CommandResult {
	failure := contracts.CommandFailure{
		Code: contracts.ErrorCodeUnavailable, Retryable: true,
		Message: "connector command acceptance could not be determined",
	}
	outcome := contracts.CommandUncertain
	var domainError *contracts.DomainError
	if errors.As(err, &domainError) {
		outcome = contracts.CommandRejected
		failure = contracts.CommandFailure{
			Code: domainError.Code, Retryable: domainError.Retryable,
			Message: strings.TrimSpace(domainError.Message),
		}
		if failure.Code == contracts.ErrorCodeRevisionConflict {
			failure.Retryable = false
		}
	} else if errors.Is(err, contracts.ErrNotFound) {
		outcome = contracts.CommandRejected
		failure = contracts.CommandFailure{
			Code: contracts.ErrorCodeNotFound, Retryable: false,
			Message: "connector market resource was not found",
		}
	}
	return contracts.CommandResult{Outcome: outcome, Failure: &failure}
}
