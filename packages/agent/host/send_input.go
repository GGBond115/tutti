package agenthost

import (
	"context"
	"errors"
	"strings"
)

// SendInput reports at most one aggregated TerminalFailure for a failed
// command. Guidance target binding and goal control own their own emissions.
func (h *Host) SendInput(ctx context.Context, ref SessionRef, input SendInput) (SendInputResult, error) {
	clientSubmitID := firstNonEmptyTrimmed(input.ClientSubmitID, legacyClientSubmitID(input.Metadata))
	ctx, command := h.beginCommand(ctx, commandTerminalFailureInput{
		flow: "message_send", workspaceID: ref.WorkspaceID, agentSessionID: ref.AgentSessionID,
		operationID:    firstNonEmptyTrimmed(clientSubmitID, input.TurnID),
		clientSubmitID: clientSubmitID, turnID: input.TurnID,
	})
	result, err := h.sendInput(ctx, ref, input)
	command.finish(ctx, h, err)
	return result, err
}

func (h *Host) sendInput(ctx context.Context, ref SessionRef, input SendInput) (SendInputResult, error) {
	ref.WorkspaceID, ref.AgentSessionID = strings.TrimSpace(ref.WorkspaceID), strings.TrimSpace(ref.AgentSessionID)
	if h == nil || h.runtime == nil || h.store == nil || ref.WorkspaceID == "" || ref.AgentSessionID == "" {
		if input.Guidance {
			return guidanceSendResult(input.TurnID, GuidanceDeliveryDispositionPreconditionFailed), ErrInvalidArgument
		}
		return SendInputResult{}, ErrInvalidArgument
	}
	// Guidance is a mutation of an already-running canonical Turn. Host
	// consumers must bind that mutation to the exact Turn observed at the
	// interaction boundary; allowing the runtime to infer "current" would make
	// an A->B transition during transport silently steer B.
	if input.Guidance && strings.TrimSpace(input.TurnID) == "" {
		err := ErrActiveTurnTargetRequired
		h.observeTerminalFailure(ctx, TerminalFailure{
			Flow: "guidance", FailureStage: "guidance_target",
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
			ClientSubmitID: strings.TrimSpace(input.ClientSubmitID),
			ErrorCode:      guidanceTargetFailureCode(err), ErrorMessage: err.Error(), Retryable: false,
		})
		return guidanceSendResult("", GuidanceDeliveryDispositionPreconditionFailed), err
	}
	normalized, promptText, err := normalizePromptContent(input.Content)
	if err != nil {
		if input.Guidance {
			return guidanceSendResult(input.TurnID, GuidanceDeliveryDispositionPreconditionFailed), err
		}
		return SendInputResult{}, err
	}
	metadata := submissionMetadata(input.Metadata, input.ClientSubmitID)
	if typedGoal, ok := ParseTypedGoalControl(normalized, input.Guidance); ok {
		goalResult, goalErr := h.goalControl(ctx, GoalControlInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
			Action: typedGoal.Action, Objective: typedGoal.Objective,
			ClientSubmitID: input.ClientSubmitID, SubmissionMetadata: metadata,
		})
		if goalErr != nil {
			return SendInputResult{}, goalErr
		}
		session, _ := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID)
		return SendInputResult{
			Session: session, Canonical: goalResult.Canonical,
			Kind: "goalControl", GoalControl: &goalResult,
		}, nil
	}
	var guidanceClaim *guidanceSubmitClaimPreparation
	if input.Guidance {
		claim, created, claimErr := h.prepareGuidanceSubmitClaim(ctx, ref, metadata, input.TurnID)
		if claimErr != nil {
			return guidanceSendResult(input.TurnID, GuidanceDeliveryDispositionOutcomeUnknown),
				errors.Join(ErrSubmitDeliveryUnknown, claimErr)
		}
		if claim.ClientSubmitID != "" && !created {
			if claim.GuidanceDisposition != "" {
				return h.replayedGuidanceSubmitResult(ctx, ref, claim)
			}
			if claim.Status == "accepted" || claim.Status == "rejected" {
				replayed, replayErr := h.replayedSubmitResult(ctx, ref, claim)
				replayed.GuidanceDisposition = GuidanceDeliveryDispositionApplied
				return replayed, replayErr
			}
		}
		guidanceClaim = &guidanceSubmitClaimPreparation{claim: claim, created: created}
	}
	var result SendInputResult
	err = h.withSessionMutationActor(ctx, ref.WorkspaceID, ref.AgentSessionID, func(actorCtx context.Context) error {
		var sendErr error
		result, sendErr = h.sendInputSerialized(actorCtx, ref, input, normalized, promptText, metadata, guidanceClaim)
		return sendErr
	})
	if err != nil && input.Guidance && result.GuidanceDisposition == "" {
		if guidanceClaim != nil && guidanceClaim.created {
			return h.finishGuidancePreconditionFailure(
				ref, guidanceClaim.claim.ClientSubmitID, input.TurnID, err,
			)
		}
		if guidanceClaim != nil && guidanceClaim.claim.ClientSubmitID != "" {
			return guidanceSendResult(input.TurnID, GuidanceDeliveryDispositionOutcomeUnknown),
				errors.Join(ErrSubmitDeliveryUnknown, err)
		}
		return guidanceSendResult(input.TurnID, GuidanceDeliveryDispositionPreconditionFailed), err
	}
	return result, err
}
