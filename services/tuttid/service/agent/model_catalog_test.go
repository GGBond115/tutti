package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCodexModelCatalogConfig(t *testing.T, contents string) {
	t.Helper()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(filepath.Join(codexHome, codexConfigFileName), []byte(contents), 0o600); err != nil {
		t.Fatalf("write codex config.toml: %v", err)
	}
}

// Codex app-server owns model discovery even when requests are routed through
// a custom model_provider. Tutti must not discard its model/list response.
func TestAgentModelCatalogCustomModelProviderPreservesDiscoveredModels(t *testing.T) {
	writeCodexModelCatalogConfig(t,
		"model_provider = \"openrouter\"\n"+
			"model = \"gpt-5.6-sol\"\n\n"+
			"[model_providers.openrouter]\n"+
			"base_url = \"https://openrouter.ai/api/v1\"\n")
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{
			{
				ID:          "gpt-5.6-sol",
				DisplayName: "GPT-5.6 Sol",
				SupportedReasoningEfforts: []AgentModelReasoningEffortOption{
					{Value: "low"},
					{Value: "medium"},
					{Value: "high"},
					{Value: "xhigh"},
					{Value: "max"},
				},
			},
			{ID: "gpt-5.5", DisplayName: "GPT-5.5"},
			{ID: "gpt-5.4", DisplayName: "GPT-5.4"},
		},
	}
	catalog := &CachedAgentModelCatalog{
		Codex: lister,
		Now: func() time.Time {
			return time.UnixMilli(1000)
		},
	}

	result, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(result.Models) != 3 {
		t.Fatalf("models = %#v, want the complete discovered catalog", result.Models)
	}
	if result.Models[0].ID != "gpt-5.6-sol" || !result.Models[0].IsDefault {
		t.Fatalf("configured model = %#v, want gpt-5.6-sol as default", result.Models[0])
	}
	wantEfforts := []string{"low", "medium", "high", "xhigh", "max"}
	if len(result.Models[0].SupportedReasoningEfforts) != len(wantEfforts) {
		t.Fatalf("reasoning efforts = %#v, want %v", result.Models[0].SupportedReasoningEfforts, wantEfforts)
	}
	for index, want := range wantEfforts {
		if got := result.Models[0].SupportedReasoningEfforts[index].Value; got != want {
			t.Fatalf("reasoning effort %d = %q, want %q", index, got, want)
		}
	}
	for index, modelID := range []string{"gpt-5.5", "gpt-5.4"} {
		model := result.Models[index+1]
		if model.ID != modelID || model.IsDefault {
			t.Fatalf("discovered model %d = %#v, want non-default %s", index+1, model, modelID)
		}
	}
	if result.Source != "codex-cli" {
		t.Fatalf("source = %q, want codex-cli", result.Source)
	}
}

func TestAgentModelCatalogCustomModelProviderKeepsConfiguredCatalog(t *testing.T) {
	writeCodexModelCatalogConfig(t,
		"model_provider = \"openrouter\"\n"+
			"model = \"~moonshotai/kimi-latest\"\n"+
			"model_catalog_json = \"cc-switch-model-catalog.json\"\n\n"+
			"[model_providers.openrouter]\n"+
			"base_url = \"https://openrouter.ai/api/v1\"\n")
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{
			{ID: "~moonshotai/kimi-latest", DisplayName: "Kimi Latest"},
			{ID: "~x-ai/grok-latest", DisplayName: "Grok Latest", IsDefault: true},
		},
	}
	catalog := &CachedAgentModelCatalog{
		Codex: lister,
		Now: func() time.Time {
			return time.UnixMilli(1000)
		},
	}

	result, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("models = %#v, want configured custom-provider catalog", result.Models)
	}
	if result.Models[0].ID != "~moonshotai/kimi-latest" || !result.Models[0].IsDefault {
		t.Fatalf("first model = %#v, want configured model marked default", result.Models[0])
	}
	if result.Models[1].ID != "~x-ai/grok-latest" || result.Models[1].IsDefault {
		t.Fatalf("second model = %#v, want non-default catalog model", result.Models[1])
	}
	if result.Source != "codex-cli" {
		t.Fatalf("source = %q, want codex-cli", result.Source)
	}
}

