package main

import (
	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
)

func agentRuntimeStreamEvents(events <-chan agentruntime.StreamEvent) <-chan agentservice.RuntimeStreamEvent {
	out := make(chan agentservice.RuntimeStreamEvent)
	go func() {
		defer close(out)
		for event := range events {
			out <- agentservice.RuntimeStreamEvent{EventType: event.EventType, Data: event.Data}
		}
	}()
	return out
}

func agentRuntimeSession(session agentruntime.Session) agentservice.ProviderRuntimeSession {
	return agentservice.ProviderRuntimeSession{
		ID: session.AgentSessionID, WorkspaceID: session.RoomID, AgentTargetID: session.AgentTargetID,
		Provider: session.Provider, ProviderSessionID: session.ProviderSessionID, Resumable: session.Resumable,
		Cwd: session.CWD, Env: append([]string(nil), session.Env...), MCPServers: serviceMCPServerBindings(session.MCPServers),
		AppServer: hostRuntimeAppServerPreparation(session.AppServer), Settings: agentRuntimeComposerSettings(session.Settings),
		Status: session.Status, TurnLifecycle: serviceTurnLifecyclePointerFromRuntime(session.TurnLifecycle),
		SubmitAvailability: serviceSubmitAvailabilityPointerFromRuntime(session.SubmitAvailability), Visible: session.Visible,
		Title: session.Title, InitialTitleEstablished: session.InitialTitleEstablished, LastError: session.LastError,
		RuntimeContext: cloneRuntimeContext(session.RuntimeContext), CreatedAtUnixMS: session.CreatedAtUnixMS,
		UpdatedAtUnixMS: session.UpdatedAtUnixMS,
	}
}

func hostRuntimeAppServerPreparation(input *agentruntime.AppServerRuntimePreparation) *agenthost.AppServerRuntimePreparation {
	if input == nil {
		return nil
	}
	result := &agenthost.AppServerRuntimePreparation{
		ProviderStateID: input.ProviderStateID,
		ExecutionHostID: input.ExecutionHostID, RuntimeGeneration: input.RuntimeGeneration,
		TransportScopeID: input.TransportScopeID, ProcessProfileDigest: input.ProcessProfileDigest,
		ProcessCwd: input.ProcessCWD, ProcessEnv: append([]string(nil), input.ProcessEnv...),
		ThreadEnv: append([]string(nil), input.ThreadEnv...), BaseInstructions: input.BaseInstructions,
		DeveloperInstructions: input.DeveloperInstructions,
	}
	for _, credential := range input.ModelProviderCredentials {
		result.ModelProviderCredentials = append(result.ModelProviderCredentials, agenthost.AppServerModelProviderCredential{
			ModelProviderID: credential.ModelProviderID, BearerToken: credential.BearerToken,
		})
	}
	return result
}

func daemonMCPServerBindings(input []agenthost.MCPServerBinding) []agentruntime.MCPServerBinding {
	if len(input) == 0 {
		return nil
	}
	result := make([]agentruntime.MCPServerBinding, 0, len(input))
	for _, binding := range input {
		headers := make(map[string]string, len(binding.Headers))
		for key, value := range binding.Headers {
			headers[key] = value
		}
		result = append(result, agentruntime.MCPServerBinding{Name: binding.Name, Type: binding.Type, URL: binding.URL, Headers: headers})
	}
	return result
}

func serviceMCPServerBindings(input []agentruntime.MCPServerBinding) []agenthost.MCPServerBinding {
	if len(input) == 0 {
		return nil
	}
	result := make([]agenthost.MCPServerBinding, 0, len(input))
	for _, binding := range input {
		headers := make(map[string]string, len(binding.Headers))
		for key, value := range binding.Headers {
			headers[key] = value
		}
		result = append(result, agenthost.MCPServerBinding{Name: binding.Name, Type: binding.Type, URL: binding.URL, Headers: headers})
	}
	return result
}

func serviceSubmitAvailabilityPointerFromRuntime(value *agentruntime.SubmitAvailability) *agentservice.SubmitAvailability {
	if value == nil {
		return nil
	}
	converted := serviceSubmitAvailabilityFromRuntime(*value)
	return &converted
}

func serviceTurnLifecyclePointerFromRuntime(value *agentruntime.TurnLifecycle) *agentservice.TurnLifecycle {
	if value == nil {
		return nil
	}
	converted := serviceTurnLifecycleFromRuntime(*value)
	return &converted
}

func agentRuntimeComposerSettings(settings *agentruntime.SessionSettings) *agentservice.ComposerSettings {
	if settings == nil {
		return nil
	}
	return &agentservice.ComposerSettings{
		Model: settings.Model, PermissionModeID: settings.PermissionModeID, PlanMode: settings.PlanMode,
		BrowserUse: cloneOptionalBool(settings.BrowserUse), ReasoningEffort: settings.ReasoningEffort,
		Speed: settings.Speed, ConversationDetailMode: settings.ConversationDetailMode,
	}
}

func cloneRuntimeContext(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = cloneRuntimeContextValue(item)
	}
	return cloned
}

func cloneRuntimeContextValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneRuntimeContextValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = cloneRuntimeContextValue(item)
		}
		return out
	default:
		return value
	}
}

func cloneOptionalBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
