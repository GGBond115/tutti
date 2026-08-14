package main

import (
	"context"
	"log/slog"

	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
	eventstreamservice "github.com/tutti-os/tutti/services/tuttid/service/eventstream"
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

func startAgentModelInvalidationAuthWatcher(
	replayComposition bool,
	modelCatalog *agentservice.CachedAgentModelCatalog,
	sessions *agentservice.Service,
	events *eventstreamservice.Service,
) *agentservice.ProviderAuthWatcher {
	// External credential switchers (for example cc-switch) rewrite provider
	// auth/config files without notifying tuttid. Watch those files so model
	// catalogs become stale, provider sessions are closed, and the GUI hears
	// about it immediately.
	publisher := eventstreamservice.AgentModelCatalogPublisher{Service: events}
	publish := func(providers []string, event string) {
		if len(providers) == 0 {
			return
		}
		if err := publisher.PublishAgentModelCatalogInvalidated(context.Background(), providers); err != nil {
			slog.Warn("agent model catalog invalidation publish failed",
				"event", "agent.model_catalog.invalidation_publish_failed",
				"providers", providers,
				"error", err,
			)
			return
		}
		slog.Info("agent model catalog invalidation published",
			"event", event,
			"providers", providers,
		)
	}
	watcher := startProviderAuthWatcher(replayComposition, func(providers []string) {
		modelCatalog.Invalidate(providers...)
		for _, provider := range providers {
			sessions.InvalidateLiveComposerModels(provider)
		}
		publish(providers, "agent.model_catalog.invalidated")
	})
	if watcher != nil {
		modelCatalog.OnRefresh = func(provider string) {
			publish([]string{provider}, "agent.model_catalog.refreshed")
		}
		watcher.OnClose = func() {
			_ = modelCatalog.Close()
		}
	}
	return watcher
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
