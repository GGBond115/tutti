package agenthost

import (
	"context"
	"strings"
	"time"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

type guidanceSubmitClaimPreparation struct {
	claim   storesqlite.SubmitClaim
	created bool
}

func legacyClientSubmitID(metadata map[string]any) string {
	value, _ := metadata["clientSubmitId"].(string)
	return strings.TrimSpace(value)
}

func submissionMetadata(metadata map[string]any, typedClientSubmitID string) map[string]any {
	clientID := strings.TrimSpace(typedClientSubmitID)
	if clientID == "" {
		return metadata
	}
	result := cloneMap(metadata)
	if result == nil {
		result = make(map[string]any, 1)
	}
	result["clientSubmitId"] = clientID
	return result
}

func (h *Host) prepareSubmitClaim(ctx context.Context, ref SessionRef, metadata map[string]any, canonicalTurnID string) (storesqlite.SubmitClaim, bool, error) {
	return h.prepareSubmitClaimWithBinding(ctx, ref, metadata, canonicalTurnID, false)
}

func (h *Host) prepareGuidanceSubmitClaim(
	ctx context.Context,
	ref SessionRef,
	metadata map[string]any,
	canonicalTurnID string,
) (storesqlite.SubmitClaim, bool, error) {
	return h.prepareSubmitClaimWithBinding(ctx, ref, metadata, canonicalTurnID, true)
}

func (h *Host) prepareSubmitClaimWithBinding(
	ctx context.Context,
	ref SessionRef,
	metadata map[string]any,
	canonicalTurnID string,
	requireCanonicalTurnBinding bool,
) (storesqlite.SubmitClaim, bool, error) {
	clientID := legacyClientSubmitID(metadata)
	if h == nil || h.store == nil || clientID == "" {
		return storesqlite.SubmitClaim{}, false, nil
	}
	claim, created, err := h.store.PrepareSubmitClaim(ctx, storesqlite.SubmitClaimPrepare{
		WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
		ClientSubmitID: clientID, CanonicalTurnID: strings.TrimSpace(canonicalTurnID), NowUnixMS: h.now().UnixMilli(),
	})
	if err != nil || created {
		return claim, created, err
	}
	if requireCanonicalTurnBinding &&
		strings.TrimSpace(claim.CanonicalTurnID) != strings.TrimSpace(canonicalTurnID) {
		return claim, false, storesqlite.ErrSubmitClaimTurnConflict
	}
	if claim.Status != "prepared" {
		return claim, false, nil
	}
	turnID, found, err := h.store.FindTurnByClientSubmitID(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID)
	if err != nil || !found {
		return claim, false, err
	}
	if strings.TrimSpace(turnID) != strings.TrimSpace(claim.CanonicalTurnID) {
		return claim, false, storesqlite.ErrSubmitClaimTurnConflict
	}
	claim, _, err = h.store.AcceptSubmitClaim(
		ctx, ref.WorkspaceID, ref.AgentSessionID, clientID, turnID, h.now().UnixMilli(),
	)
	return claim, false, err
}

func (h *Host) recordGuidanceSubmitDisposition(
	ref SessionRef,
	clientID string,
	turnID string,
	disposition GuidanceDeliveryDisposition,
) error {
	if h == nil || h.store == nil || strings.TrimSpace(clientID) == "" {
		return nil
	}
	stored, ok := storeGuidanceDisposition(disposition)
	if !ok {
		return storesqlite.ErrSubmitClaimGuidanceDispositionConflict
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := h.store.RecordSubmitClaimGuidanceDisposition(
		ctx,
		storesqlite.SubmitClaimGuidanceDispositionRecord{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
			ClientSubmitID: clientID, CanonicalTurnID: strings.TrimSpace(turnID),
			Disposition: stored, NowUnixMS: h.now().UnixMilli(),
		},
	)
	return err
}

func storeGuidanceDisposition(
	disposition GuidanceDeliveryDisposition,
) (storesqlite.SubmitClaimGuidanceDisposition, bool) {
	switch disposition {
	case GuidanceDeliveryDispositionApplied:
		return storesqlite.SubmitClaimGuidanceDispositionApplied, true
	case GuidanceDeliveryDispositionPreconditionFailed:
		return storesqlite.SubmitClaimGuidanceDispositionPreconditionFailed, true
	case GuidanceDeliveryDispositionExplicitRejection:
		return storesqlite.SubmitClaimGuidanceDispositionExplicitRejection, true
	case GuidanceDeliveryDispositionOutcomeUnknown:
		return storesqlite.SubmitClaimGuidanceDispositionOutcomeUnknown, true
	default:
		return "", false
	}
}

func hostGuidanceDisposition(
	disposition storesqlite.SubmitClaimGuidanceDisposition,
) GuidanceDeliveryDisposition {
	switch disposition {
	case storesqlite.SubmitClaimGuidanceDispositionApplied:
		return GuidanceDeliveryDispositionApplied
	case storesqlite.SubmitClaimGuidanceDispositionPreconditionFailed:
		return GuidanceDeliveryDispositionPreconditionFailed
	case storesqlite.SubmitClaimGuidanceDispositionExplicitRejection:
		return GuidanceDeliveryDispositionExplicitRejection
	case storesqlite.SubmitClaimGuidanceDispositionOutcomeUnknown:
		return GuidanceDeliveryDispositionOutcomeUnknown
	default:
		return ""
	}
}

func (h *Host) abandonSubmitClaim(ref SessionRef, clientID string) {
	if h == nil || h.store == nil || strings.TrimSpace(clientID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = h.store.DeleteSubmitClaim(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID)
}

// abandonGuidanceSubmitClaim is the conversion barrier for adaptive guidance.
// Unlike legacy best-effort cleanup, it returns only after the durable delete
// is confirmed. A caller must not reuse the ClientSubmitID for ordinary work
// when this method returns an error.
func (h *Host) abandonGuidanceSubmitClaim(ref SessionRef, clientID string) error {
	if h == nil || h.store == nil || strings.TrimSpace(clientID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := h.store.DeleteSubmitClaim(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID)
	if err != nil {
		return err
	}
	// The store contract is idempotent. deleted=false means another cleanup
	// attempt already established the same durable absence postcondition.
	return nil
}

func (h *Host) acceptSubmitClaim(ref SessionRef, clientID, turnID string) error {
	if h == nil || h.store == nil || strings.TrimSpace(clientID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := h.store.AcceptSubmitClaim(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID, turnID, h.now().UnixMilli())
	return err
}

func (h *Host) rejectSubmitClaim(ref SessionRef, clientID, turnID string) error {
	if h == nil || h.store == nil || strings.TrimSpace(clientID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := h.store.RejectSubmitClaim(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID, turnID, h.now().UnixMilli())
	return err
}

func (h *Host) finalizeRejectedSubmitClaim(ref SessionRef, clientID, turnID string) error {
	if strings.TrimSpace(turnID) == "" {
		return nil
	}
	return h.rejectSubmitClaim(ref, clientID, turnID)
}

func (h *Host) replayedSubmitResult(ctx context.Context, ref SessionRef, claim storesqlite.SubmitClaim) (SendInputResult, error) {
	canonicalSession, ok, err := h.store.GetSession(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return SendInputResult{}, err
	}
	if !ok {
		if _, live := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID); !live {
			return SendInputResult{}, ErrSessionNotFound
		}
	}
	turn, ok, err := h.store.GetTurn(ctx, ref.WorkspaceID, ref.AgentSessionID, claim.TurnID)
	if err != nil {
		return SendInputResult{}, err
	}
	if !ok {
		return SendInputResult{}, ErrSubmitDeliveryUnknown
	}
	live, _ := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID)
	availability := SubmitAvailability{State: "available"}
	if strings.TrimSpace(canonicalSession.ActiveTurnID) != "" {
		availability = SubmitAvailability{State: "blocked", Reason: "active_turn"}
	}
	return SendInputResult{
		Session: live, Canonical: canonicalSession, Turn: &turn, TurnID: claim.TurnID,
		TurnLifecycle: lifecycleFromTurn(turn), SubmitAvailability: availability,
	}, nil
}

func (h *Host) replayedGuidanceSubmitResult(
	ctx context.Context,
	ref SessionRef,
	claim storesqlite.SubmitClaim,
) (SendInputResult, error) {
	disposition := hostGuidanceDisposition(claim.GuidanceDisposition)
	result := guidanceSendResult(claim.CanonicalTurnID, disposition)
	switch disposition {
	case GuidanceDeliveryDispositionApplied:
		if claim.Status == "prepared" {
			acceptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			accepted, _, err := h.store.AcceptSubmitClaim(
				acceptCtx, ref.WorkspaceID, ref.AgentSessionID,
				claim.ClientSubmitID, claim.CanonicalTurnID, h.now().UnixMilli(),
			)
			cancel()
			if err != nil {
				return result, err
			}
			claim = accepted
		}
		replayed, err := h.replayedSubmitResult(ctx, ref, claim)
		replayed.GuidanceDisposition = disposition
		return replayed, err
	case GuidanceDeliveryDispositionPreconditionFailed:
		return result, ErrGuidancePreconditionFailed
	case GuidanceDeliveryDispositionExplicitRejection:
		return result, ErrGuidanceExplicitRejection
	case GuidanceDeliveryDispositionOutcomeUnknown:
		return result, ErrSubmitDeliveryUnknown
	default:
		return guidanceSendResult(claim.CanonicalTurnID, GuidanceDeliveryDispositionOutcomeUnknown),
			ErrSubmitDeliveryUnknown
	}
}
