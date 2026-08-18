package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

type appServerCatalogRequestSet string

const (
	appServerCatalogRequestSetCodex      appServerCatalogRequestSet = "codex"
	appServerCatalogRequestSetSkillsOnly appServerCatalogRequestSet = "skills_only"
)

type CodexCLICapabilityLister struct {
	RequestSet appServerCatalogRequestSet
	Catalog    AppServerCatalogReader
	Provider   string
}

// ParseAppServerCapabilityResponses adapts connection-global runtime payloads
// into Composer DTOs. The runtime owns the RPC connection and response
// correlation; this package owns only product presentation mapping.
func ParseAppServerCapabilityResponses(
	responses map[string]json.RawMessage,
	requestSet string,
) []ComposerCapabilityOption {
	options := make([]ComposerCapabilityOption, 0)
	if raw := responses["skills/list"]; len(raw) > 0 {
		options = append(options, parseCodexSkillCapabilities(raw)...)
	}
	if strings.TrimSpace(requestSet) != string(appServerCatalogRequestSetCodex) {
		return dedupeComposerCapabilityOptions(options)
	}
	if raw := responses["app/list"]; len(raw) > 0 {
		options = append(options, parseCodexAppCapabilities(raw)...)
	}
	if raw := responses["plugin/list"]; len(raw) > 0 {
		options = append(options, parseCodexPluginCapabilities(raw)...)
	}
	if raw := responses["mcpServerStatus/list"]; len(raw) > 0 {
		options = append(options, parseCodexMCPCapabilities(raw)...)
	}
	return dedupeComposerCapabilityOptions(options)
}

type defaultComposerCapabilityLister struct {
	catalog AppServerCatalogReader
}

func (s defaultComposerCapabilityLister) ListComposerCapabilityOptions(
	ctx context.Context,
	provider string,
	cwd string,
	fallbackSkills []ComposerSkillOption,
) ([]ComposerCapabilityOption, []string) {
	return discoverComposerCapabilityOptionsWithCatalog(ctx, provider, cwd, fallbackSkills, s.catalog)
}

func (s defaultComposerCapabilityLister) ListComposerCapabilityOptionsWithPreparation(
	ctx context.Context,
	provider string,
	cwd string,
	fallbackSkills []ComposerSkillOption,
	preparation *runtimeprep.PrepareInput,
) ([]ComposerCapabilityOption, []string) {
	return discoverComposerCapabilityOptionsWithCatalog(ctx, provider, cwd, fallbackSkills, s.catalog, preparation)
}

func (s *Service) composerCapabilityLister() ComposerCapabilityLister {
	if s.CapabilityLister != nil {
		return s.CapabilityLister
	}
	return defaultComposerCapabilityLister{catalog: s.AppServerCatalog}
}

func discoverComposerCapabilityOptionsWithCatalog(
	ctx context.Context,
	provider string,
	cwd string,
	fallbackSkills []ComposerSkillOption,
	catalog AppServerCatalogReader,
	preparation ...*runtimeprep.PrepareInput,
) ([]ComposerCapabilityOption, []string) {
	fallback := composerCapabilityCatalogFromSkills(provider, fallbackSkills)
	lister, ok, err := composerCapabilityCatalogLister(composerProfileFor(provider))
	if err != nil {
		return fallback, []string{err.Error()}
	}
	if !ok {
		return fallback, nil
	}
	lister.Provider = provider
	lister.Catalog = catalog
	var prepared *runtimeprep.PrepareInput
	if len(preparation) > 0 {
		prepared = preparation[0]
	}
	options, err := lister.ListWithPreparation(ctx, cwd, prepared)
	if err != nil {
		return fallback, []string{err.Error()}
	}
	return mergeComposerCapabilityOptions(fallback, options), nil
}