func TestAgentModelCatalogCustomModelProviderPreservesUnrelatedDiscoveredCatalog(t *testing.T) {
	writeCodexModelCatalogConfig(t,
		"model_provider = \"openrouter\"\n"+
			"model = \"~moonshotai/kimi-latest\"\n"+
			"model_catalog_json = \"cc-switch-model-catalog.json\"\n\n"+
			"[model_providers.openrouter]\n"+
			"base_url = \"https://openrouter.ai/api/v1\"\n")
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{
			{ID: "gpt-5.5", DisplayName: "GPT-5.5", IsDefault: true},
			{ID: "gpt-5.4", DisplayName: "GPT-5.4"},
		},
	}
	catalog := &CachedAgentModelCatalog{
		Codex: lister,
		Now: func() time.Time {
			return time.UnixMilli(1000)
		},
	}

	result, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(result.Models) != 3 {
		t.Fatalf("models = %#v, want discovered models plus configured default", result.Models)
	}
	if result.Models[0].ID != "gpt-5.5" || result.Models[0].IsDefault ||
		result.Models[1].ID != "gpt-5.4" || result.Models[1].IsDefault {
		t.Fatalf("discovered models = %#v, want preserved non-default catalog", result.Models[:2])
	}
	if result.Models[2].ID != "~moonshotai/kimi-latest" || !result.Models[2].IsDefault {
		t.Fatalf("configured model = %#v, want appended default", result.Models[2])
	}
	if result.Source != "codex-cli" {
		t.Fatalf("source = %q, want codex-cli", result.Source)
	}
}

func TestAgentModelCatalogDefaultProviderKeepsOfficialListWithConfiguredDefault(t *testing.T) {
	writeCodexModelCatalogConfig(t, "model = \"gpt-5.5\"\n")
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{
			{ID: "gpt-5.5", DisplayName: "GPT-5.5"},
			{ID: "gpt-5.4", DisplayName: "GPT-5.4"},
		},
	}
	catalog := &CachedAgentModelCatalog{
		Codex: lister,
		Now: func() time.Time {
			return time.UnixMilli(1000)
		},
	}

	result, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("models = %#v, want official catalog untouched", result.Models)
	}
	if !result.Models[0].IsDefault || result.Models[0].ID != "gpt-5.5" {
		t.Fatalf("models = %#v, want configured gpt-5.5 marked default", result.Models)
	}
}

func TestAgentModelCatalogDoesNotReturnClaudeStaticModels(t *testing.T) {
	catalog := &CachedAgentModelCatalog{
		Now: func() time.Time {
			return time.UnixMilli(1000)
		},
	}

	if _, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "claude-code"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("ListModels error = %v, want ErrInvalidArgument", err)
	}
}

func TestAgentModelCatalogInvalidateServesStaleModelsWhileRefreshing(t *testing.T) {
	lister := &sequencedAgentModelLister{
		models: []AgentModelListResult{
			{Models: []AgentModelOption{{ID: "gpt-5.2-codex", DisplayName: "Old", IsDefault: true}}},
			{Models: []AgentModelOption{{ID: "gpt-5.3-codex", DisplayName: "New", IsDefault: true}}},
		},
		secondStarted: make(chan struct{}),
		secondRelease: make(chan struct{}),
	}
	refreshed := make(chan string, 1)
	catalog := &CachedAgentModelCatalog{
		Codex: lister,
		OnRefresh: func(provider string) {
			refreshed <- provider
		},
	}

	first, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("first ListModels returned error: %v", err)
	}

	catalog.Invalidate("codex")
	stale, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("ListModels after invalidate returned error: %v", err)
	}
	if !stale.Stale || stale.Models[0].ID != first.Models[0].ID {
		t.Fatalf("stale result = %#v, want old catalog marked stale", stale)
	}
	select {
	case <-lister.secondStarted:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
	close(lister.secondRelease)
	select {
	case provider := <-refreshed:
		if provider != "codex" {
			t.Fatalf("refresh provider = %q, want codex", provider)
		}
	case <-time.After(time.Second):
		t.Fatal("background refresh did not settle")
	}
	fresh, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("fresh ListModels returned error: %v", err)
	}
	if fresh.Stale || fresh.Models[0].ID != "gpt-5.3-codex" {
		t.Fatalf("fresh result = %#v, want new catalog", fresh)
	}
}

func TestAgentModelCatalogInvalidateIgnoresOtherProviders(t *testing.T) {
	now := time.UnixMilli(1000)
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{{ID: "gpt-5.2-codex", DisplayName: "gpt-5.2-codex", IsDefault: true}},
	}
	catalog := &CachedAgentModelCatalog{
		Codex: lister,
		Now: func() time.Time {
			return now
		},
	}

	if _, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"}); err != nil {
		t.Fatalf("first ListModels returned error: %v", err)
	}
	catalog.Invalidate("claude-code", "unknown-provider")
	if _, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"}); err != nil {
		t.Fatalf("second ListModels returned error: %v", err)
	}
	if lister.calls != 1 {
		t.Fatalf("lister calls = %d, want 1 (codex cache must survive unrelated invalidations)", lister.calls)
	}
}

