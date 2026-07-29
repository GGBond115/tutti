package agenthost

import (
	"context"
	"strings"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func (h *Host) GetSessionForkCapabilities(
	ctx context.Context,
	input SessionForkCapabilityInput,
) (SessionForkCapabilities, error) {
	normalizeSessionForkCapabilityInput(&input)
	if h == nil || h.sessionForks == nil || h.sessionForkRuntime == nil ||
		input.WorkspaceID == "" || input.SourceAgentSessionID == "" {
		return SessionForkCapabilities{}, nil
	}
	sourceSession, found, err := h.sessionForks.GetSessionForkSource(
		ctx, input.WorkspaceID, input.SourceAgentSessionID,
	)
	if err != nil || !found {
		return SessionForkCapabilities{}, err
	}
	runtimeSource := h.sessionForkCapabilityRuntimeSource(sourceSession)
	if strings.TrimSpace(runtimeSource.ProviderSessionID) !=
		strings.TrimSpace(sourceSession.ProviderSessionID) {
		return SessionForkCapabilities{}, nil
	}
	descriptor, err := h.sessionForkRuntime.ResolveSessionFork(
		ctx,
		cloneSessionForkRuntimeSource(runtimeSource),
	)
	if err != nil {
		return SessionForkCapabilities{}, err
	}
	normalizeSessionForkDriverDescriptor(&descriptor)
	capabilities := SessionForkCapabilities{
		FullSession: descriptor.FullSession &&
			descriptor.Kind != "" &&
			descriptor.Version != "" &&
			validSessionForkStateBindingMode(
				descriptor.StateBindingMode,
				h.sessionForkState,
				runtimeSource.Provider,
			),
		ThroughTurn: descriptor.ThroughTurn &&
			descriptor.Kind != "" &&
			descriptor.Version != "" &&
			validSessionForkStateBindingMode(
				descriptor.StateBindingMode,
				h.sessionForkState,
				runtimeSource.Provider,
			),
	}
	return capabilities, nil
}

// sessionForkCapabilityRuntimeSource is intentionally preparation-free.
// Capability projection runs on Session detail reads and may only consume a
// live runtime observation or persisted canonical attestation. Full runtime,
// target, credential, and worktree preparation belongs to ForkSession.
func (h *Host) sessionForkCapabilityRuntimeSource(
	session storesqlite.Session,
) ProviderRuntimeSession {
	if h != nil && h.runtime != nil {
		if live, found := h.runtime.Session(session.WorkspaceID, session.ID); found {
			return cloneSessionForkRuntimeSource(live)
		}
	}
	settings := composerSettingsFromMap(session.Settings)
	return ProviderRuntimeSession{
		ID: session.ID, WorkspaceID: session.WorkspaceID, UserID: session.UserID,
		AgentTargetID: session.AgentTargetID, Provider: session.Provider,
		ProviderSessionID: session.ProviderSessionID, Resumable: true,
		Cwd: session.Cwd, Settings: &settings,
		RuntimeContext: cloneMap(session.InternalRuntimeContext),
		Status:         persistedRuntimeStatus(session.ActiveTurnID), Title: session.Title,
		PinnedAtUnixMS: session.PinnedAtUnixMS, CreatedAtUnixMS: session.CreatedAtUnixMS,
		UpdatedAtUnixMS: session.UpdatedAtUnixMS,
	}
}

func normalizeSessionForkDriverDescriptor(input *SessionForkDriverDescriptor) {
	input.Kind = strings.TrimSpace(input.Kind)
	input.Version = strings.TrimSpace(input.Version)
	if input.StateBindingMode == "" {
		input.StateBindingMode = SessionForkStateBindingHostCopy
	}
}

func validSessionForkStateBindingMode(
	mode SessionForkStateBindingMode,
	hostBinder SessionForkProviderStateBinder,
	provider string,
) bool {
	switch mode {
	case SessionForkStateBindingHostCopy:
		return hostBinder != nil &&
			hostBinder.SupportsSessionForkProviderStateBinding(provider)
	case SessionForkStateBindingProviderOwned:
		return true
	default:
		return false
	}
}
