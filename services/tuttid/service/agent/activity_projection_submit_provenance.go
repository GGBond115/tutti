package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	canonical "github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

type submitIntentRepository interface {
	AdmitSubmitIntent(context.Context, agentactivitybiz.SubmitIntentAdmission) (agentactivitybiz.SubmitIntentAdmissionResult, error)
	UpdateSubmitProvenance(context.Context, agentactivitybiz.SubmitProvenanceUpdate) (agentactivitybiz.SubmitClaim, bool, error)
}

type canonicalAgentStoreProvider interface {
	AgentCanonicalStore() *agentactivitybiz.Store
}

func (p *ActivityProjection) submitIntentStore() (submitIntentRepository, error) {
	if p == nil || p.repo == nil {
		return nil, fmt.Errorf("agent activity repository is unavailable")
	}
	if store, ok := p.repo.(submitIntentRepository); ok {
		return store, nil
	}
	provider, ok := p.repo.(canonicalAgentStoreProvider)
	if !ok || provider.AgentCanonicalStore() == nil {
		return nil, fmt.Errorf("agent submit intent store is unavailable")
	}
	return provider.AgentCanonicalStore(), nil
}

// AdmitSubmitIntent is the only ActivityProjection operation that creates a
// canonical user message. The state, turn, claim, and message are committed
// by the store as one admission transaction.
func (p *ActivityProjection) AdmitSubmitIntent(
	ctx context.Context,
	input agentsessionstore.SubmitIntentInput,
) error {
	store, err := p.submitIntentStore()
	if err != nil {
		return err
	}
	stateInput := input.State
	messageInput := input.Messages
	stateOrigin, stateSource, err := normalizeReportSessionOrigins(stateInput.SessionOrigin, stateInput.Source)
	if err != nil {
		return err
	}
	messageOrigin, messageSource, err := normalizeReportSessionOrigins(messageInput.SessionOrigin, messageInput.Source)
	if err != nil {
		return err
	}
	if stateOrigin != messageOrigin || strings.TrimSpace(stateInput.WorkspaceID) != strings.TrimSpace(messageInput.WorkspaceID) ||
		strings.TrimSpace(stateInput.AgentSessionID) != strings.TrimSpace(messageInput.AgentSessionID) ||
		strings.TrimSpace(stateInput.WorkspaceID) != strings.TrimSpace(input.WorkspaceID) ||
		strings.TrimSpace(stateInput.AgentSessionID) != strings.TrimSpace(input.AgentSessionID) {
		return fmt.Errorf("submit intent state and message scopes do not match")
	}
	stateInput.SessionOrigin = stateOrigin
	stateInput.Source = stateSource
	messageInput.SessionOrigin = messageOrigin
	messageInput.Source = messageSource
	if len(messageInput.Updates) != 1 {
		return fmt.Errorf("submit intent admission requires exactly one canonical message")
	}
	update := messageInput.Updates[0]
	clientSubmitID := strings.TrimSpace(input.ClientSubmitID)
	canonicalTurnID := strings.TrimSpace(input.CanonicalTurnID)
	if clientSubmitID == "" || canonicalTurnID == "" || strings.TrimSpace(update.MessageID) == "" ||
		strings.TrimSpace(update.TurnID) != canonicalTurnID || payloadString(update.Payload, "clientSubmitId") != clientSubmitID {
		return fmt.Errorf("submit intent admission identity is incomplete or mismatched")
	}

	activityReport, canonicalTargetID, err := p.activityStateReport(ctx, stateInput)
	if err != nil {
		return err
	}
	if activityReport.Turn == nil || strings.TrimSpace(activityReport.Turn.TurnID) != canonicalTurnID {
		return fmt.Errorf("submit intent admission requires the claimed canonical turn")
	}
	activityReport.Messages = activityMessageUpdates(messageInput.Updates)
	now := input.CanonicalSubmitOccurredAtUnixMS
	if now <= 0 {
		now = update.OccurredAtUnixMS
	}
	if now <= 0 {
		now = stateInput.State.OccurredAtUnixMS
	}
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	result, err := store.AdmitSubmitIntent(ctx, agentactivitybiz.SubmitIntentAdmission{
		Claim: agentactivitybiz.SubmitClaimPrepare{
			WorkspaceID: strings.TrimSpace(input.WorkspaceID), AgentSessionID: strings.TrimSpace(input.AgentSessionID),
			ClientSubmitID: clientSubmitID, CanonicalTurnID: canonicalTurnID,
			CanonicalMessageID: strings.TrimSpace(firstNonEmptyString(input.CanonicalMessageID, update.MessageID)), NowUnixMS: now,
		},
		Activity: activityReport,
	})
	if err != nil {
		return err
	}
	if !result.Activity.State.Accepted || result.Activity.Messages.AcceptedCount != 1 || len(result.Activity.Messages.Messages) != 1 {
		return fmt.Errorf("submit intent admission was not fully accepted")
	}

	stateReply := canonical.ReportSessionStateReply{
		Accepted: result.Activity.State.Accepted, StateApplied: result.Activity.State.StateApplied,
		LastEventAtUnixMS: result.Activity.State.LastEventUnixMS, RequestBodyBytes: result.Activity.State.RequestBodyBytes,
	}
	provisional := activityStateIsProvisional(stateInput)
	if !provisional {
		p.publishPersistedTurnState(ctx, stateInput, result.Activity)
	}
	if provisional {
		p.observeSessionState(ctx, stateInput, stateReply)
		p.observeSessionMessages(ctx, messageInput, canonical.ReportSessionMessagesReply{
			AcceptedCount: result.Activity.Messages.AcceptedCount, LatestVersion: result.Activity.Messages.LatestVersion,
		})
		return nil
	}
	p.publishActivityUpdated(ctx, stateInput.WorkspaceID, stateInput.AgentSessionID, "session_reconcile_required",
		activitySessionUpdateEventPayload(stateInput.WorkspaceID, stateInput.AgentSessionID, result.Activity.State.LastEventUnixMS, canonicalTargetID))
	p.observeSessionState(ctx, stateInput, stateReply)
	publishedAgentSessionID := canonicalMessageUpdateSessionID(messageInput.AgentSessionID, result.Activity.Messages.Messages)
	p.publishActivityUpdated(ctx, messageInput.WorkspaceID, publishedAgentSessionID, "message_update", map[string]any{
		"acceptedCount": result.Activity.Messages.AcceptedCount, "agentSessionId": publishedAgentSessionID,
		"eventType": "message_update", "latestVersion": result.Activity.Messages.LatestVersion,
		"messages": activityMessagesEventPayload(result.Activity.Messages.Messages), "workspaceId": strings.TrimSpace(messageInput.WorkspaceID),
	})
	p.observeSessionMessages(ctx, messageInput, canonical.ReportSessionMessagesReply{
		AcceptedCount: result.Activity.Messages.AcceptedCount, LatestVersion: result.Activity.Messages.LatestVersion,
	})
	return nil
}

