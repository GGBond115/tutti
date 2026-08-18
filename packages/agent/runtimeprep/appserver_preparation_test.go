package runtimeprep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type passthroughCodexPreparer struct{}

func (passthroughCodexPreparer) Provider() string { return "codex" }

func (passthroughCodexPreparer) Prepare(_ context.Context, input ProviderPrepareInput) (ProviderPrepareResult, error) {
	home := filepath.Join(input.RuntimeRoot, codexHomeDirectory)
	if input.ProviderStateRoot != "" {
		home = filepath.Join(input.ProviderStateRoot, codexHomeDirectory)
	}
	return ProviderPrepareResult{Cwd: input.Cwd, Env: []string{"CODEX_HOME=" + home}}, nil
}

type latePolicyFailureCodexPreparer struct{}

func (latePolicyFailureCodexPreparer) Provider() string { return "codex" }

func (latePolicyFailureCodexPreparer) Prepare(_ context.Context, input ProviderPrepareInput) (ProviderPrepareResult, error) {
	if !input.appServerProcessProfile && input.commandCapabilities != nil {
		input.commandCapabilities.byID["references.task.list"] = CommandCapability{
			ID: "references.task.list", Path: []string{"reference", "list"},
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}
	}
	home := filepath.Join(input.RuntimeRoot, codexHomeDirectory)
	if input.ProviderStateRoot != "" {
		home = filepath.Join(input.ProviderStateRoot, codexHomeDirectory)
	}
	return ProviderPrepareResult{Cwd: input.Cwd, Env: []string{"CODEX_HOME=" + home}}, nil
}

type failOnceProfileCleanupStore struct {
	RuntimeStore
	providerStateStore ProviderStateStore
	remainingFailures  int
	profileAttempts    int
}

func (s *failOnceProfileCleanupStore) ProviderStateRoot(id string) (string, error) {
	return s.providerStateStore.ProviderStateRoot(id)
}

func (s *failOnceProfileCleanupStore) EnsureProviderStateRoot(root string) error {
	return s.providerStateStore.EnsureProviderStateRoot(root)
}

func (s *failOnceProfileCleanupStore) CleanupRuntime(input StoreCleanupInput) error {
	if strings.HasPrefix(input.AgentSessionID, appServerProfileSessionPrefix) {
		s.profileAttempts++
		if s.remainingFailures > 0 {
			s.remainingFailures--
			return errors.New("injected profile cleanup failure")
		}
	}
	return s.RuntimeStore.CleanupRuntime(input)
}

