package agent

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestCodexCLIModelListerDelegatesModelListToRuntimeCatalog(t *testing.T) {
	reader := &recordingAppServerCatalogReader{result: AppServerCatalogResult{
		Models: []AgentModelOption{{ID: "gpt-5", DisplayName: "GPT-5"}},
	}}
	lister := CodexCLIModelLister{
		Provider: "codex", Cwd: "/workspace", ClientName: "tuttid", Catalog: reader,
	}
	result, err := lister.ListModels(t.Context())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(result.Models) != 1 || result.Models[0].ID != "gpt-5" {
		t.Fatalf("models = %#v, want runtime catalog result", result.Models)
	}
	if reader.lastRequest != (AppServerCatalogRequest{
		Provider: "codex", Cwd: "/workspace", ClientName: "tuttid", RequestSet: "model",
	}) {
		t.Fatalf("runtime catalog request = %#v", reader.lastRequest)
	}
}

func TestCachedAgentModelCatalogScopesInjectedCodexListerToPreparation(t *testing.T) {
	reader := &recordingAppServerCatalogReader{result: AppServerCatalogResult{
		Models: []AgentModelOption{{ID: "gpt-5"}},
	}}
	catalog := &CachedAgentModelCatalog{
		Codex: CodexCLIModelLister{Provider: "codex", Catalog: reader},
	}
	preparation := &runtimeprep.PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "catalog-model-1", Provider: "codex", Cwd: "/workspace",
	}
	if _, err := catalog.ListModels(t.Context(), AgentModelCatalogInput{
		Provider: "codex", Cwd: "/workspace", Preparation: preparation,
	}); err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if reader.lastRequest.Preparation != preparation {
		t.Fatalf("request preparation = %#v, want exact request-scoped preparation", reader.lastRequest.Preparation)
	}
}

func TestCachedAgentModelCatalogDoesNotCrossCacheDifferentPreparations(t *testing.T) {
	reader := &preparationAwareAppServerCatalogReader{}
	catalog := &CachedAgentModelCatalog{
		Codex: CodexCLIModelLister{Provider: "codex", Catalog: reader},
	}
	preparationA := &runtimeprep.PrepareInput{
		WorkspaceID: "workspace-a", AgentSessionID: "session-a", Provider: "codex", Cwd: "/workspace",
	}
	preparationB := &runtimeprep.PrepareInput{
		WorkspaceID: "workspace-b", AgentSessionID: "session-b", Provider: "codex", Cwd: "/workspace",
	}
	for _, preparation := range []*runtimeprep.PrepareInput{preparationA, preparationB, preparationA, preparationB} {
		result, err := catalog.ListModels(t.Context(), AgentModelCatalogInput{
			Provider: "codex", Cwd: "/workspace", Preparation: preparation,
		})
		if err != nil {
			t.Fatalf("ListModels(%s) error = %v", preparation.WorkspaceID, err)
		}
		if len(result.Models) == 0 || result.Models[0].ID != preparation.WorkspaceID {
			t.Fatalf("models(%s) = %#v, want preparation-specific result", preparation.WorkspaceID, result.Models)
		}
	}
	if reader.calls != 2 {
		t.Fatalf("runtime catalog calls = %d, want one fetch per exact preparation", reader.calls)
	}
}

type recordingAppServerCatalogReader struct {
	result      AppServerCatalogResult
	lastRequest AppServerCatalogRequest
}

type preparationAwareAppServerCatalogReader struct {
	calls int
}

func (r *preparationAwareAppServerCatalogReader) ListAppServerCatalog(
	_ context.Context,
	input AppServerCatalogRequest,
) (AppServerCatalogResult, error) {
	r.calls++
	if input.Preparation == nil {
		return AppServerCatalogResult{}, errors.New("preparation is required")
	}
	return AppServerCatalogResult{
		Models: []AgentModelOption{{ID: input.Preparation.WorkspaceID}},
	}, nil
}

func (r *recordingAppServerCatalogReader) ListAppServerCatalog(_ context.Context, input AppServerCatalogRequest) (AppServerCatalogResult, error) {
	r.lastRequest = input
	return r.result, nil
}