func composerCapabilityCatalogLister(profile composerProfile) (CodexCLICapabilityLister, bool, error) {
	switch profile.CapabilityCatalogKind {
	case "":
		return CodexCLICapabilityLister{}, false, nil
	case providerregistry.CapabilityCatalogKindCodexAppServer, providerregistry.CapabilityCatalogKindAppServerSkills:
		requestSet := appServerCatalogRequestSetCodex
		if profile.CapabilityCatalogKind == providerregistry.CapabilityCatalogKindAppServerSkills {
			requestSet = appServerCatalogRequestSetSkillsOnly
		}
		return CodexCLICapabilityLister{
			RequestSet: requestSet,
		}, true, nil
	default:
		return CodexCLICapabilityLister{}, false, fmt.Errorf("unsupported capability catalog kind %q", profile.CapabilityCatalogKind)
	}
}

func (l CodexCLICapabilityLister) List(ctx context.Context, cwd string) ([]ComposerCapabilityOption, error) {
	return l.ListWithPreparation(ctx, cwd, nil)
}

func (l CodexCLICapabilityLister) ListWithPreparation(ctx context.Context, cwd string, preparation *runtimeprep.PrepareInput) ([]ComposerCapabilityOption, error) {
	if l.Catalog == nil {
		return nil, fmt.Errorf("app-server catalog reader is unavailable")
	}
	result, err := l.Catalog.ListAppServerCatalog(ctx, AppServerCatalogRequest{
		Preparation: preparation, Provider: l.Provider, Cwd: cwd, ClientName: "tuttid", RequestSet: string(l.RequestSet),
	})
	if err != nil {
		return nil, err
	}
	return append([]ComposerCapabilityOption(nil), result.Capabilities...), nil
}

