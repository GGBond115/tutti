package agenthost

import (
	"context"
	"errors"
	"strings"
	"time"
)

// GuidanceDeliveryDisposition is the provider-neutral, exact-target delivery
// verdict for one guidance submission. Only TargetInactive proves that no
// provider request was made and that the same ClientSubmitID may be reused for
// ordinary work after Host durably removes its prepared guidance claim.
type GuidanceDeliveryDisposition string

const (
	GuidanceDeliveryDispositionApplied            GuidanceDeliveryDisposition = "applied"
	GuidanceDeliveryDispositionTargetInactive     GuidanceDeliveryDisposition = "not_dispatched_target_inactive"
	GuidanceDeliveryDispositionPreconditionFailed GuidanceDeliveryDisposition = "not_dispatched_precondition_failed"
	GuidanceDeliveryDispositionExplicitRejection  GuidanceDeliveryDisposition = "not_dispatched_explicit_rejection"
	GuidanceDeliveryDispositionOutcomeUnknown     GuidanceDeliveryDisposition = "outcome_unknown"
)

func (h *Host) finishGuidanceSend(
	ctx context.Context,
	ref SessionRef,
	input SendInput,
	session ProviderRuntimeSession,
	claimClientSubmitID string,
	claimCreatedAtUnixMS int64,
	claimPending bool,
	preparedContent preparedPromptContent,
	displayPrompt string,
	execResult RuntimeExecResult,
	startedAt time.Time,
	execErr error,
) (SendInputResult, error) {
	disposition := execResult.ProviderDispatch.GuidanceDisposition
	if disposition == "" {
		execResult.ProviderDispatch.Disposition = RuntimeDispatchDispositionOutcomeUnknown
		disposition = GuidanceDeliveryDispositionOutcomeUnknown
		if execErr == nil {
			execErr = errors.New("runtime guidance returned without a typed delivery disposition")
		}
	} else if disposition != GuidanceDeliveryDispositionApplied && execErr == nil {
		execErr = errors.New("runtime guidance returned a non-applied disposition without an error")
	}
	if disposition != GuidanceDeliveryDispositionTargetInactive {
		if persistErr := h.recordGuidanceSubmitDisposition(
			ref, claimClientSubmitID, input.TurnID, disposition,
		); persistErr != nil {
			return guidanceSendResult(input.TurnID, GuidanceDeliveryDispositionOutcomeUnknown),
				errors.Join(ErrSubmitDeliveryUnknown, execErr, persistErr)
		}
	}
	if execErr != nil {
		return h.finishFailedGuidanceSend(
			ctx, ref, input.TurnID, session.Provider, claimClientSubmitID,
			claimPending, disposition, startedAt, execErr,
		)
	}

	turnID := firstNonEmptyTrimmed(execResult.TurnID, input.TurnID)
	if turnID == "" {
		return guidanceSendResult("", GuidanceDeliveryDispositionApplied), ErrSubmitDeliveryUnknown
	}
	if reporter, ok := h.runtime.(RuntimeSubmitProvenanceReporter); ok {
		if err := reporter.DurablyReportSubmitProvenance(ctx, RuntimeSubmitProvenanceInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID, TurnID: turnID,
			ClientSubmitID: claimClientSubmitID, CanonicalSubmitOccurredAtUnixMS: claimCreatedAtUnixMS,
			Content: preparedContent.Hydrated, DisplayPrompt: displayPrompt, Guidance: true,
		}); err != nil {
			return guidanceSendResult(turnID, GuidanceDeliveryDispositionApplied), err
		}
	}
	if claimClientSubmitID != "" {
		if err := h.acceptSubmitClaim(ref, claimClientSubmitID, turnID); err != nil {
			return guidanceSendResult(turnID, GuidanceDeliveryDispositionApplied), err
		}
	}
	h.observeStep(ctx, "message_send", "runtime_exec", ref.WorkspaceID, ref.AgentSessionID, session.Provider, startedAt, nil)
	canonicalSession, _, err := h.store.GetSession(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return guidanceSendResult(turnID, GuidanceDeliveryDispositionApplied), err
	}
	turn, found, err := h.store.GetTurn(ctx, ref.WorkspaceID, ref.AgentSessionID, turnID)
	if err != nil {
		return guidanceSendResult(turnID, GuidanceDeliveryDispositionApplied), err
	}
	result := SendInputResult{
		Session: session, Canonical: canonicalSession, TurnID: turnID,
		TurnLifecycle: execResult.TurnLifecycle, SubmitAvailability: execResult.SubmitAvailability,
		GuidanceDisposition: GuidanceDeliveryDispositionApplied,
	}
	if found {
		result.Turn = &turn
	}
	return result, nil
}

