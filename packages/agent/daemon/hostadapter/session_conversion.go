package hostadapter

import (
	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	host "github.com/tutti-os/tutti/packages/agent/host"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

func (a *RuntimeController) fromSession(session agentruntime.Session) host.ProviderRuntimeSession {
	var settings *host.ComposerSettings
	if session.Settings != nil {
		value := hostSettings(*session.Settings)
		settings = &value
	}
	return host.ProviderRuntimeSession{
		ID: session.AgentSessionID, WorkspaceID: session.RoomID, UserID: a.currentUserID(),
		Scope:                host.RuntimeSessionScope(session.Scope),
		SourceAgentSessionID: session.SourceAgentSessionID, SideRequestID: session.SideRequestID,
		AgentTargetID: session.AgentTargetID, Provider: session.Provider, ProviderSessionID: session.ProviderSessionID,
		Resumable: session.Resumable,
		Cwd:       session.CWD, Env: append([]string(nil), session.Env...), MCPServers: hostMCPServerBindings(session.MCPServers), Settings: settings,
		AppServer:         hostAppServerPreparation(session.AppServer),
		ProviderTargetRef: cloneMap(session.ProviderTargetRef),
		RuntimeContext:    cloneMap(session.RuntimeContext), Status: session.Status,
		TurnLifecycle: hostTurnLifecyclePointer(session.TurnLifecycle), SubmitAvailability: hostSubmitAvailability(session.SubmitAvailability),
		Visible: session.Visible, Title: session.Title, InitialTitleEstablished: session.InitialTitleEstablished,
		LastError: session.LastError, CreatedAtUnixMS: session.CreatedAtUnixMS, UpdatedAtUnixMS: session.UpdatedAtUnixMS,
	}
}

func runtimeSession(session host.ProviderRuntimeSession) agentruntime.Session {
	var settings *agentruntime.SessionSettings
	if session.Settings != nil {
		settings = runtimeSettings(*session.Settings)
	}
	return agentruntime.Session{
		RoomID: session.WorkspaceID, AgentSessionID: session.ID,
		Scope:                agentruntime.RuntimeSessionScope(session.Scope),
		SourceAgentSessionID: session.SourceAgentSessionID, SideRequestID: session.SideRequestID,
		AgentTargetID: session.AgentTargetID, Provider: session.Provider,
		ProviderSessionID: session.ProviderSessionID, Resumable: session.Resumable,
		CWD: session.Cwd, Env: append([]string(nil), session.Env...), MCPServers: runtimeMCPServerBindings(session.MCPServers),
		AppServer: runtimeAppServerPreparation(session.AppServer),
		Status:    session.Status, TurnLifecycle: runtimeTurnLifecyclePointer(session.TurnLifecycle),
		SubmitAvailability: runtimeSubmitAvailability(session.SubmitAvailability),
		Title:              session.Title, LastError: session.LastError, Visible: session.Visible,
		RuntimeContext: cloneMap(session.RuntimeContext), ProviderTargetRef: cloneMap(session.ProviderTargetRef),
		Settings:        settings,
		CreatedAtUnixMS: session.CreatedAtUnixMS, UpdatedAtUnixMS: session.UpdatedAtUnixMS,
		InitialTitleEstablished: session.InitialTitleEstablished,
	}
}

// sessionWithState preserves the daemon runtime's provider-enriched live
// observation. The base Session owns process identity and lifecycle fields;
// State overlays provider-computed settings and runtime context such as model
// catalogs, usage, rate limits, account details, and commands.
func (a *RuntimeController) sessionWithState(session agentruntime.Session) host.ProviderRuntimeSession {
	result := a.fromSession(session)
	if a == nil || a.Backend == nil {
		return result
	}
	state, err := a.Backend.State(session.RoomID, session.AgentSessionID)
	if err != nil {
		return result
	}
	if state.ProviderSessionID != "" {
		result.ProviderSessionID = state.ProviderSessionID
	}
	result.Resumable = result.Resumable || state.Resumable
	if state.Status != "" {
		result.Status = state.Status
	}
	if state.TurnLifecycle != nil {
		result.TurnLifecycle = hostTurnLifecyclePointer(state.TurnLifecycle)
	}
	if state.SubmitAvailability != nil {
		result.SubmitAvailability = hostSubmitAvailability(state.SubmitAvailability)
	}
	if state.Settings != nil {
		settings := hostSettings(*state.Settings)
		result.Settings = &settings
	}
	result.Capabilities = canonical.CloneCapabilitySnapshot(state.Capabilities)
	result.RuntimeContext = cloneMap(state.RuntimeContext)
	if state.UpdatedAtUnixMS > 0 {
		result.UpdatedAtUnixMS = state.UpdatedAtUnixMS
	}
	return result
}
