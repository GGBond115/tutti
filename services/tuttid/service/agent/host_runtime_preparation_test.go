package agent

import (
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestHostAppServerPreparationPassesThreadCredentialDTO(t *testing.T) {
	input := &runtimeprep.AppServerPreparedRuntime{
		ProviderStateID: "provider-state-account-a",
		ExecutionHostID: "device-1", RuntimeGeneration: "runtime-1", TransportScopeID: "transport-1",
		ProcessProfileDigest: "profile-1", ProcessCwd: "/profile",
		ProcessEnv: []string{"CODEX_HOME=/profile/codex-home"},
		ThreadEnv:  []string{"TUTTI_AGENT_SESSION_ID=session-1"},
		ModelProviderCredentials: []runtimeprep.AppServerModelProviderCredential{{
			ModelProviderID: runtimeprep.ModelPlanProviderID, BearerToken: "session-token",
		}},
	}

	got := hostAppServerPreparation(input)
	input.ModelProviderCredentials[0].BearerToken = "mutated"
	if got == nil || got.ProviderStateID != "provider-state-account-a" || len(got.ModelProviderCredentials) != 1 ||
		got.ModelProviderCredentials[0].ModelProviderID != runtimeprep.ModelPlanProviderID ||
		got.ModelProviderCredentials[0].BearerToken != "session-token" {
		t.Fatalf("host AppServer preparation = %#v", got)
	}
}
