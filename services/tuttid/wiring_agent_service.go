package main

import (
	"context"

	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
)

type workspaceAgentTargetResolverSetter interface {
	SetWorkspaceAgentTargetResolver(agentservice.WorkspaceAgentTargetResolver)
}

func configureWorkspaceAgentProjection(
	activityProjection workspaceAgentTargetResolverSetter,
	workspaceAgentTargets agentservice.WorkspaceAgentTargetResolver,
) {
	if workspaceAgentTargets != nil {
		activityProjection.SetWorkspaceAgentTargetResolver(workspaceAgentTargets)
	}
}

func agentWorkspaceIDs(
	store workspacedata.CatalogStore,
) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		workspaces, err := store.List(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(workspaces))
		for _, workspace := range workspaces {
			ids = append(ids, workspace.ID)
		}
		return ids, nil
	}
}
