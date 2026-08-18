package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const (
	appServerCatalogRequestSetCodex      = "codex"
	appServerCatalogRequestSetSkillsOnly = "skills_only"
)

func (a *CodexAppServerAdapter) ListAppServerCatalog(
	ctx context.Context,
	input AppServerCatalogRequest,
) (AppServerCatalogResult, error) {
	if a == nil {
		return AppServerCatalogResult{}, errors.New("app-server adapter is unavailable")
	}
	session := input.Session
	if strings.TrimSpace(session.Provider) == "" {
		session.Provider = a.config.provider
	}
	if strings.TrimSpace(session.CWD) == "" {
		session.CWD = strings.TrimSpace(input.CWD)
	}
	if strings.TrimSpace(session.CWD) == "" {
		return AppServerCatalogResult{}, errors.New("app-server catalog cwd is required")
	}
	trace := newCodexAppServerStartupTrace(session)
	launch, err := a.prepareInitializedClientLaunch(ctx, session)
	if err != nil {
		return AppServerCatalogResult{}, err
	}
	client, _, _, err := a.startClientPrepared(ctx, session, trace, launch, false, true)
	if err != nil {
		return AppServerCatalogResult{}, errors.Join(err, a.connections.cleanupOrRetain(launch.threadCleanup))
	}
	connection := a.connections.connectionForClient(client)
	if connection == nil {
		cleanupErr := a.connections.cleanupOrRetain(launch.threadCleanup)
		_ = client.Close()
		return AppServerCatalogResult{}, errors.Join(errors.New("app-server catalog connection is unavailable"), cleanupErr)
	}
	result, err := connection.listCatalog(ctx, input.RequestSet, session.CWD)
	cleanupErr := a.connections.cleanupOrRetain(launch.threadCleanup)
	return result, errors.Join(err, cleanupErr)
}

func (c *appServerConnection) listCatalog(
	ctx context.Context,
	requestSet string,
	cwd string,
) (AppServerCatalogResult, error) {
	requestSet = strings.TrimSpace(requestSet)
	if requestSet == "" {
		requestSet = appServerCatalogRequestSetCodex
	}
	result := AppServerCatalogResult{}
	switch requestSet {
	case "model":
		models, err := c.catalogCall(ctx, appServerMethodModelList, map[string]any{"limit": 200})
		if err != nil {
			return AppServerCatalogResult{}, err
		}
		result.Models = catalogDataRows(models)
		return result, nil
	case appServerCatalogRequestSetSkillsOnly, appServerCatalogRequestSetCodex:
	default:
		return AppServerCatalogResult{}, errors.New("unsupported app-server catalog request set " + requestSet)
	}
	result.CapabilityResponse = make(map[string]json.RawMessage)
	capabilityCalls := []struct {
		method string
		params map[string]any
	}{
		{method: appServerMethodSkillsList, params: map[string]any{
			"cwds":        catalogCWDs(cwd),
			"forceReload": false,
		}},
	}
	if requestSet == appServerCatalogRequestSetCodex {
		capabilityCalls = append(capabilityCalls,
			struct {
				method string
				params map[string]any
			}{method: appServerMethodAppList, params: map[string]any{"limit": 200, "forceRefetch": false}},
			struct {
				method string
				params map[string]any
			}{method: appServerMethodPluginList, params: map[string]any{"limit": 200}},
			struct {
				method string
				params map[string]any
			}{method: appServerMethodMCPServerStatusList, params: map[string]any{"limit": 200, "detail": "toolsAndAuthOnly"}},
		)
	}
	for _, call := range capabilityCalls {
		response, err := c.catalogCall(ctx, call.method, call.params)
		if err != nil {
			return AppServerCatalogResult{}, err
		}
		result.CapabilityResponse[call.method] = append(json.RawMessage(nil), response...)
	}
	return result, nil
}

func (c *appServerConnection) catalogCall(
	ctx context.Context,
	method string,
	params map[string]any,
) (json.RawMessage, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("app-server connection is unavailable")
	}
	return c.client.RawCallNoHandler(ctx, acpStartCallTimeout, method, params)
}

func catalogCWDs(cwd string) []string {
	if cwd = strings.TrimSpace(cwd); cwd != "" {
		return []string{cwd}
	}
	return []string{}
}

func catalogDataRows(raw json.RawMessage) []json.RawMessage {
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	result := make([]json.RawMessage, 0, len(payload.Data))
	for _, row := range payload.Data {
		result = append(result, append(json.RawMessage(nil), row...))
	}
	return result
}