func TestCachedAgentModelCatalogCachesCodexModels(t *testing.T) {
	now := time.UnixMilli(1000)
	lister := &fakeAgentModelLister{
		models: []AgentModelOption{{ID: "gpt-5", DisplayName: "GPT-5"}},
	}
	catalog := &CachedAgentModelCatalog{
		Codex: lister,
		Now: func() time.Time {
			return now
		},
	}

	first, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("first ListModels returned error: %v", err)
	}
	second, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("second ListModels returned error: %v", err)
	}
	if lister.calls != 1 {
		t.Fatalf("lister calls = %d, want one cached fetch", lister.calls)
	}
	if first.Models[0].ID != second.Models[0].ID {
		t.Fatalf("cached result mismatch: first=%#v second=%#v", first, second)
	}
}

func TestCachedAgentModelCatalogSharesConcurrentColdFetch(t *testing.T) {
	lister := &blockingAgentModelLister{
		started: make(chan struct{}),
		release: make(chan struct{}),
		models:  []AgentModelOption{{ID: "gpt-5", DisplayName: "GPT-5"}},
	}
	catalog := &CachedAgentModelCatalog{Codex: lister}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
			results <- err
		}()
	}
	select {
	case <-lister.started:
	case <-time.After(time.Second):
		t.Fatal("model lister did not start")
	}
	close(lister.release)
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("ListModels returned error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("shared model catalog fetch did not settle")
		}
	}
	if lister.calls() != 1 {
		t.Fatalf("lister calls = %d, want one shared fetch", lister.calls())
	}
}

func TestCachedAgentModelCatalogRefreshesStaleModelsInBackground(t *testing.T) {
	lister := &sequencedAgentModelLister{
		models: []AgentModelListResult{
			{Models: []AgentModelOption{{ID: "old-model", DisplayName: "Old"}}},
			{Models: []AgentModelOption{{ID: "new-model", DisplayName: "New"}}},
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
		t.Fatalf("initial ListModels returned error: %v", err)
	}
	catalog.Invalidate("codex")
	stale, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("stale ListModels returned error: %v", err)
	}
	if !stale.Stale || stale.Models[0].ID != first.Models[0].ID {
		t.Fatalf("stale result = %#v, want old model marked stale", stale)
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
		t.Fatal("background refresh did not publish completion")
	}
	fresh, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("fresh ListModels returned error: %v", err)
	}
	if fresh.Stale || len(fresh.Models) == 0 || fresh.Models[0].ID != "new-model" {
		t.Fatalf("fresh result = %#v, want new model", fresh)
	}
}

func TestCachedAgentModelCatalogLoadsPersistentCatalogBeforeBackgroundRefresh(t *testing.T) {
	persistentPath := filepath.Join(t.TempDir(), "model-catalog.json")
	first := &CachedAgentModelCatalog{
		Codex: &fakeAgentModelLister{
			models: []AgentModelOption{{ID: "old-model", DisplayName: "Old"}},
		},
		PersistentPath: persistentPath,
		AuthFingerprint: func(string) string {
			return "account-a"
		},
	}
	if _, err := first.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"}); err != nil {
		t.Fatalf("initial ListModels returned error: %v", err)
	}

	refreshLister := &blockingAgentModelLister{
		started: make(chan struct{}),
		release: make(chan struct{}),
		models:  []AgentModelOption{{ID: "new-model", DisplayName: "New"}},
	}
	refreshed := make(chan string, 1)
	second := &CachedAgentModelCatalog{
		Codex:          refreshLister,
		PersistentPath: persistentPath,
		AuthFingerprint: func(string) string {
			return "account-a"
		},
		OnRefresh: func(provider string) {
			refreshed <- provider
		},
	}
	result, err := second.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("persistent ListModels returned error: %v", err)
	}
	if !result.Stale || len(result.Models) == 0 || result.Models[0].ID != "old-model" {
		t.Fatalf("persistent result = %#v, want stale old model", result)
	}
	select {
	case <-refreshLister.started:
	case <-time.After(time.Second):
		t.Fatal("persistent catalog did not start background refresh")
	}
	close(refreshLister.release)
	select {
	case provider := <-refreshed:
		if provider != "codex" {
			t.Fatalf("refresh provider = %q, want codex", provider)
		}
	case <-time.After(time.Second):
		t.Fatal("persistent catalog refresh did not settle")
	}
}

