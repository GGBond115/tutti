package agentruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

const catalogSubprocessHelperEnv = "TUTTI_CODEX_CATALOG_SUBPROCESS_HELPER"

// TestCatalogSubprocessHelper is the real stdio boundary used by the
// vertical catalog/Session reuse test below. Keeping the helper in the test
// binary exercises LocalProcessTransport, JSON-RPC framing, and the runtime
// registry together; fake in-memory connections cannot prove that boundary.
func TestCatalogSubprocessHelper(_ *testing.T) {
	if os.Getenv(catalogSubprocessHelperEnv) != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	thread := 0
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || len(request.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case appServerMethodInitialize:
			result = map[string]any{"serverInfo": map[string]any{"name": "catalog-helper", "version": "1"}}
		case appServerMethodModelList:
			result = map[string]any{"data": []any{map[string]any{"id": "helper-model", "displayName": "Helper Model"}}}
		case appServerMethodSkillsList, appServerMethodAppList, appServerMethodPluginList,
			appServerMethodMCPServerStatusList, appServerMethodCollaborationModeList:
			result = map[string]any{"data": []any{}}
		case appServerMethodThreadStart:
			thread++
			result = map[string]any{"thread": map[string]any{"id": "helper-thread-" + strconv.Itoa(thread)}}
		}
		if err := encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": result}); err != nil {
			return
		}
	}
}

type countingCatalogSubprocessTransport struct {
	mu     sync.Mutex
	starts int
}

func (t *countingCatalogSubprocessTransport) Start(ctx context.Context, spec ProcessSpec) (ProcessConnection, error) {
	connection, err := NewLocalProcessTransport().Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.starts++
	t.mu.Unlock()
	return connection, nil
}

func (t *countingCatalogSubprocessTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.starts
}

func catalogSubprocessAdapter(t *testing.T, transport ProcessTransport, provider string) *CodexAppServerAdapter {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(context.Context, string) (ProviderCommand, error) {
		return ProviderCommand{
			Command: []string{executable, "-test.run=^TestCatalogSubprocessHelper$"},
			Env:     []string{catalogSubprocessHelperEnv + "=1"},
		}, nil
	}
	if provider == ProviderTuttiAgent {
		descriptor, ok := providerregistry.Find(ProviderTuttiAgent)
		if !ok {
			t.Fatal("tutti-agent provider descriptor is missing")
		}
		adapter, ok := newAdapterFromProviderDescriptor(
			descriptor,
			transport,
			LegacyHostMetadata(),
			resolver,
			providerAdapterOptions{},
		).(*CodexAppServerAdapter)
		if !ok {
			t.Fatalf("Tutti Agent descriptor constructed %T", adapter)
		}
		return adapter
	}
	return NewCodexAppServerAdapterWithHostMetadataAndCommandResolver(transport, LegacyHostMetadata(), resolver)
}

func catalogSubprocessSession(t *testing.T, provider, id, cwd string) Session {
	t.Helper()
	session := testAppServerSession()
	session.Provider = provider
	session.AgentSessionID = id
	session.RoomID = "catalog-room"
	session.CWD = cwd
	session.AppServer = testAppServerRuntimePreparation(cwd)
	return session
}