// UpdateSubmitProvenance only advances submit identity and delivery facts. It
// deliberately has no state or message payload and therefore cannot rewrite
// the canonical user message admitted above.
func (p *ActivityProjection) UpdateSubmitProvenance(
	ctx context.Context,
	input agentsessionstore.SubmitProvenanceInput,
) error {
	store, err := p.submitIntentStore()
	if err != nil {
		return err
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.AgentSessionID = strings.TrimSpace(input.AgentSessionID)
	input.ClientSubmitID = strings.TrimSpace(input.ClientSubmitID)
	input.CanonicalTurnID = strings.TrimSpace(input.CanonicalTurnID)
	if input.WorkspaceID == "" || input.AgentSessionID == "" || input.ClientSubmitID == "" || input.CanonicalTurnID == "" {
		return fmt.Errorf("submit provenance identity is incomplete")
	}
	if input.OccurredAtUnixMS <= 0 {
		input.OccurredAtUnixMS = time.Now().UnixMilli()
	}
	_, _, err = store.UpdateSubmitProvenance(ctx, agentactivitybiz.SubmitProvenanceUpdate{
		WorkspaceID: input.WorkspaceID, AgentSessionID: input.AgentSessionID, ClientSubmitID: input.ClientSubmitID,
		CanonicalTurnID: input.CanonicalTurnID, CanonicalMessageID: strings.TrimSpace(input.CanonicalMessageID),
		ProviderSessionID: strings.TrimSpace(input.ProviderSessionID), ProviderTurnID: strings.TrimSpace(input.ProviderTurnID),
		DispatchStatus: strings.TrimSpace(input.DispatchStatus), DeliveryStatus: strings.TrimSpace(input.DeliveryStatus),
		FailureReason: strings.TrimSpace(input.FailureReason), NowUnixMS: input.OccurredAtUnixMS,
	})
	return err
}

func (p *ActivityProjection) activityStateReport(
	ctx context.Context,
	input canonical.ReportSessionStateInput,
) (agentactivitybiz.ActivityStateReport, string, error) {
	canonicalTargetID, runtimeContext, runtimeContextPatch := p.canonicalizeAgentTargetState(
		ctx,
		input.WorkspaceID,
		firstNonEmptyString(input.State.AgentTargetID, input.Source.AgentTargetID),
		input.State.RuntimeContext,
		input.State.RuntimeContextPatch,
	)
	stateReport := agentactivitybiz.SessionStateReport{
		WorkspaceID:          strings.TrimSpace(input.WorkspaceID),
		AgentSessionID:       strings.TrimSpace(input.AgentSessionID),
		Kind:                 strings.TrimSpace(input.State.Kind),
		RootAgentSessionID:   strings.TrimSpace(input.State.RootAgentSessionID),
		RootTurnID:           strings.TrimSpace(input.State.RootTurnID),
		ParentAgentSessionID: strings.TrimSpace(input.State.ParentAgentSessionID),
		ParentTurnID:         strings.TrimSpace(input.State.ParentTurnID),
		ParentToolCallID:     strings.TrimSpace(input.State.ParentToolCallID),
		Origin:               strings.TrimSpace(input.SessionOrigin),
		UserID:               strings.TrimSpace(input.Source.UserID),
		AgentTargetID:        canonicalTargetID,
		Provider:             strings.TrimSpace(firstNonEmptyString(input.State.Provider, input.Source.Provider)),
		ProviderSessionID:    strings.TrimSpace(firstNonEmptyString(input.State.ProviderSessionID, input.Source.ProviderSessionID)),
		Model:                strings.TrimSpace(input.State.Model),
		Settings:             clonePayload(input.State.Settings),
		Capabilities:         canonical.CloneCapabilitySnapshot(input.State.Capabilities),
		RuntimeContext:       cloneOptionalPayload(runtimeContext),
		RuntimeContextPatch:  canonical.CloneRuntimeContextPatch(runtimeContextPatch),
		Cwd:                  strings.TrimSpace(input.State.CWD),
		Title:                strings.TrimSpace(sessionStateTitle(input.State)),
		Status:               strings.TrimSpace(input.State.LifecycleStatus),
		CurrentPhase:         strings.TrimSpace(input.State.CurrentPhase),
		LastError:            strings.TrimSpace(input.State.LastError),
		OccurredAtUnixMS:     input.State.OccurredAtUnixMS,
		StartedAtUnixMS:      input.State.StartedAtUnixMS,
		EndedAtUnixMS:        input.State.EndedAtUnixMS,
		CreatedAtUnixMS:      input.Source.SessionCreatedAtUnixMS,
	}
	activityReport := agentactivitybiz.ActivityStateReport{Session: stateReport}
	if transition, ok := turnTransitionFromStateInput(input); ok {
		activityReport.Turn = &transition
	}
	if transition, ok := rootProviderTurnTransitionFromStateInput(input); ok {
		activityReport.RootProviderTurn = &transition
	}
	interaction, err := interactionTransitionFromStateInput(input)
	if err != nil {
		return agentactivitybiz.ActivityStateReport{}, "", err
	}
	activityReport.Interaction = interaction
	return activityReport, canonicalTargetID, nil
}