func (h *Host) finishFailedGuidanceSend(
	ctx context.Context,
	ref SessionRef,
	turnID string,
	provider string,
	claimClientSubmitID string,
	claimPending bool,
	disposition GuidanceDeliveryDisposition,
	startedAt time.Time,
	execErr error,
) (SendInputResult, error) {
	if disposition == GuidanceDeliveryDispositionTargetInactive ||
		errors.Is(execErr, ErrActiveTurnTargetMismatch) ||
		errors.Is(execErr, ErrActiveTurnTargetInactive) ||
		errors.Is(execErr, ErrActiveTurnTargetRequired) {
		h.observeGuidanceTargetFailure(ctx, ref, provider, turnID, claimClientSubmitID, startedAt, execErr)
	} else {
		h.observeStep(ctx, "message_send", "runtime_exec", ref.WorkspaceID, ref.AgentSessionID, provider, startedAt, execErr)
	}

	result := guidanceSendResult(turnID, disposition)
	switch disposition {
	case GuidanceDeliveryDispositionTargetInactive:
		if claimPending {
			if cleanupErr := h.abandonGuidanceSubmitClaim(ref, claimClientSubmitID); cleanupErr != nil {
				failureErr := errors.Join(ErrGuidanceSubmitClaimCleanupFailed, execErr, cleanupErr)
				if persistErr := h.recordGuidanceSubmitDisposition(
					ref, claimClientSubmitID, turnID, GuidanceDeliveryDispositionPreconditionFailed,
				); persistErr != nil {
					return guidanceSendResult(turnID, GuidanceDeliveryDispositionOutcomeUnknown),
						errors.Join(ErrSubmitDeliveryUnknown, failureErr, persistErr)
				}
				result.GuidanceDisposition = GuidanceDeliveryDispositionPreconditionFailed
				return result, failureErr
			}
		}
		return result, execErr
	case GuidanceDeliveryDispositionPreconditionFailed,
		GuidanceDeliveryDispositionExplicitRejection:
		// Known failures do not authorize identity reuse. Keep the prepared
		// claim as a durable duplicate-delivery fence.
		return result, execErr
	case GuidanceDeliveryDispositionApplied:
		// Provider application is already known. A later local error must not
		// erase that ACK or authorize conversion; keep the prepared claim fence.
		return result, execErr
	default:
		result.GuidanceDisposition = GuidanceDeliveryDispositionOutcomeUnknown
		return result, errors.Join(ErrSubmitDeliveryUnknown, execErr)
	}
}

func (h *Host) finishGuidancePreconditionFailure(
	ref SessionRef,
	claimClientSubmitID string,
	turnID string,
	err error,
) (SendInputResult, error) {
	if persistErr := h.recordGuidanceSubmitDisposition(
		ref, claimClientSubmitID, turnID, GuidanceDeliveryDispositionPreconditionFailed,
	); persistErr != nil {
		return guidanceSendResult(turnID, GuidanceDeliveryDispositionOutcomeUnknown),
			errors.Join(ErrSubmitDeliveryUnknown, err, persistErr)
	}
	return guidanceSendResult(turnID, GuidanceDeliveryDispositionPreconditionFailed), err
}

func guidanceSendResult(turnID string, disposition GuidanceDeliveryDisposition) SendInputResult {
	return SendInputResult{
		TurnID:              strings.TrimSpace(turnID),
		GuidanceDisposition: disposition,
	}
}
