package agentruntime

import (
	"strings"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

const messageOriginMetadataKey = "messageOrigin"

type messageOrigin string

const (
	messageOriginCanonicalSubmit   messageOrigin = "canonical_submit"
	messageOriginProviderEcho      messageOrigin = "provider_echo"
	messageOriginHistoricalReplay  messageOrigin = "historical_replay"
	messageOriginGuidance          messageOrigin = "guidance"
	messageOriginProviderInitiated messageOrigin = "provider_initiated"
	messageOriginProviderActivity  messageOrigin = "provider_activity"
)

// messageOriginForSessionEvent is deliberately driven by an explicit producer
// marker. Role=user is not enough to identify a canonical submit: historical
// replay, guidance, and provider-initiated messages are all valid user events.
func messageOriginForSessionEvent(event activityshared.Event) messageOrigin {
	metadata := event.Payload.Metadata
	switch strings.TrimSpace(stringFromPayload(metadata, messageOriginMetadataKey)) {
	case string(messageOriginCanonicalSubmit):
		return messageOriginCanonicalSubmit
	case string(messageOriginProviderEcho):
		return messageOriginProviderEcho
	case string(messageOriginHistoricalReplay):
		return messageOriginHistoricalReplay
	case string(messageOriginGuidance):
		return messageOriginGuidance
	case string(messageOriginProviderInitiated):
		return messageOriginProviderInitiated
	}
	if metadata["guidance"] == true || metadata["steered"] == true {
		return messageOriginGuidance
	}
	if metadata["historicalReplay"] == true || metadata["replayed"] == true {
		return messageOriginHistoricalReplay
	}
	if metadata["providerInitiated"] == true {
		return messageOriginProviderInitiated
	}
	if event.Payload.Role == activityshared.MessageRoleUser {
		// Unknown user events remain activity instead of being treated as a
		// canonical submit. This preserves historical/provider-initiated input
		// while failing closed for canonical ownership.
		return messageOriginProviderInitiated
	}
	return messageOriginProviderActivity
}

func isProviderEchoMessageEvent(event activityshared.Event) bool {
	return event.Payload.Role == activityshared.MessageRoleUser &&
		messageOriginForSessionEvent(event) == messageOriginProviderEcho
}

// providerEchoTimelineItem keeps a provider echo observable without sending it
// through the canonical message writer. Its stable identity makes duplicate
// echoes idempotent at the activity layer.
func providerEchoTimelineItem(
	event activityshared.Event,
	sessionID string,
	timestamp int64,
) (agentsessionstore.WorkspaceAgentTimelineItem, bool) {
	if !isProviderEchoMessageEvent(event) || strings.TrimSpace(sessionID) == "" || timestamp <= 0 {
		return agentsessionstore.WorkspaceAgentTimelineItem{}, false
	}
	messageID := firstNonEmptyString(stringFromPayload(event.Payload.Metadata, "messageId"), event.EventID)
	if messageID == "" {
		return agentsessionstore.WorkspaceAgentTimelineItem{}, false
	}
	payload := clonePayload(event.Payload.Metadata)
	if payload == nil {
		payload = map[string]any{}
	}
	payload[messageOriginMetadataKey] = string(messageOriginProviderEcho)
	if event.Payload.Content != "" {
		if _, exists := payload["content"]; !exists {
			payload["content"] = event.Payload.Content
		}
		if _, exists := payload["text"]; !exists {
			payload["text"] = event.Payload.Content
		}
	}
	return agentsessionstore.WorkspaceAgentTimelineItem{
		AgentSessionID:   strings.TrimSpace(sessionID),
		TurnID:           strings.TrimSpace(event.Payload.TurnID),
		EventSource:      "runtime",
		EventID:          "provider-echo:" + messageID,
		ActorType:        "provider",
		ActorID:          strings.TrimSpace(string(event.Provider)),
		OccurredAtUnixMS: timestamp,
		CreatedAtUnixMS:  timestamp,
		ItemType:         "provider.echo",
		Role:             string(activityshared.MessageRoleUser),
		Status:           firstNonEmptyString(stringFromPayload(event.Payload.Metadata, "streamState"), event.Payload.Status),
		Payload:          payload,
	}, true
}
