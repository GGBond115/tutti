package agenthost

import "strings"

type RuntimeSubmitProvenanceInput struct {
	WorkspaceID                     string
	AgentSessionID                  string
	TurnID                          string
	ClientSubmitID                  string
	CanonicalMessageID              string
	ProviderSessionID               string
	ProviderTurnID                  string
	DispatchStatus                  string
	DeliveryStatus                  string
	FailureReason                   string
	CanonicalSubmitOccurredAtUnixMS int64
	Guidance                        bool
}

func runtimeSubmitProvenanceInput(
	workspaceID string,
	agentSessionID string,
	turnID string,
	clientSubmitID string,
	occurredAtUnixMS int64,
	providerSessionID string,
	execResult RuntimeExecResult,
	failureReason string,
	guidance bool,
) RuntimeSubmitProvenanceInput {
	dispatch := execResult.ProviderDispatch
	if dispatch.Acceptance != nil {
		providerSessionID = firstNonEmpty(providerSessionID, dispatch.Acceptance.ProviderSessionID)
	}
	providerTurnID := ""
	if dispatch.Acceptance != nil {
		providerTurnID = strings.TrimSpace(dispatch.Acceptance.ProviderTurnID)
	}
	dispatchStatus, deliveryStatus := submitStatuses(dispatch.Disposition, failureReason)
	return RuntimeSubmitProvenanceInput{
		WorkspaceID: workspaceID, AgentSessionID: agentSessionID, TurnID: turnID,
		ClientSubmitID: clientSubmitID, CanonicalMessageID: "client-submit:user:" + clientSubmitID,
		ProviderSessionID: providerSessionID,
		ProviderTurnID:    providerTurnID, DispatchStatus: dispatchStatus,
		DeliveryStatus: deliveryStatus, FailureReason: failureReason,
		CanonicalSubmitOccurredAtUnixMS: occurredAtUnixMS, Guidance: guidance,
	}
}

func submitStatuses(disposition RuntimeDispatchDisposition, failureReason string) (string, string) {
	switch disposition {
	case RuntimeDispatchDispositionApplied, RuntimeDispatchDispositionAppliedWithoutProviderTurn:
		return "accepted", "accepted"
	case RuntimeDispatchDispositionRejected:
		return "failed", "failed"
	case RuntimeDispatchDispositionNotDispatched:
		return "not_started", "not_started"
	case RuntimeDispatchDispositionOutcomeUnknown:
		return "unknown", "unknown"
	default:
		if strings.TrimSpace(failureReason) != "" {
			return "unknown", "unknown"
		}
		return "not_started", "pending"
	}
}