func parseCodexSkillCapabilities(raw json.RawMessage) []ComposerCapabilityOption {
	var result struct {
		Data []struct {
			Skills []map[string]any `json:"skills"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	options := make([]ComposerCapabilityOption, 0)
	for _, group := range result.Data {
		for _, skill := range group.Skills {
			name := codexTextValue(skill, "name")
			if name == "" {
				continue
			}
			label := firstNonEmptyString(codexTextValue(codexNestedMap(skill, "interface"), "displayName"), name)
			description := firstNonEmptyString(
				codexTextValue(codexNestedMap(skill, "interface"), "shortDescription"),
				codexTextValue(skill, "description"),
			)
			status := "available"
			if enabled, ok := codexBoolValue(skill, "enabled"); ok && !enabled {
				status = "disabled"
			}
			path := codexTextValue(skill, "path")
			options = append(options, ComposerCapabilityOption{
				ID:          "skill:" + name,
				Kind:        "skill",
				Name:        name,
				Label:       label,
				Description: description,
				Status:      status,
				Trigger:     "$" + name,
				Path:        path,
				Invocation:  "promptItem",
			})
		}
	}
	return options
}

func parseCodexAppCapabilities(raw json.RawMessage) []ComposerCapabilityOption {
	var result struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	options := make([]ComposerCapabilityOption, 0, len(result.Data))
	for _, app := range result.Data {
		id := codexTextValue(app, "id")
		name := firstNonEmptyString(codexTextValue(app, "name"), id)
		if id == "" || name == "" {
			continue
		}
		status := "available"
		if enabled, ok := codexBoolValue(app, "isEnabled"); ok && !enabled {
			status = "disabled"
		}
		if accessible, ok := codexBoolValue(app, "isAccessible"); ok && !accessible {
			status = "authRequired"
		}
		options = append(options, ComposerCapabilityOption{
			ID:          "connector:" + id,
			Kind:        "connector",
			Name:        id,
			Label:       name,
			Description: codexTextValue(app, "description"),
			Status:      status,
			Trigger:     "$" + id,
			Path:        "app://" + id,
			Invocation:  "promptItem",
		})
	}
	return options
}

func parseCodexPluginCapabilities(raw json.RawMessage) []ComposerCapabilityOption {
	var result struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	options := make([]ComposerCapabilityOption, 0, len(result.Data))
	for _, plugin := range result.Data {
		name := firstNonEmptyString(codexTextValue(plugin, "name"), codexTextValue(plugin, "id"), codexTextValue(plugin, "pluginName"))
		if name == "" {
			continue
		}
		label := firstNonEmptyString(codexTextValue(plugin, "displayName"), codexTextValue(plugin, "title"), name)
		options = append(options, ComposerCapabilityOption{
			ID:          "plugin:" + name,
			Kind:        "plugin",
			Name:        name,
			Label:       label,
			Description: codexTextValue(plugin, "description"),
			Status:      "available",
			Source:      codexPluginSource(plugin),
			PluginName:  name,
			Invocation:  "none",
		})
	}
	return options
}

func parseCodexMCPCapabilities(raw json.RawMessage) []ComposerCapabilityOption {
	var result struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	options := make([]ComposerCapabilityOption, 0)
	for _, server := range result.Data {
		name := firstNonEmptyString(codexTextValue(server, "name"), codexTextValue(server, "serverName"))
		if name == "" {
			continue
		}
		status := normalizeCodexMCPStatus(codexTextValue(server, "status"))
		options = append(options, ComposerCapabilityOption{
			ID:         "mcpServer:" + name,
			Kind:       "mcpServer",
			Name:       name,
			Label:      name,
			Status:     status,
			ServerName: name,
			Invocation: "none",
		})
		for _, tool := range codexSliceOfMaps(server["tools"]) {
			toolName := firstNonEmptyString(codexTextValue(tool, "name"), codexTextValue(tool, "toolName"))
			if toolName == "" {
				continue
			}
			options = append(options, ComposerCapabilityOption{
				ID:          "mcpTool:" + name + "/" + toolName,
				Kind:        "mcpTool",
				Name:        toolName,
				Label:       toolName,
				Description: codexTextValue(tool, "description"),
				Status:      status,
				ServerName:  name,
				ToolName:    toolName,
				Invocation:  "none",
			})
		}
	}
	return options
}

func normalizeCodexMCPStatus(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(normalized, "auth"):
		return "authRequired"
	case strings.Contains(normalized, "fail"), strings.Contains(normalized, "error"), strings.Contains(normalized, "disabled"):
		return "setupRequired"
	default:
		return "available"
	}
}

func codexPluginSource(plugin map[string]any) string {
	source := codexNestedMap(plugin, "source")
	if source == nil {
		return codexTextValue(plugin, "source")
	}
	return firstNonEmptyString(codexTextValue(source, "type"), codexTextValue(source, "url"), codexTextValue(source, "path"))
}

func codexTextValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func codexBoolValue(values map[string]any, key string) (bool, bool) {
	if values == nil {
		return false, false
	}
	value, ok := values[key].(bool)
	return value, ok
}

func codexNestedMap(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	value, _ := values[key].(map[string]any)
	return value
}

func codexSliceOfMaps(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record, ok := item.(map[string]any); ok {
			result = append(result, record)
		}
	}
	return result
}

func mergeComposerCapabilityOptions(left []ComposerCapabilityOption, right []ComposerCapabilityOption) []ComposerCapabilityOption {
	if len(left) == 0 {
		return dedupeComposerCapabilityOptions(right)
	}
	if len(right) == 0 {
		return dedupeComposerCapabilityOptions(left)
	}
	return dedupeComposerCapabilityOptions(append(append([]ComposerCapabilityOption{}, left...), right...))
}

func dedupeComposerCapabilityOptions(options []ComposerCapabilityOption) []ComposerCapabilityOption {
	if len(options) == 0 {
		return []ComposerCapabilityOption{}
	}
	seen := map[string]struct{}{}
	result := make([]ComposerCapabilityOption, 0, len(options))
	for _, option := range options {
		id := strings.TrimSpace(option.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, option)
	}
	return result
}