func TestAgentModelCatalogEnrichesOpenCodeModelsWithImageCapability(t *testing.T) {
	now := time.UnixMilli(1000)
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{{
			ID:          "openai/gpt-5.2-pro",
			DisplayName: "GPT-5.2 Pro",
			IsDefault:   true,
		}},
	}
	catalog := &CachedAgentModelCatalog{
		OpenCode: lister,
		ModelCapabilities: fakeModelCapabilitiesResolver{
			"opencode:openai/gpt-5.2-pro": true,
		},
		Now: func() time.Time {
			return now
		},
	}

	result, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "opencode"})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(result.Models) != 1 {
		t.Fatalf("models = %#v, want one OpenCode model", result.Models)
	}
	if result.Models[0].SupportsImageInput == nil || !*result.Models[0].SupportsImageInput {
		t.Fatalf("supportsImageInput = %#v, want true", result.Models[0].SupportsImageInput)
	}
}

func TestAgentModelCatalogDoesNotCacheOpenCodeModels(t *testing.T) {
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{{ID: "opencode/big-pickle", DisplayName: "Big Pickle"}},
	}
	catalog := &CachedAgentModelCatalog{OpenCode: lister}
	input := AgentModelCatalogInput{Provider: "opencode", Cwd: "/workspace"}

	if _, err := catalog.ListModels(context.Background(), input); err != nil {
		t.Fatalf("first ListModels returned error: %v", err)
	}
	if _, err := catalog.ListModels(context.Background(), input); err != nil {
		t.Fatalf("second ListModels returned error: %v", err)
	}
	if lister.calls != 2 {
		t.Fatalf("lister calls = %d, want 2 uncached OpenCode fetches", lister.calls)
	}
	if _, ok := catalog.cache["opencode"]; ok {
		t.Fatal("OpenCode result must not be stored in the model catalog cache")
	}
}

func TestAgentModelCatalogDoesNotCacheOpenCodeErrors(t *testing.T) {
	lister := &fakeAgentModelLister{err: errors.New("opencode unavailable")}
	catalog := &CachedAgentModelCatalog{OpenCode: lister}
	input := AgentModelCatalogInput{Provider: "opencode", Cwd: "/workspace"}

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := catalog.ListModels(context.Background(), input); err == nil {
			t.Fatalf("ListModels attempt %d returned no error", attempt+1)
		}
	}
	if lister.calls != 2 {
		t.Fatalf("lister calls = %d, want 2 uncached OpenCode failures", lister.calls)
	}
	if _, ok := catalog.cache["opencode"]; ok {
		t.Fatal("OpenCode error must not be stored in the model catalog cache")
	}
}

func TestAgentModelCatalogCachePolicyAcrossProviders(t *testing.T) {
	tests := []struct {
		provider string
		cached   bool
	}{
		{provider: "codex", cached: true},
		{provider: "opencode", cached: false},
		{provider: "tutti-agent", cached: true},
	}
	if len(agentModelCatalogSpecs) != len(tests) {
		t.Fatalf("model catalog specs = %d, want reviewed cache policy for %d providers", len(agentModelCatalogSpecs), len(tests))
	}
	for _, test := range tests {
		spec, ok := agentModelCatalogSpecs[test.provider]
		if !ok {
			t.Fatalf("model catalog spec missing for %s", test.provider)
		}
		if got := specCachesModelCatalog(spec); got != test.cached {
			t.Fatalf("provider %s cached = %v, want %v", test.provider, got, test.cached)
		}
	}
}

func TestAgentModelCatalogListsTuttiAgentModelsFromLiveLister(t *testing.T) {
	now := time.UnixMilli(1000)
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{{ID: "gpt-5.4", DisplayName: "GPT-5.4", IsDefault: true}},
	}
	catalog := &CachedAgentModelCatalog{
		TuttiAgent: lister,
		Now: func() time.Time {
			return now
		},
	}

	first, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "tutti-agent"})
	if err != nil {
		t.Fatalf("first ListModels returned error: %v", err)
	}
	second, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "tutti-agent"})
	if err != nil {
		t.Fatalf("second ListModels returned error: %v", err)
	}

	if lister.calls != 1 {
		t.Fatalf("lister calls = %d, want 1", lister.calls)
	}
	if first.Provider != "tutti-agent" {
		t.Fatalf("provider = %q, want tutti-agent", first.Provider)
	}
	if first.Source != "tutti-agent-cli" {
		t.Fatalf("source = %q, want tutti-agent-cli", first.Source)
	}
	if len(first.Models) != 1 || first.Models[0].ID != "gpt-5.4" {
		t.Fatalf("models = %#v, want gpt-5.4", first.Models)
	}
	if second.Models[0].ID != first.Models[0].ID {
		t.Fatalf("cached model mismatch: first=%#v second=%#v", first, second)
	}
}
