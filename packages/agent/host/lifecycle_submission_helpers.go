package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func (h *Host) recordTurnSubmission(
	ctx context.Context,
	ref SessionRef,
	turnID string,
	clientSubmitID string,
	content []PromptContentBlock,
	displayPrompt string,
	capabilityRefs []CapabilityReference,
	tuttiModeSnapshot *TuttiModeTurnSnapshot,
) error {
	if h == nil || h.turnSubmissions == nil {
		return nil
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("encode turn submission content: %w", err)
	}
	capabilityRefsJSON, err := json.Marshal(capabilityRefs)
	if err != nil {
		return fmt.Errorf("encode turn submission capability refs: %w", err)
	}
	tuttiModeSnapshotJSON, err := json.Marshal(tuttiModeSnapshot)
	if err != nil {
		return fmt.Errorf("encode turn submission tutti mode snapshot: %w", err)
	}
	now := h.now().UnixMilli()
	_, _, err = h.turnSubmissions.RecordTurnSubmission(ctx, storesqlite.TurnSubmission{
		WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
		TurnID: strings.TrimSpace(turnID), ContentJSON: string(contentJSON),
		DisplayPrompt:         strings.TrimSpace(displayPrompt),
		CapabilityRefsJSON:    string(capabilityRefsJSON),
		TuttiModeSnapshotJSON: string(tuttiModeSnapshotJSON),
		ClientSubmitID:        strings.TrimSpace(clientSubmitID),
		CreatedAtUnixMS:       now, UpdatedAtUnixMS: now,
	})
	if err != nil {
		return fmt.Errorf("record turn submission envelope: %w", err)
	}
	return nil
}

func (h *Host) requireSendAllowedByEffectiveHistory(ctx context.Context, ref SessionRef) error {
	if h == nil || h.effectiveHistory == nil {
		return nil
	}
	history, found, err := h.effectiveHistory.GetSessionHistory(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil || !found || history.RecoveryState == storesqlite.SessionHistoryRecoveryReady {
		return err
	}
	if h.editRetryDisabled {
		// Durable edit-retry is neutralized, so a fence whose owning operation is
		// no longer in flight can never clear through the saga (recovery only
		// quarantines claimable operations; a previously failed one is invisible
		// to it). Heal it here so the session is not send-blocked forever; if the
		// clear does not apply (operation still in flight), fall through to the
		// normal fence error.
		if cleared, clearErr := h.effectiveHistory.ClearAbandonedEditRetryFence(ctx, storesqlite.ClearAbandonedEditRetryFenceInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID, NowUnixMS: h.now().UnixMilli(),
		}); clearErr == nil && cleared {
			return nil
		}
	}
	switch history.RecoveryState {
	case storesqlite.SessionHistoryRecoveryRollbackPending:
		return ErrEditRetryInProgress
	case storesqlite.SessionHistoryRecoveryRequired:
		return ErrEditRetryRecoveryRequired
	default:
		return ErrEditRetryResendPending
	}
}
