package hostadapter

import (
	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	host "github.com/tutti-os/tutti/packages/agent/host"
)

func hostAppServerPreparation(input *agentruntime.AppServerRuntimePreparation) *host.AppServerRuntimePreparation {
	if input == nil {
		return nil
	}
	return &host.AppServerRuntimePreparation{
		ExecutionHostID: input.ExecutionHostID, RuntimeGeneration: input.RuntimeGeneration,
		TransportScopeID: input.TransportScopeID, ProcessProfileDigest: input.ProcessProfileDigest,
		ProcessCwd: input.ProcessCWD, ProcessEnv: append([]string(nil), input.ProcessEnv...),
		ThreadEnv:                append([]string(nil), input.ThreadEnv...),
		ModelProviderCredentials: hostAppServerModelProviderCredentials(input.ModelProviderCredentials),
		BaseInstructions:         input.BaseInstructions,
		DeveloperInstructions:    input.DeveloperInstructions,
	}
}

func hostAppServerModelProviderCredentials(input []agentruntime.AppServerModelProviderCredential) []host.AppServerModelProviderCredential {
	result := make([]host.AppServerModelProviderCredential, 0, len(input))
	for _, credential := range input {
		result = append(result, host.AppServerModelProviderCredential{
			ModelProviderID: credential.ModelProviderID, BearerToken: credential.BearerToken,
		})
	}
	return result
}

func runtimeAppServerPreparation(input *host.AppServerRuntimePreparation) *agentruntime.AppServerRuntimePreparation {
	if input == nil {
		return nil
	}
	return &agentruntime.AppServerRuntimePreparation{
		ExecutionHostID: input.ExecutionHostID, RuntimeGeneration: input.RuntimeGeneration,
		TransportScopeID: input.TransportScopeID, ProcessProfileDigest: input.ProcessProfileDigest,
		ProcessCWD: input.ProcessCwd, ProcessEnv: append([]string(nil), input.ProcessEnv...),
		ThreadEnv:                append([]string(nil), input.ThreadEnv...),
		ModelProviderCredentials: runtimeAppServerModelProviderCredentials(input.ModelProviderCredentials),
		BaseInstructions:         input.BaseInstructions,
		DeveloperInstructions:    input.DeveloperInstructions,
	}
}

func runtimeAppServerModelProviderCredentials(input []host.AppServerModelProviderCredential) []agentruntime.AppServerModelProviderCredential {
	result := make([]agentruntime.AppServerModelProviderCredential, 0, len(input))
	for _, credential := range input {
		result = append(result, agentruntime.AppServerModelProviderCredential{
			ModelProviderID: credential.ModelProviderID, BearerToken: credential.BearerToken,
		})
	}
	return result
}
