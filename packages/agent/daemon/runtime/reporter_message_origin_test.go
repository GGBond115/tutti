package agentruntime

import (
	"testing"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestReportActivityInputRoutesProviderEchoAwayFromCanonicalMessage(t *testing.T) {
	t.Parallel()

	session := reportTestSession()
	canonical := originTestMessageEvent(
		session,
		"canonical-submit-event",
		messageOriginCanonicalSubmit,
		"client-submit:user:submit-1",
		"submit-1",
		"canonical content",
	)
	echo := originTestMessageEvent(
		session,
		"provider-echo-event",
		messageOriginProviderEcho,
		"client-submit:user:submit-1",
		"submit-1",
		"conflicting provider content",
	)

	for _, events := range [][]activityshared.Event{
		{canonical, echo, echo},
		{echo, canonical, echo},
	} {
		report := reportActivityInput(session, events)
		if len(report.MessageUpdates) != 1 {
			t.Fatalf("message updates = %#v, want one canonical message", report.MessageUpdates)
		}
		message := report.MessageUpdates[0]
		if message.MessageID != "client-submit:user:submit-1" || message.Payload["content"] != "canonical content" ||
			message.Payload[messageOriginMetadataKey] != string(messageOriginCanonicalSubmit) {
			t.Fatalf("canonical message = %#v", message)
		}
		if len(report.TimelineItems) != 1 || report.TimelineItems[0].ItemType != "provider.echo" ||
			report.TimelineItems[0].EventID != "provider-echo:client-submit:user:submit-1" {
			t.Fatalf("provider echo activity = %#v", report.TimelineItems)
		}
		if report.TimelineItems[0].Payload["content"] != "conflicting provider content" {
			t.Fatalf("provider echo payload = %#v, want conflicting activity content", report.TimelineItems[0].Payload)
		}
	}
}

func TestRealtimeProjectionKeepsProviderEchoBeforeToolCall(t *testing.T) {
	t.Parallel()

	session := reportTestSession()
	echo := originTestMessageEvent(
		session,
		"provider-echo-event",
		messageOriginProviderEcho,
		"client-submit:user:submit-1",
		"submit-1",
		"provider echo",
	)
	tool := newTurnActivityEvent(session, EventCallStarted, "turn-1", messageStreamStateStreaming, "", "Read files", map[string]any{
		"callId":   "tool-1",
		"callType": "tool",
		"name":     "Read files",
	})

	stream := ProjectActivityEventsToStreamEvents(session, []activityshared.Event{echo, tool})
	if len(stream) < 2 {
		t.Fatalf("stream = %#v, want provider echo followed by tool call", stream)
	}
	echoUpdate, ok := stream[0].Data.(agentsessionstore.WorkspaceAgentMessageUpdate)
	if !ok || stream[0].EventType != StreamEventMessageUpdate {
		t.Fatalf("first stream event = %#v, want realtime provider echo message update", stream[0])
	}
	if echoUpdate.Role != RoleUser || echoUpdate.Payload[messageOriginMetadataKey] != string(messageOriginProviderEcho) ||
		echoUpdate.Payload["clientSubmitId"] != "submit-1" || echoUpdate.Payload["content"] != "provider echo" {
		t.Fatalf("provider echo stream update = %#v", echoUpdate)
	}
	toolUpdate, ok := stream[1].Data.(agentsessionstore.WorkspaceAgentMessageUpdate)
	if !ok || stream[1].EventType != StreamEventMessageUpdate || toolUpdate.Kind != "tool_call" {
		t.Fatalf("second stream event = %#v, want tool call after provider echo", stream[1])
	}
}

func TestReportActivityInputPreservesNonCanonicalUserOrigins(t *testing.T) {
	t.Parallel()

	session := reportTestSession()
	events := []activityshared.Event{
		originTestMessageEvent(session, "history-1", messageOriginHistoricalReplay, "history-1", "", "historical user message"),
		originTestMessageEvent(session, "guidance-1", messageOriginGuidance, "guidance-1", "guidance-submit", "guidance message"),
		originTestMessageEvent(session, "provider-1", messageOriginProviderInitiated, "provider-1", "", "provider initiated message"),
	}

	report := reportActivityInput(session, events)
	if len(report.MessageUpdates) != len(events) {
		t.Fatalf("message updates = %#v, want all non-echo user messages", report.MessageUpdates)
	}
	for index, wantOrigin := range []messageOrigin{
		messageOriginHistoricalReplay,
		messageOriginGuidance,
		messageOriginProviderInitiated,
	} {
		if got := report.MessageUpdates[index].Payload[messageOriginMetadataKey]; got != string(wantOrigin) {
			t.Fatalf("message %d origin = %#v, want %q", index, got, wantOrigin)
		}
	}
}

func TestRealtimeProjectionPreservesNonCanonicalUserOrigins(t *testing.T) {
	t.Parallel()

	session := reportTestSession()
	events := []activityshared.Event{
		originTestMessageEvent(session, "history-1", messageOriginHistoricalReplay, "history-1", "", "historical user message"),
		originTestMessageEvent(session, "guidance-1", messageOriginGuidance, "guidance-1", "guidance-submit", "guidance message"),
		originTestMessageEvent(session, "provider-1", messageOriginProviderInitiated, "provider-1", "", "provider initiated message"),
	}

	stream := ProjectActivityEventsToStreamEvents(session, events)
	if len(stream) != len(events) {
		t.Fatalf("stream = %#v, want one message update per non-echo origin", stream)
	}
	for index, wantOrigin := range []messageOrigin{
		messageOriginHistoricalReplay,
		messageOriginGuidance,
		messageOriginProviderInitiated,
	} {
		update, ok := stream[index].Data.(agentsessionstore.WorkspaceAgentMessageUpdate)
		if !ok || stream[index].EventType != StreamEventMessageUpdate {
			t.Fatalf("stream event %d = %#v, want message update", index, stream[index])
		}
		if got := update.Payload[messageOriginMetadataKey]; got != string(wantOrigin) {
			t.Fatalf("stream message %d origin = %#v, want %q", index, got, wantOrigin)
		}
	}
}

func originTestMessageEvent(
	session Session,
	eventID string,
	origin messageOrigin,
	messageID string,
	clientSubmitID string,
	content string,
) activityshared.Event {
	metadata := map[string]any{
		messageOriginMetadataKey: string(origin),
		"messageId":              messageID,
	}
	if clientSubmitID != "" {
		metadata["clientSubmitId"] = clientSubmitID
	}
	return newTurnActivityEventWithID(
		session,
		eventID,
		EventMessage,
		"turn-1",
		messageStreamStateCompleted,
		RoleUser,
		content,
		metadata,
	)
}
