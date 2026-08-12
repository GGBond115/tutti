package agenthost

import (
	"context"
	"strings"
)

// ReprepareRuntimeSession replaces the live provider connection and its MCP
// bindings while preserving the canonical Session, history, and provider
// session identity. The operation is admitted only while both durable and
// runtime lifecycle state prove that the Session is idle.
func (h *Host) ReprepareRuntimeSession(
	ctx context.Context,
	input ReprepareRuntimeSessionInput,
) (ProviderRuntimeSession, error) {
	ref := SessionRef{
		WorkspaceID:    strings.TrimSpace(input.WorkspaceID),
		AgentSessionID: strings.TrimSpace(input.AgentSessionID),
	}
	if h == nil || h.runtime == nil || h.store == nil || h.preparation == nil ||
		ref.WorkspaceID == "" || ref.AgentSessionID == "" {
		return ProviderRuntimeSession{}, ErrInvalidArgument
	}
	repreparer, ok := h.runtime.(RuntimeSessionRepreparer)
	if !ok {
		return ProviderRuntimeSession{}, ErrRuntimeSessionReprepareUnavailable
	}
	release, err := h.acquireSession(ctx, ref)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	defer release()

	deleted, err := h.store.SessionDeleted(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	if deleted {
		return ProviderRuntimeSession{}, ErrSessionNotFound
	}
	canonicalSession, found, err := h.store.GetSession(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	if !found || ResolveResumePolicy(canonicalSession).Mode == ResumeModeReject ||
		strings.TrimSpace(canonicalSession.ProviderSessionID) == "" {
		return ProviderRuntimeSession{}, ErrSessionNotFound
	}
	if strings.TrimSpace(canonicalSession.ActiveTurnID) != "" {
		return ProviderRuntimeSession{}, ErrRuntimeSessionActive
	}
	live, found := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID)
	if !found {
		return ProviderRuntimeSession{}, ErrRuntimeSessionDisconnected
	}
	if runtimeSessionHasActiveTurn(live) {
		return ProviderRuntimeSession{}, ErrRuntimeSessionActive
	}
	evidence, err := h.store.GetProviderSessionResumeEvidence(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	if !evidence.Established {
		return ProviderRuntimeSession{}, ErrProviderSessionNotEstablished
	}

	settings := composerSettingsFromMap(canonicalSession.Settings)
	preparationInput := resumePreparationInput(canonicalSession, settings)
	preparationInput.RuntimeContext = overlayRuntimeContext(
		canonicalSession.InternalRuntimeContext,
		input.RuntimeContextOverlay,
	)
	prepared, err := h.preparation.Prepare(ctx, preparationInput)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	if prepared.Settings != nil {
		settings = *prepared.Settings
	}
	goalGenerationFences, err := h.listRuntimeGoalGenerationFences(ctx, ref)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	releaseStartup, err := h.acquireStartup(ctx, canonicalSession.Provider)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	defer releaseStartup()
	result, err := repreparer.Reprepare(ctx, RuntimeResumeInput{
		WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
		AgentTargetID: strings.TrimSpace(canonicalSession.AgentTargetID), Provider: strings.TrimSpace(canonicalSession.Provider),
		ProviderSessionID: strings.TrimSpace(canonicalSession.ProviderSessionID), Resumable: true, Cwd: prepared.Cwd,
		Env: append([]string(nil), prepared.Env...), MCPServers: cloneHostMCPServerBindings(prepared.MCPServers), Title: strings.TrimSpace(canonicalSession.Title),
		Status: persistedRuntimeStatus(""), Settings: settings,
		CreatedAtUnixMS: canonicalSession.CreatedAtUnixMS, UpdatedAtUnixMS: canonicalSession.UpdatedAtUnixMS,
		Visible: boolPointer(canonicalSession.Metadata.Visible), RuntimeContext: cloneMap(canonicalSession.InternalRuntimeContext),
		ProviderTargetRef: cloneMap(prepared.ProviderTargetRef), Metadata: canonicalSession.Metadata,
		InternalRuntimeContext: cloneMap(canonicalSession.InternalRuntimeContext),
		GoalGenerationFences:   append([]RuntimeGoalGenerationFenceInput(nil), goalGenerationFences...),
	})
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	return result, nil
}
