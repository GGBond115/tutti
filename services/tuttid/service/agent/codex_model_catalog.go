package agent

import (
	"context"
	"errors"
	"strings"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

type CodexCLIModelLister struct {
	Provider   string
	Cwd        string
	ClientName string
	// Catalog routes model/list through the runtime-owned connection registry.
	Catalog     AppServerCatalogReader
	Preparation *runtimeprep.PrepareInput
}

// WithPreparation returns a request-scoped lister carrying the exact runtime
// preparation for this catalog request. CachedAgentModelCatalog may inject a
// configured lister, but that lister must not silently discard per-request
// process-profile identity.
func (l CodexCLIModelLister) WithPreparation(preparation *runtimeprep.PrepareInput) AgentModelLister {
	l.Preparation = preparation
	return l
}

func (l CodexCLIModelLister) ListModels(ctx context.Context) (AgentModelListResult, error) {
	if l.Catalog == nil {
		return AgentModelListResult{}, errors.New("app-server catalog reader is unavailable")
	}
	result, err := l.Catalog.ListAppServerCatalog(ctx, AppServerCatalogRequest{
		Preparation: l.Preparation, Provider: l.Provider, Cwd: l.Cwd, ClientName: l.clientName(), RequestSet: "model",
	})
	if err != nil {
		return AgentModelListResult{}, err
	}
	return AgentModelListResult{Models: append([]AgentModelOption(nil), result.Models...)}, nil
}

func (l CodexCLIModelLister) clientName() string {
	if name := strings.TrimSpace(l.ClientName); name != "" {
		return name
	}
	return "tuttid"
}
