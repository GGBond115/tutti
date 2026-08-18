package main

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	agentprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	tuttitypes "github.com/tutti-os/tutti/services/tuttid/types"
)

func configureAgentAppServerPreparation(
	preparer *agentprep.DefaultPreparer,
	stateDir string,
	transportScopeID string,
) (agentruntime.ProviderLaunchPreparer, error) {
	if preparer == nil {
		return nil, errors.New("agent app-server preparation requires a runtime preparer")
	}
	transportScopeID = strings.TrimSpace(transportScopeID)
	if transportScopeID == "" {
		return nil, errors.New("agent app-server preparation requires a transport scope")
	}
	executionHostID, err := tuttitypes.LoadOrCreateDeviceID(stateDir)
	if err != nil {
		return nil, err
	}
	preparer.AppServerScope = agentprep.AppServerProfileScope{
		ExecutionHostID: executionHostID, RuntimeGeneration: uuid.NewString(),
		TransportScopeID: transportScopeID,
	}
	return newAgentAppServerProviderLaunchPreparer(preparer), nil
}

func newAgentAppServerProviderLaunchPreparer(
	leases agentprep.AppServerLaunchLeaseProvider,
) agentruntime.ProviderLaunchPreparer {
	if leases == nil {
		return nil
	}
	return func(ctx context.Context, input agentruntime.ProviderLaunchPrepareInput) (agentruntime.ProviderLaunchPrepareResult, error) {
		prepared := input.Session.AppServer
		if prepared == nil {
			return agentruntime.ProviderLaunchPrepareResult{
				Command: append([]string(nil), input.Command...), Env: append([]string(nil), input.Env...), CWD: input.CWD,
			}, nil
		}
		lease, err := leases.AcquireAppServerLaunchLease(ctx, agentprep.AppServerLaunchLeaseInput{
			WorkspaceID: input.Session.RoomID, AgentSessionID: input.Session.AgentSessionID,
			Provider: input.Provider,
		})
		if err != nil {
			return agentruntime.ProviderLaunchPrepareResult{}, err
		}
		processEnv := appServerProcessLaunchEnvironment(input.Env, input.Session.Env, prepared.ProcessEnv)
		return agentruntime.ProviderLaunchPrepareResult{AppServer: &agentruntime.AppServerLaunchPreparation{
			ProcessProfile: agentruntime.AppServerProcessProfile{
				ExecutionHostID: prepared.ExecutionHostID, RuntimeGeneration: prepared.RuntimeGeneration,
				TransportScopeID: prepared.TransportScopeID, ProcessProfileDigest: prepared.ProcessProfileDigest,
				Command: append([]string(nil), input.Command...), Env: processEnv, CWD: prepared.ProcessCWD,
			},
			ThreadOverlay: agentruntime.AppServerThreadOverlay{
				Env:                      append([]string(nil), prepared.ThreadEnv...),
				MCPServers:               appServerRuntimeMCPServers(input.Session.MCPServers),
				ModelProviderCredentials: appServerRuntimeModelProviderCredentials(prepared.ModelProviderCredentials),
				BaseInstructions:         prepared.BaseInstructions,
				DeveloperInstructions:    prepared.DeveloperInstructions,
			},
			ProcessCleanup: lease.ProcessCleanup, ThreadCleanup: lease.ThreadCleanup,
		}}, nil
	}
}

func appServerRuntimeModelProviderCredentials(
	input []agentruntime.AppServerModelProviderCredential,
) []agentruntime.AppServerModelProviderCredential {
	return append([]agentruntime.AppServerModelProviderCredential(nil), input...)
}

func appServerProcessLaunchEnvironment(launch, thread, profile []string) []string {
	removed := make(map[string]struct{}, len(thread)+6)
	for _, entry := range thread {
		if key, _, ok := strings.Cut(entry, "="); ok {
			removed[strings.ToUpper(strings.TrimSpace(key))] = struct{}{}
		}
	}
	for _, key := range []string{
		"CODEX_HOME", "TUTTI_AGENT_HOME", agentprep.ModelPlanAPIKeyEnv,
		"TUTTI_WORKSPACE_ID", "TUTTI_AGENT_SESSION_ID", agenthost.AgentCWDEnvironmentVariable,
		agenthost.AgentRailPlacementEnvironmentVariable,
	} {
		removed[key] = struct{}{}
	}
	result := make([]string, 0, len(launch)+len(profile))
	for _, entry := range launch {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, drop := removed[strings.ToUpper(strings.TrimSpace(key))]; drop {
				continue
			}
		}
		result = append(result, entry)
	}
	for _, entry := range profile {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		normalized := strings.ToUpper(strings.TrimSpace(key))
		filtered := result[:0]
		for _, existing := range result {
			existingKey, _, existingOK := strings.Cut(existing, "=")
			if existingOK && strings.ToUpper(strings.TrimSpace(existingKey)) == normalized {
				continue
			}
			filtered = append(filtered, existing)
		}
		result = append(filtered, entry)
	}
	return result
}

func appServerRuntimeMCPServers(input []agentruntime.MCPServerBinding) []agentruntime.MCPServerBinding {
	result := make([]agentruntime.MCPServerBinding, 0, len(input))
	for _, binding := range input {
		headers := make(map[string]string, len(binding.Headers))
		for key, value := range binding.Headers {
			headers[key] = value
		}
		binding.Headers = headers
		result = append(result, binding)
	}
	return result
}
