package application

import (
	"context"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (application *service) RecoverableOperations(ctx context.Context) ([]contracts.Operation, error) {
	return application.config.Repository.RecoverableOperations(ctx)
}

func (application *service) ResolveAuthorizationSession(
	ctx context.Context,
	operationID string,
	resolution contracts.AuthorizationSessionResolution,
) error {
	return application.config.Repository.ResolveAuthorizationSession(ctx, operationID, resolution)
}
