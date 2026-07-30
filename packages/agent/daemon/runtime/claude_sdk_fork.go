package agentruntime

import (
	"context"
	"errors"
	"strings"
)

const (
	claudeSDKForkDriverKind = "claude-agent-sdk-session-fork"
	// The official SDK forkSession API allocates the provider child identity.
	// Host therefore permits one dispatch and fails closed instead of replaying
	// an unknown result that could create a duplicate provider child.
	claudeSDKForkDriverVersion = "0.3.220/sidecar-v8-full-turn-bindings"
)

func (a *ClaudeCodeSDKAdapter) ForkCapabilities(
	_ context.Context,
	_ Session,
) (SessionForkCapabilities, error) {
	if a == nil || a.transport == nil {
		return SessionForkCapabilities{}, nil
	}
	return SessionForkCapabilities{
		DriverKind:       claudeSDKForkDriverKind,
		DriverVersion:    claudeSDKForkDriverVersion,
		StateBindingMode: "provider_owned",
		ThroughTurn:      true,
	}, nil
}

func (a *ClaudeCodeSDKAdapter) Fork(
	ctx context.Context,
	input SessionForkInput,
) (SessionForkResult, error) {
	result := SessionForkResult{
		ForkedFromProviderSessionID: strings.TrimSpace(input.Source.ProviderSessionID),
		ThroughProviderTurnID:       strings.TrimSpace(input.ProviderTurnID),
		DeliveryDisposition:         SessionForkDeliveryNotStarted,
	}
	if a == nil || a.transport == nil {
		return result, ErrSessionForkUnsupported
	}
	event, sent, err := a.statelessClaudeSDKForkRequest(
		ctx,
		input.Source,
		claudeSDKSidecarRequest{
			ID:   newID(),
			Type: "fork_session",
			Payload: map[string]any{
				"providerSessionId": strings.TrimSpace(input.Source.ProviderSessionID),
				"providerTurnId":    strings.TrimSpace(input.ProviderTurnID),
				"providerCheckpointMessageId": strings.TrimSpace(
					input.ProviderCheckpointMessageID,
				),
				"cwd":   strings.TrimSpace(input.Source.CWD),
				"title": strings.TrimSpace(input.TargetTitle),
			},
		},
	)
	if err != nil {
		if sent {
			result.DeliveryDisposition = SessionForkDeliveryUnknown
		}
		if eventDisposition := SessionForkDeliveryDisposition(
			payloadString(event.Payload, "deliveryDisposition"),
		); eventDisposition == SessionForkDeliveryNotStarted ||
			eventDisposition == SessionForkDeliveryUnknown {
			result.DeliveryDisposition = eventDisposition
		}
		return result, err
	}
	targetTurnBindings, ok := claudeSDKPayloadTurnBindings(
		event.Payload,
		"targetProviderTurnBindings",
	)
	if !ok {
		result.DeliveryDisposition = SessionForkDeliveryUnknown
		return result, errors.New(
			"claude SDK fork returned invalid target provider turn bindings",
		)
	}
	result.ProviderSessionID = payloadString(event.Payload, "providerSessionId")
	result.TargetProviderTurnBindings = targetTurnBindings
	result.StateBindingMode = payloadString(event.Payload, "stateBindingMode")
	result.StateBindingReceipt = payloadString(event.Payload, "stateBindingReceipt")
	result.DeliveryDisposition = SessionForkDeliveryDisposition(
		payloadString(event.Payload, "deliveryDisposition"),
	)
	if result.ProviderSessionID == "" ||
		result.ProviderSessionID == result.ForkedFromProviderSessionID ||
		len(result.TargetProviderTurnBindings) == 0 ||
		result.StateBindingMode != "provider_owned" ||
		result.StateBindingReceipt == "" ||
		result.DeliveryDisposition != SessionForkDeliveryAccepted {
		result.DeliveryDisposition = SessionForkDeliveryUnknown
		return result, errors.New("claude SDK fork returned incomplete verification evidence")
	}
	return result, nil
}

func (a *ClaudeCodeSDKAdapter) statelessClaudeSDKForkRequest(
	ctx context.Context,
	session Session,
	request claudeSDKSidecarRequest,
) (claudeSDKSidecarEvent, bool, error) {
	spec, cleanup, err := prepareProviderLaunch(ctx, a.preparer, session, ProcessSpec{
		Provider:       strings.TrimSpace(session.Provider),
		AgentSessionID: session.AgentSessionID,
		RoomID:         session.RoomID,
		CWD:            session.CWD,
		Command:        claudeSDKSidecarCommand(session.Env),
		Env:            claudeSDKSidecarEnv(session),
		DirectStart:    true,
	})
	if err != nil {
		return claudeSDKSidecarEvent{}, false, err
	}
	conn, err := a.transport.Start(ctx, spec)
	if err != nil {
		cleanupPreparedLaunch(cleanup)
		return claudeSDKSidecarEvent{}, false, err
	}
	conn = wrapProviderLaunchCleanup(conn, cleanup)
	defer conn.Close()
	adapterSession := &claudeSDKAdapterSession{
		conn:   conn,
		reader: &claudeSDKLineReader{conn: conn},
	}
	if err := adapterSession.send(request); err != nil {
		return claudeSDKSidecarEvent{}, false, err
	}
	event, err := adapterSession.roundTripDirectResponse(ctx, request)
	if err != nil {
		return event, true, err
	}
	return event, true, nil
}

func claudeSDKPayloadTurnBindings(
	payload map[string]any,
	key string,
) ([]SessionForkProviderTurnBinding, bool) {
	raw, exists := payload[key]
	if !exists {
		return nil, false
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	result := make([]SessionForkProviderTurnBinding, 0, len(values))
	seenProviderTurnIDs := make(map[string]struct{}, len(values))
	seenCheckpointMessageIDs := make(map[string]struct{}, len(values))
	for _, rawValue := range values {
		value, ok := rawValue.(map[string]any)
		if !ok {
			return nil, false
		}
		binding := SessionForkProviderTurnBinding{
			ProviderTurnID: strings.TrimSpace(
				payloadString(value, "providerTurnId"),
			),
			CheckpointMessageID: strings.TrimSpace(
				payloadString(value, "checkpointMessageId"),
			),
		}
		if binding.ProviderTurnID == "" || binding.CheckpointMessageID == "" {
			return nil, false
		}
		if _, duplicate := seenProviderTurnIDs[binding.ProviderTurnID]; duplicate {
			return nil, false
		}
		if _, duplicate := seenCheckpointMessageIDs[binding.CheckpointMessageID]; duplicate {
			return nil, false
		}
		seenProviderTurnIDs[binding.ProviderTurnID] = struct{}{}
		seenCheckpointMessageIDs[binding.CheckpointMessageID] = struct{}{}
		result = append(result, binding)
	}
	return result, len(result) > 0
}

var _ SessionForkAdapter = (*ClaudeCodeSDKAdapter)(nil)
