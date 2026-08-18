package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AppServerCatalogRequest addresses only connection-global app-server RPCs.
// Session is launch context for preparation/keying; this API never publishes
// a Host Session, creates a provider Thread, or mutates lifecycle state.
type AppServerCatalogRequest struct {
	Session    Session
	RequestSet string
	CWD        string
	ClientName string
}

// AppServerCatalogResult keeps provider response payloads opaque to runtime.
// Consumers adapt the result into product DTOs; runtime owns transport,
// connection reuse, response correlation, and cleanup.
type AppServerCatalogResult struct {
	Models             []json.RawMessage
	CapabilityResponse map[string]json.RawMessage
}

type AppServerCatalogAdapter interface {
	ListAppServerCatalog(context.Context, AppServerCatalogRequest) (AppServerCatalogResult, error)
}

func (c *Controller) ListAppServerCatalog(
	ctx context.Context,
	input AppServerCatalogRequest,
) (AppServerCatalogResult, error) {
	provider := strings.TrimSpace(input.Session.Provider)
	if provider == "" {
		return AppServerCatalogResult{}, errors.New("app-server catalog provider is required")
	}
	adapter := c.adapter(provider)
	catalog, ok := adapter.(AppServerCatalogAdapter)
	if !ok {
		return AppServerCatalogResult{}, fmt.Errorf("provider %q does not expose an app-server catalog", provider)
	}
	return catalog.ListAppServerCatalog(ctx, input)
}