func TestCachedAgentModelCatalogWaitForFreshBlocksUntilRefreshSettles(t *testing.T) {
	lister := &sequencedAgentModelLister{
		models: []AgentModelListResult{
			{Models: []AgentModelOption{{ID: "old-model"}}},
			{Models: []AgentModelOption{{ID: "new-model"}}},
		},
		secondStarted: make(chan struct{}),
		secondRelease: make(chan struct{}),
	}
	catalog := &CachedAgentModelCatalog{Codex: lister}
	if _, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"}); err != nil {
		t.Fatalf("initial ListModels returned error: %v", err)
	}
	catalog.Invalidate("codex")
	resultCh := make(chan struct {
		result AgentModelCatalogResult
		err    error
	}, 1)
	go func() {
		result, err := catalog.ListModels(context.Background(), AgentModelCatalogInput{
			Provider:     "codex",
			WaitForFresh: true,
		})
		resultCh <- struct {
			result AgentModelCatalogResult
			err    error
		}{result: result, err: err}
	}()
	select {
	case <-lister.secondStarted:
	case <-time.After(time.Second):
		t.Fatal("fresh catalog refresh did not start")
	}
	select {
	case result := <-resultCh:
		t.Fatalf("fresh request returned before refresh settled: %#v", result)
	default:
	}
	close(lister.secondRelease)
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("fresh ListModels returned error: %v", result.err)
		}
		if result.result.Stale || result.result.Models[0].ID != "new-model" {
			t.Fatalf("fresh result = %#v, want new model", result.result)
		}
	case <-time.After(time.Second):
		t.Fatal("fresh request did not settle")
	}
}

func TestCachedAgentModelCatalogRejectsPersistentCatalogFromAnotherAuthGeneration(t *testing.T) {
	persistentPath := filepath.Join(t.TempDir(), "model-catalog.json")
	first := &CachedAgentModelCatalog{
		Codex:          &fakeAgentModelLister{models: []AgentModelOption{{ID: "old-model"}}},
		PersistentPath: persistentPath,
		AuthFingerprint: func(string) string {
			return "account-a"
		},
	}
	if _, err := first.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"}); err != nil {
		t.Fatalf("initial ListModels returned error: %v", err)
	}
	secondLister := &fakeAgentModelLister{models: []AgentModelOption{{ID: "new-model"}}}
	second := &CachedAgentModelCatalog{
		Codex:          secondLister,
		PersistentPath: persistentPath,
		AuthFingerprint: func(string) string {
			return "account-b"
		},
	}
	result, err := second.ListModels(context.Background(), AgentModelCatalogInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("auth-generation ListModels returned error: %v", err)
	}
	if result.Stale || len(result.Models) == 0 || result.Models[0].ID != "new-model" {
		t.Fatalf("auth-generation result = %#v, want fresh new model", result)
	}
	if secondLister.calls != 1 {
		t.Fatalf("new-account lister calls = %d, want one", secondLister.calls)
	}
}

type fakeAgentModelLister struct {
	calls    int
	models   []AgentModelOption
	fallback bool
	err      error
}

func (f *fakeAgentModelLister) ListModels(context.Context) (AgentModelListResult, error) {
	f.calls += 1
	return AgentModelListResult{Models: f.models, IsFallback: f.fallback}, f.err
}

type blockingAgentModelLister struct {
	started chan struct{}
	release chan struct{}
	models  []AgentModelOption
	mu      sync.Mutex
	count   int
	once    sync.Once
}

func (l *blockingAgentModelLister) ListModels(ctx context.Context) (AgentModelListResult, error) {
	l.mu.Lock()
	l.count++
	l.mu.Unlock()
	l.once.Do(func() { close(l.started) })
	select {
	case <-l.release:
		return AgentModelListResult{Models: l.models}, nil
	case <-ctx.Done():
		return AgentModelListResult{}, ctx.Err()
	}
}

func (l *blockingAgentModelLister) calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

type sequencedAgentModelLister struct {
	models        []AgentModelListResult
	secondStarted chan struct{}
	secondRelease chan struct{}
	mu            sync.Mutex
	count         int
}

func (l *sequencedAgentModelLister) ListModels(ctx context.Context) (AgentModelListResult, error) {
	l.mu.Lock()
	index := l.count
	l.count++
	l.mu.Unlock()
	if index == 1 {
		close(l.secondStarted)
		select {
		case <-l.secondRelease:
		case <-ctx.Done():
			return AgentModelListResult{}, ctx.Err()
		}
	}
	if index >= len(l.models) {
		index = len(l.models) - 1
	}
	return l.models[index], nil
}