func TestAppServerCatalogAndSessionsShareOneRealSubprocess(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	transport := &countingCatalogSubprocessTransport{}
	adapter := catalogSubprocessAdapter(t, transport, ProviderCodex)
	first := catalogSubprocessSession(t, ProviderCodex, "session-a", cwd)

	if _, err := adapter.ListAppServerCatalog(t.Context(), AppServerCatalogRequest{
		Session: first, RequestSet: "model", CWD: cwd,
	}); err != nil {
		t.Fatalf("Composer-first catalog: %v", err)
	}
	capabilityResult, err := adapter.ListAppServerCatalog(t.Context(), AppServerCatalogRequest{
		Session: first, RequestSet: "codex", CWD: cwd,
	})
	if err != nil {
		t.Fatalf("Composer-first capability catalog: %v", err)
	}
	for _, method := range []string{
		appServerMethodSkillsList,
		appServerMethodAppList,
		appServerMethodPluginList,
		appServerMethodMCPServerStatusList,
	} {
		if _, ok := capabilityResult.CapabilityResponse[method]; !ok {
			t.Fatalf("Composer-first capability response missing %q: %#v", method, capabilityResult.CapabilityResponse)
		}
	}
	if _, err := adapter.Start(t.Context(), first); err != nil {
		t.Fatalf("first Session after Composer: %v", err)
	}
	second := catalogSubprocessSession(t, ProviderCodex, "session-b", cwd)
	if _, err := adapter.Start(t.Context(), second); err != nil {
		t.Fatalf("second Session after Composer: %v", err)
	}
	if got := transport.count(); got != 1 {
		t.Fatalf("Composer-first physical process count = %d, want 1", got)
	}
	if err := adapter.Close(t.Context(), first); err != nil {
		t.Fatalf("close first Session: %v", err)
	}
	if err := adapter.Close(t.Context(), second); err != nil {
		t.Fatalf("close second Session: %v", err)
	}
	cleanup := adapter.CleanupLiveSessionResources(t.Context(), 1)
	if cleanup.Attempted != 1 || cleanup.Cleaned != 1 {
		t.Fatalf("Composer-first cleanup = %#v, want one clean close", cleanup)
	}

	transport = &countingCatalogSubprocessTransport{}
	adapter = catalogSubprocessAdapter(t, transport, ProviderCodex)
	first = catalogSubprocessSession(t, ProviderCodex, "session-first", cwd)
	if _, err := adapter.Start(t.Context(), first); err != nil {
		t.Fatalf("Session-first Start: %v", err)
	}
	if _, err := adapter.ListAppServerCatalog(t.Context(), AppServerCatalogRequest{
		Session: first, RequestSet: "model", CWD: cwd,
	}); err != nil {
		t.Fatalf("Session-first Composer catalog: %v", err)
	}
	if got := transport.count(); got != 1 {
		t.Fatalf("Session-first physical process count = %d, want 1", got)
	}
	if err := adapter.Close(t.Context(), first); err != nil {
		t.Fatalf("close Session-first Session: %v", err)
	}
	if cleanup := adapter.CleanupLiveSessionResources(t.Context(), 1); cleanup.Cleaned != 1 {
		t.Fatalf("Session-first cleanup = %#v, want one clean close", cleanup)
	}
}

func TestAppServerCatalogProfileAndProviderIsolationUseSeparateSubprocesses(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	transport := &countingCatalogSubprocessTransport{}
	codex := catalogSubprocessAdapter(t, transport, ProviderCodex)
	first := catalogSubprocessSession(t, ProviderCodex, "codex", cwd)
	if _, err := codex.ListAppServerCatalog(t.Context(), AppServerCatalogRequest{Session: first, RequestSet: "model", CWD: cwd}); err != nil {
		t.Fatalf("Codex catalog: %v", err)
	}
	second := catalogSubprocessSession(t, ProviderCodex, "codex-profile", cwd)
	second.AppServer.RuntimeGeneration = "different-runtime"
	if _, err := codex.ListAppServerCatalog(t.Context(), AppServerCatalogRequest{Session: second, RequestSet: "model", CWD: cwd}); err != nil {
		t.Fatalf("different-profile catalog: %v", err)
	}
	tutti := catalogSubprocessAdapter(t, transport, ProviderTuttiAgent)
	third := catalogSubprocessSession(t, ProviderTuttiAgent, "tutti", cwd)
	if _, err := tutti.ListAppServerCatalog(t.Context(), AppServerCatalogRequest{Session: third, RequestSet: "model", CWD: cwd}); err != nil {
		t.Fatalf("Tutti catalog: %v", err)
	}
	if got := transport.count(); got != 3 {
		t.Fatalf("profile/provider isolation process count = %d, want 3", got)
	}
	_ = codex.ShutdownLiveSessionResources(t.Context())
	_ = tutti.ShutdownLiveSessionResources(t.Context())
}