func TestAppServerPreparationSharesProcessProfileAndIsolatesThreadOverlay(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	firstCwd := filepath.Join(t.TempDir(), "first")
	secondCwd := filepath.Join(t.TempDir(), "second")
	for _, cwd := range []string{firstCwd, secondCwd} {
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	preparer := newTestPreparer(stateDir)
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "device-1", RuntimeGeneration: "runtime-1", TransportScopeID: "transport-1",
	}
	prepare := func(sessionID, cwd, apiKey, model, instructions, mcpURL string) PreparedRuntime {
		t.Helper()
		prepared, err := preparer.Prepare(t.Context(), PrepareInput{
			WorkspaceID: "workspace-1", AgentSessionID: sessionID, AgentTargetID: "target-1",
			Provider: "codex", Cwd: cwd, CLICommand: "tutti", AgentInstructions: instructions,
			MCPServers: []MCPServerBinding{{Name: "connector", Type: "http", URL: mcpURL, Headers: map[string]string{"Authorization": "Bearer " + sessionID}}},
			ModelEndpoint: &ModelEndpointConfig{
				PlanID: "plan-1", Protocol: "openai", BaseURL: "http://127.0.0.1/model/v1",
				APIKey: apiKey, WireAPI: "responses", Model: model,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if prepared.AppServer == nil {
			t.Fatal("Prepare() AppServer = nil")
		}
		return prepared
	}

	first := prepare("session-1", firstCwd, "credential-one", "model-1", "first instructions", "http://127.0.0.1/first")
	second := prepare("session-2", secondCwd, "credential-two", "model-2", "second instructions", "http://127.0.0.1/second")
	firstHome := appServerEnvironmentValue(first.AppServer.ProcessEnv, "CODEX_HOME")
	secondHome := appServerEnvironmentValue(second.AppServer.ProcessEnv, "CODEX_HOME")
	if firstHome == "" || firstHome != secondHome {
		t.Fatalf("process CODEX_HOME = %q and %q, want one shared profile", firstHome, secondHome)
	}
	if first.AppServer.ProcessProfileDigest != second.AppServer.ProcessProfileDigest {
		t.Fatalf("process profile digests differ: %q and %q", first.AppServer.ProcessProfileDigest, second.AppServer.ProcessProfileDigest)
	}
	for _, prepared := range []PreparedRuntime{first, second} {
		processEnv := strings.Join(prepared.AppServer.ProcessEnv, "\n")
		if strings.Contains(processEnv, "credential-") || strings.Contains(processEnv, "TUTTI_AGENT_SESSION_ID") || strings.Contains(processEnv, "TUTTI_AGENT_CWD") {
			t.Fatalf("Session value leaked into process env: %s", processEnv)
		}
		if appServerEnvironmentValue(prepared.AppServer.ProcessEnv, "PATH") == "" {
			t.Fatalf("shared process profile lost PATH: %#v", prepared.AppServer.ProcessEnv)
		}
		if appServerEnvironmentValue(prepared.AppServer.ThreadEnv, "PATH") != "" {
			t.Fatalf("machine-specific PATH leaked into thread overlay: %#v", prepared.AppServer.ThreadEnv)
		}
	}
	if len(first.AppServer.ModelProviderCredentials) != 1 ||
		first.AppServer.ModelProviderCredentials[0].ModelProviderID != ModelPlanProviderID ||
		first.AppServer.ModelProviderCredentials[0].BearerToken != "credential-one" ||
		len(second.AppServer.ModelProviderCredentials) != 1 ||
		second.AppServer.ModelProviderCredentials[0].BearerToken != "credential-two" {
		t.Fatalf("Thread credentials = %#v and %#v", first.AppServer.ModelProviderCredentials, second.AppServer.ModelProviderCredentials)
	}
	if appServerEnvironmentValue(first.AppServer.ThreadEnv, ModelPlanAPIKeyEnv) != "" ||
		appServerEnvironmentValue(second.AppServer.ThreadEnv, ModelPlanAPIKeyEnv) != "" {
		t.Fatalf("model credential leaked into shell env = %#v and %#v", first.AppServer.ThreadEnv, second.AppServer.ThreadEnv)
	}
	if appServerEnvironmentValue(first.AppServer.ThreadEnv, "TUTTI_AGENT_SESSION_ID") != "session-1" ||
		appServerEnvironmentValue(second.AppServer.ThreadEnv, "TUTTI_AGENT_SESSION_ID") != "session-2" {
		t.Fatalf("Thread Session identities = %#v and %#v", first.AppServer.ThreadEnv, second.AppServer.ThreadEnv)
	}
	if first.AppServer.DeveloperInstructions == second.AppServer.DeveloperInstructions ||
		!strings.Contains(first.AppServer.DeveloperInstructions, "first instructions") ||
		!strings.Contains(second.AppServer.DeveloperInstructions, "second instructions") {
		t.Fatalf("Thread instructions were not isolated:\nfirst=%s\nsecond=%s", first.AppServer.DeveloperInstructions, second.AppServer.DeveloperInstructions)
	}
	if _, err := os.Stat(filepath.Join(firstHome, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("shared process profile AGENTS.md exists, err=%v", err)
	}
	profileConfig, err := os.ReadFile(filepath.Join(firstHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(profileConfig), "127.0.0.1/first") || strings.Contains(string(profileConfig), "127.0.0.1/second") {
		t.Fatalf("Thread MCP leaked into shared config: %s", profileConfig)
	}
	if strings.Contains(string(profileConfig), "env_key") || strings.Contains(string(profileConfig), "credential-") {
		t.Fatalf("shared model provider config contains process credential lookup: %s", profileConfig)
	}
	if strings.Contains(string(profileConfig), `model = "model-1"`) || strings.Contains(string(profileConfig), `model = "model-2"`) {
		t.Fatalf("Session model leaked into shared process config: %s", profileConfig)
	}

	firstLease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-2", Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	firstOverlayRoot, _ := preparer.runtimeStore().RuntimeRoot("workspace-1", "session-1")
	secondOverlayRoot, _ := preparer.runtimeStore().RuntimeRoot("workspace-1", "session-2")
	if err := firstLease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstOverlayRoot); !os.IsNotExist(err) {
		t.Fatalf("first Thread root still exists, err=%v", err)
	}
	if _, err := os.Stat(secondOverlayRoot); err != nil {
		t.Fatalf("first Thread cleanup removed second overlay: %v", err)
	}
	if err := secondLease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstHome); err != nil {
		t.Fatalf("reused process lease cleanup removed live profile: %v", err)
	}
	if err := secondLease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := firstLease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstHome); err != nil {
		t.Fatalf("last process cleanup removed durable provider state, err=%v", err)
	}
	profileRoots, err := filepath.Glob(filepath.Join(stateDir, "agent", "runs", appServerProfileSessionPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profileRoots) != 0 {
		t.Fatalf("last process cleanup left process profile roots: %#v", profileRoots)
	}
	if err := firstLease.ProcessCleanup(t.Context()); err != nil {
		t.Fatalf("repeat process cleanup: %v", err)
	}
}

func TestAppServerPreparationSupportsTuttiAgentProvider(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	preparer := newTestPreparer(stateDir)
	preparer.RegisterProvider(TuttiAgentPreparer{})
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "device-1", RuntimeGeneration: "runtime-1", TransportScopeID: "transport-1",
	}
	prepare := func(sessionID, instructions, mcpURL string) PreparedRuntime {
		t.Helper()
		prepared, err := preparer.Prepare(t.Context(), PrepareInput{
			WorkspaceID: "workspace-1", AgentSessionID: sessionID, AgentTargetID: "target-1",
			Provider: "tutti-agent", Cwd: t.TempDir(), AgentInstructions: instructions,
			MCPServers: []MCPServerBinding{{Name: "connector", URL: mcpURL}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	first := prepare("session-1", "first session instructions", "http://127.0.0.1/first")
	second := prepare("session-2", "second session instructions", "http://127.0.0.1/second")
	if first.AppServer == nil || second.AppServer == nil {
		t.Fatalf("Prepare() AppServer = %#v and %#v", first.AppServer, second.AppServer)
	}
	processHome := appServerEnvironmentValue(first.AppServer.ProcessEnv, "TUTTI_AGENT_HOME")
	secondHome := appServerEnvironmentValue(second.AppServer.ProcessEnv, "TUTTI_AGENT_HOME")
	if processHome == "" || processHome != secondHome ||
		first.AppServer.ProcessProfileDigest != second.AppServer.ProcessProfileDigest ||
		appServerEnvironmentValue(first.Env, "TUTTI_AGENT_HOME") != "" ||
		appServerEnvironmentValue(second.Env, "TUTTI_AGENT_HOME") != "" {
		t.Fatalf("process homes=%q/%q preparations=%#v/%#v", processHome, secondHome, first.AppServer, second.AppServer)
	}
	processEnv := strings.Join(first.AppServer.ProcessEnv, "\n")
	if strings.Contains(processEnv, "session-1") || strings.Contains(processEnv, "session-2") ||
		!strings.Contains(first.AppServer.DeveloperInstructions, "first session instructions") ||
		strings.Contains(first.AppServer.DeveloperInstructions, "second session instructions") ||
		!strings.Contains(second.AppServer.DeveloperInstructions, "second session instructions") ||
		strings.Contains(second.AppServer.DeveloperInstructions, "first session instructions") ||
		len(first.MCPServers) != 1 || first.MCPServers[0].URL != "http://127.0.0.1/first" ||
		len(second.MCPServers) != 1 || second.MCPServers[0].URL != "http://127.0.0.1/second" {
		t.Fatalf("prepared runtimes = %#v and %#v", first, second)
	}
	firstLease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", Provider: "tutti-agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-2", Provider: "tutti-agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstLease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(processHome); err != nil {
		t.Fatalf("Thread cleanup removed process home: %v", err)
	}
	if err := secondLease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(processHome); err != nil {
		t.Fatalf("reused process lease cleanup removed process home: %v", err)
	}
	if err := secondLease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := firstLease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(processHome); err != nil {
		t.Fatalf("process cleanup removed durable Tutti Agent home, err=%v", err)
	}
}

func TestAppServerProcessCleanupRetriesAfterStoreFailure(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	store := &failOnceProfileCleanupStore{
		RuntimeStore: LocalStore{StateDir: stateDir}, providerStateStore: LocalStore{StateDir: stateDir}, remainingFailures: 1,
	}
	preparer := newTestPreparer(stateDir)
	preparer.Store = store
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "device-1", RuntimeGeneration: "runtime-1", TransportScopeID: "transport-1",
	}
	prepared, err := preparer.Prepare(t.Context(), PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", AgentTargetID: "target-1",
		Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti",
	})
	if err != nil {
		t.Fatal(err)
	}
	profileHome := appServerEnvironmentValue(prepared.AppServer.ProcessEnv, "CODEX_HOME")
	lease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lease.ProcessCleanup(t.Context()); err == nil {
		t.Fatal("first ProcessCleanup() error = nil, want injected store failure")
	}
	if _, err := os.Stat(profileHome); err != nil {
		t.Fatalf("failed cleanup removed retryable profile root: %v", err)
	}
	prepareInput := PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-2", AgentTargetID: "target-1",
		Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti",
	}
	replacement, err := preparer.Prepare(t.Context(), prepareInput)
	if err != nil {
		t.Fatalf("same-key Prepare did not recover retiring profile: %v", err)
	}
	replacementHome := appServerEnvironmentValue(replacement.AppServer.ProcessEnv, "CODEX_HOME")
	if replacementHome != profileHome {
		t.Fatalf("replacement profile root = %q, want deterministic root %q", replacementHome, profileHome)
	}
	if err := lease.ProcessCleanup(t.Context()); err != nil {
		t.Fatalf("old lease retry after replacement: %v", err)
	}
	if _, err := os.Stat(replacementHome); err != nil {
		t.Fatalf("old cleanup retry removed replacement profile root: %v", err)
	}
	if store.profileAttempts != 2 {
		t.Fatalf("profile cleanup attempts = %d, want 2", store.profileAttempts)
	}
	replacementLease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-2", Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := replacementLease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := replacementLease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAppServerPrepareRecoversProfileCleanupFailureBeforeSessionLeaseExists(t *testing.T) {
	stateDir := t.TempDir()
	setTestHome(t, t.TempDir())
	store := &failOnceProfileCleanupStore{
		RuntimeStore: LocalStore{StateDir: stateDir}, providerStateStore: LocalStore{StateDir: stateDir}, remainingFailures: 1,
	}
	preparer := newTestPreparer(stateDir)
	preparer.Store = store
	preparer.RegisterProvider(latePolicyFailureCodexPreparer{})
	preparer.AppServerScope = AppServerProfileScope{
		ExecutionHostID: "device-1", RuntimeGeneration: "runtime-1", TransportScopeID: "transport-1",
	}
	preparer.CommandCatalog = staticCommandCatalog{}
	input := PrepareInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-policy-fail", AgentTargetID: "target-1",
		Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti",
	}
	if _, err := preparer.Prepare(t.Context(), input); err == nil || !strings.Contains(err.Error(), "does not accept --source") {
		t.Fatalf("malformed policy Prepare() error = %v, want rejected policy command input", err)
	}
	if store.profileAttempts != 1 {
		t.Fatalf("failed policy profile cleanup attempts = %d, want 1", store.profileAttempts)
	}
	preparer.RegisterProvider(passthroughCodexPreparer{})
	input.AgentSessionID = "session-recovered"
	prepared, err := preparer.Prepare(t.Context(), input)
	if err != nil {
		t.Fatalf("valid same-key Prepare() did not recover orphaned retirement: %v", err)
	}
	if prepared.AppServer == nil {
		t.Fatal("recovered Prepare() AppServer = nil")
	}
	if store.profileAttempts != 2 {
		t.Fatalf("recovery cleanup attempts = %d, want failed cleanup plus acquire retry", store.profileAttempts)
	}
	lease, err := preparer.AcquireAppServerLaunchLease(t.Context(), AppServerLaunchLeaseInput{
		WorkspaceID: "workspace-1", AgentSessionID: input.AgentSessionID, Provider: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.ThreadCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lease.ProcessCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
}
