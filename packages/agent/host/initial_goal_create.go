package agenthost

import (
	"context"
	"strings"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

// replayInitialGoalCreate projects one durable Goal-operation replay back into
// the CreateSession result without performing runtime preparation or startup.
func (h *Host) replayInitialGoalCreate(
	ctx context.Context,
	input CreateSessionInput,
	goalInput GoalControlInput,
) (CreateSessionResult, bool, error) {
	replay, found, err := h.existingGoalControlResult(ctx, goalInput)
	if err != nil || !found {
		return CreateSessionResult{}, found, err
	}
	if !railPlacementMatchesSession(input.RailPlacement, replay.Canonical) {
		return CreateSessionResult{}, true, ErrRailPlacementConflict
	}
	runtimeSession, live := h.runtime.Session(goalInput.WorkspaceID, input.AgentSessionID)
	if !live {
		runtimeSession = providerRuntimeSessionIdentity(replay.Canonical)
	}
	return CreateSessionResult{
		Session: runtimeSession, Canonical: replay.Canonical,
		Kind: "goalControl", GoalControl: &replay,
	}, true, nil
}

func providerRuntimeSessionIdentity(session storesqlite.Session) ProviderRuntimeSession {
	return ProviderRuntimeSession{
		ID:                strings.TrimSpace(session.ID),
		WorkspaceID:       strings.TrimSpace(session.WorkspaceID),
		UserID:            strings.TrimSpace(session.UserID),
		AgentTargetID:     strings.TrimSpace(session.AgentTargetID),
		Provider:          strings.TrimSpace(session.Provider),
		ProviderSessionID: strings.TrimSpace(session.ProviderSessionID),
		Cwd:               strings.TrimSpace(session.Cwd),
		Visible:           session.Metadata.Visible,
		Title:             strings.TrimSpace(session.Title),
		PinnedAtUnixMS:    session.PinnedAtUnixMS,
		CreatedAtUnixMS:   session.CreatedAtUnixMS,
		UpdatedAtUnixMS:   session.UpdatedAtUnixMS,
	}
}
