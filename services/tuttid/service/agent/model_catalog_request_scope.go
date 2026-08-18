package agent

import (
	"context"
	"strings"
	"sync"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

type requestScopedAgentModelCatalogKey struct{}
type requestScopedAgentModelCatalogPreparationKey struct{}

type requestScopedAgentModelCatalog struct {
	delegate AgentModelCatalog
	mu       sync.Mutex
	entries  map[AgentModelCatalogInput]requestScopedAgentModelCatalogEntry
}

type requestScopedAgentModelCatalogEntry struct {
	result AgentModelCatalogResult
	err    error
}

func withRequestScopedAgentModelCatalog(ctx context.Context, catalog AgentModelCatalog) context.Context {
	if catalog == nil {
		return ctx
	}
	return context.WithValue(ctx, requestScopedAgentModelCatalogKey{}, &requestScopedAgentModelCatalog{
		delegate: catalog,
		entries:  make(map[AgentModelCatalogInput]requestScopedAgentModelCatalogEntry),
	})
}

func withRequestScopedAgentModelCatalogPreparation(ctx context.Context, preparation *runtimeprep.PrepareInput) context.Context {
	if preparation == nil {
		return ctx
	}
	return context.WithValue(ctx, requestScopedAgentModelCatalogPreparationKey{}, preparation)
}

func (s *Service) modelCatalogForContext(ctx context.Context) AgentModelCatalog {
	if catalog, ok := ctx.Value(requestScopedAgentModelCatalogKey{}).(AgentModelCatalog); ok && catalog != nil {
		return catalog
	}
	if s == nil {
		return nil
	}
	return s.ModelCatalog
}

func (c *requestScopedAgentModelCatalog) ListModels(ctx context.Context, input AgentModelCatalogInput) (AgentModelCatalogResult, error) {
	input.Provider = agentprovider.Normalize(input.Provider)
	input.Cwd = strings.TrimSpace(input.Cwd)
	if input.Preparation == nil {
		if preparation, ok := ctx.Value(requestScopedAgentModelCatalogPreparationKey{}).(*runtimeprep.PrepareInput); ok &&
			preparation != nil &&
			agentprovider.Normalize(preparation.Provider) == input.Provider &&
			strings.TrimSpace(preparation.Cwd) == input.Cwd {
			input.Preparation = preparation
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[input]; ok {
		return cloneAgentModelCatalogResult(entry.result), entry.err
	}

	result, err := c.delegate.ListModels(ctx, input)
	c.entries[input] = requestScopedAgentModelCatalogEntry{
		result: cloneAgentModelCatalogResult(result),
		err:    err,
	}
	return cloneAgentModelCatalogResult(result), err
}
