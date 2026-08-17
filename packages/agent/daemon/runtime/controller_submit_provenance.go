package agentruntime

import (
	"context"
	"errors"
	"strings"
	"time"
)

// UpdateSubmitProvenance records provider and delivery facts after Exec. It
// never reconstructs or submits a canonical user message.
func (c *Controller) UpdateSubmitProvenance(ctx context.Context, input SubmitProvenanceInput) error {
	if c == nil || c.reporter == nil {
		return errors.New("agent session activity reporter is unavailable")
	}
	input.RoomID = strings.TrimSpace(input.RoomID)
	input.AgentSessionID = strings.TrimSpace(input.AgentSessionID)
	input.TurnID = strings.TrimSpace(input.TurnID)
	input.ClientSubmitID = strings.TrimSpace(input.ClientSubmitID)
	if input.RoomID == "" || input.AgentSessionID == "" || input.TurnID == "" || input.ClientSubmitID == "" {
		return errors.New("workspace id, agent session id, turn id, and client submit id are required")
	}
	session, ok := c.get(input.RoomID, input.AgentSessionID)
	if !ok {
		return ErrSessionNotFound
	}
	if session.IsSideConversation() {
		return ErrSideConversationUnsupported
	}
	if input.OccurredAtUnixMS <= 0 {
		input.OccurredAtUnixMS = time.Now().UnixMilli()
	}
	request := reportRequest{
		ctx:              context.WithoutCancel(ctx),
		submitProvenance: true,
		provenance: &SubmitProvenanceInput{
			RoomID: input.RoomID, AgentSessionID: input.AgentSessionID,
			ClientSubmitID: input.ClientSubmitID, TurnID: input.TurnID,
			CanonicalMessageID: input.CanonicalMessageID, ProviderSessionID: input.ProviderSessionID,
			ProviderTurnID: input.ProviderTurnID, DispatchStatus: input.DispatchStatus,
			DeliveryStatus: input.DeliveryStatus, FailureReason: input.FailureReason,
			OccurredAtUnixMS: input.OccurredAtUnixMS, Guidance: input.Guidance,
		},
		done: make(chan error, 1),
	}
	if c.reportQueue == nil {
		return c.report(request.ctx, request)
	}
	c.reportQueue.enqueue(request)
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
