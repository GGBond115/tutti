package main

import (
	"context"
	"strings"
	"testing"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	agentprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

type appServerWiringLeaseProvider struct {
	processCleanup func(context.Context) error
	threadCleanup  func(context.Context) error
}

type appServerWiringCommandCatalog struct{}

func (appServerWiringCommandCatalog) Capabilities(
	context.Context,
	agentprep.CommandContext,
) []agentprep.CommandCapability {
	return nil
}

func (p appServerWiringLeaseProvider) AcquireAppServerLaunchLease(
	context.Context, agentprep.AppServerLaunchLeaseInput,
) (agentprep.AppServerLaunchLease, error) {
	return agentprep.AppServerLaunchLease{ProcessCleanup: p.processCleanup, ThreadCleanup: p.threadCleanup}, nil
}

func TestAgentAppServerProviderLaunchPreparerSeparatesProcessAndThreadInputs(t *testing.T) {
	processCleanup := func(context.Context) error { return nil }
	threadCleanup := func(context.Context) error { return nil }
	preparer := newAgentAppServerProviderLaunchPreparer(appServerWiringLeaseProvider{
		processCleanup: processCleanup, threadCleanup: threadCleanup,
	})
	result, err := preparer(t.Context(), agentruntime.ProviderLaunchPrepareInput{
		Provider: "codex", Command: []string{"codex", "app-server"}, CWD: "/session/cwd",
		Env: []string{
			"TUTTI_AGENT_ROUTING=1", "TUTTI_WORKSPACE_ID=workspace-1", "TUTTI_AGENT_SESSION_ID=session-1",
			agenthost.AgentCWDEnvironmentVariable + "=/session/cwd",
			agenthost.AgentRailPlacementEnvironmentVariable + "={\"version\":1,\"kind\":\"project\",\"projectPath\":\"/workspace\",\"sectionKey\":\"project:/workspace\"}",
			agentprep.ModelPlanAPIKeyEnv + "=secret", "RESOLVED_RUNTIME=stable",
		},
		Session: agentruntime.Session{
			RoomID: "workspace-1", AgentSessionID: "session-1", Provider: "codex", CWD: "/session/cwd",
			Env:        []string{"TUTTI_WORKSPACE_ID=workspace-1", "TUTTI_AGENT_SESSION_ID=session-1", agentprep.ModelPlanAPIKeyEnv + "=secret"},
			MCPServers: []agentruntime.MCPServerBinding{{Name: "connector", URL: "http://127.0.0.1/session", Headers: map[string]string{"Authorization": "secret"}}},
			AppServer: &agentruntime.AppServerRuntimePreparation{
				ExecutionHostID: "device-1", RuntimeGeneration: "runtime-1", TransportScopeID: "transport-1",
				ProcessProfileDigest: "profile-1", ProcessCWD: "/profile",
				ProcessEnv: []string{"CODEX_HOME=/profile/codex-home"},
				ThreadEnv:  []string{"TUTTI_AGENT_SESSION_ID=session-1"},
				ModelProviderCredentials: []agentruntime.AppServerModelProviderCredential{{
					ModelProviderID: agentprep.ModelPlanProviderID, BearerToken: "secret",
				}},
				DeveloperInstructions: "session instructions",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AppServer == nil {
		t.Fatal("ProviderLaunchPreparer AppServer = nil")
	}
	processEnv := strings.Join(result.AppServer.ProcessProfile.Env, "\n")
	if !strings.Contains(processEnv, "CODEX_HOME=/profile/codex-home") ||
		!strings.Contains(processEnv, "RESOLVED_RUNTIME=stable") || !strings.Contains(processEnv, "TUTTI_AGENT_ROUTING=1") {
		t.Fatalf("stable process env missing: %s", processEnv)
	}
	if strings.Contains(processEnv, "session-1") || strings.Contains(processEnv, "secret") || strings.Contains(processEnv, "workspace-1") ||
		strings.Contains(processEnv, agenthost.AgentCWDEnvironmentVariable) || strings.Contains(processEnv, agenthost.AgentRailPlacementEnvironmentVariable) {
		t.Fatalf("Session value leaked into process env: %s", processEnv)
	}
	overlay := result.AppServer.ThreadOverlay
	if len(overlay.MCPServers) != 1 || overlay.MCPServers[0].URL != "http://127.0.0.1/session" ||
		strings.Contains(strings.Join(overlay.Env, "\n"), "secret") ||
		len(overlay.ModelProviderCredentials) != 1 ||
		overlay.ModelProviderCredentials[0].BearerToken != "secret" ||
		overlay.DeveloperInstructions != "session instructions" {
		t.Fatalf("Thread overlay = %#v", overlay)
	}
	if result.AppServer.ProcessCleanup == nil || result.AppServer.ThreadCleanup == nil {
		t.Fatal("split cleanup leases are nil")
	}
}

func TestConfigureAgentAppServerPreparationUsesDurableHostAndFreshRuntimeIdentity(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	first := agentprep.NewDefaultPreparer(stateDir)
	first.CommandCatalog = appServerWiringCommandCatalog{}
	if preparer, err := configureAgentAppServerPreparation(first, stateDir, "transport-1"); err != nil || preparer == nil {
		t.Fatalf("first configure = (%v, %v)", preparer, err)
	}
	second := agentprep.NewDefaultPreparer(stateDir)
	if preparer, err := configureAgentAppServerPreparation(second, stateDir, "transport-2"); err != nil || preparer == nil {
		t.Fatalf("second configure = (%v, %v)", preparer, err)
	}
	if first.AppServerScope.ExecutionHostID == "" || first.AppServerScope.ExecutionHostID != second.AppServerScope.ExecutionHostID {
		t.Fatalf("execution host identities = %#v and %#v", first.AppServerScope, second.AppServerScope)
	}
	if first.AppServerScope.RuntimeGeneration == second.AppServerScope.RuntimeGeneration {
		t.Fatalf("runtime generation was reused: %#v and %#v", first.AppServerScope, second.AppServerScope)
	}
	if first.AppServerScope.TransportScopeID != "transport-1" || second.AppServerScope.TransportScopeID != "transport-2" {
		t.Fatalf("transport scopes = %#v and %#v", first.AppServerScope, second.AppServerScope)
	}
	prepare := func(sessionID, credential string) agentprep.PreparedRuntime {
		t.Helper()
		prepared, err := first.Prepare(t.Context(), agentprep.PrepareInput{
			WorkspaceID: "workspace-1", AgentSessionID: sessionID, AgentTargetID: "target-1",
			Provider: "codex", Cwd: t.TempDir(), CLICommand: "tutti",
			AgentInstructions: "instructions for " + sessionID,
			ModelEndpoint: &agentprep.ModelEndpointConfig{
				PlanID: "plan-1", Protocol: "openai", BaseURL: "http://127.0.0.1/model/v1",
				APIKey: credential, WireAPI: "responses", Model: "model-1",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	firstSession := prepare("session-1", "credential-one")
	secondSession := prepare("session-2", "credential-two")
	if firstSession.AppServer == nil || secondSession.AppServer == nil {
		t.Fatalf("configured AppServer preparations = %#v and %#v", firstSession.AppServer, secondSession.AppServer)
	}
	if firstSession.AppServer.ProcessProfileDigest != secondSession.AppServer.ProcessProfileDigest {
		t.Fatalf("configured profiles differ = %#v and %#v", firstSession.AppServer, secondSession.AppServer)
	}
	firstProcessEnv := strings.Join(firstSession.AppServer.ProcessEnv, "\n")
	if strings.Contains(firstProcessEnv, "credential-") || strings.Contains(firstProcessEnv, "session-") {
		t.Fatalf("configured shared process env leaked Session material: %s", firstProcessEnv)
	}
	if len(firstSession.AppServer.ModelProviderCredentials) != 1 ||
		firstSession.AppServer.ModelProviderCredentials[0].BearerToken != "credential-one" ||
		len(secondSession.AppServer.ModelProviderCredentials) != 1 ||
		secondSession.AppServer.ModelProviderCredentials[0].BearerToken != "credential-two" {
		t.Fatalf("configured Thread credentials = %#v and %#v", firstSession.AppServer.ModelProviderCredentials, secondSession.AppServer.ModelProviderCredentials)
	}
	for _, sessionID := range []string{"session-1", "session-2"} {
		if err := first.Cleanup(t.Context(), agentprep.CleanupInput{
			WorkspaceID: "workspace-1", AgentSessionID: sessionID, Provider: "codex",
		}); err != nil {
			t.Fatalf("cleanup %s: %v", sessionID, err)
		}
	}
}
