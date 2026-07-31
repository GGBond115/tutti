package agentruntime

import (
	"context"
	"encoding/json"
	"testing"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func TestStandardACPConfigOptionUpdateSignalsSessionStateReload(t *testing.T) {
	t.Parallel()

	session := standardTestSession(ProviderOpenCode)
	session.ProviderSessionID = "opencode-session-1"

	events := standardACPUpdateEvents(standardACPConfig{provider: ProviderOpenCode}, session, "turn-1", json.RawMessage(`{
		"update": {
			"sessionUpdate": "config_option_update",
			"key": "model",
			"value": "opus"
		}
	}`), newACPTurnNormalizer())

	if len(events) != 1 {
		t.Fatalf("events = %#v, want one session update signal", events)
	}
	if events[0].Type != activityshared.EventSessionUpdated {
		t.Fatalf("event type = %q, want session updated", events[0].Type)
	}
	if got := events[0].Payload.Metadata["sessionUpdateKind"]; got != "config_option_update" {
		t.Fatalf("metadata sessionUpdateKind = %#v, want config_option_update", got)
	}
	if got := events[0].Payload.Metadata["configOptionKey"]; got != "model" {
		t.Fatalf("metadata configOptionKey = %#v, want model", got)
	}
}

func TestStandardACPIgnoresForeignProviderSessionUpdateDuringTurn(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("OpenCode", "opencode-session-current")
	adapter := newOpenCodeTestAdapter(transport)
	session := standardTestSession(ProviderOpenCode)
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.ProviderSessionID = "opencode-session-current"

	var commandSnapshots []AgentSessionCommandSnapshot
	var emittedEvents [][]activityshared.Event
	var configUpdates []AgentSessionConfigOptionsUpdate
	adapter.SetCommandSnapshotSink(func(snapshot AgentSessionCommandSnapshot) {
		commandSnapshots = append(commandSnapshots, snapshot)
	})
	adapter.SetSessionEventSink(func(_ string, events []activityshared.Event) {
		emittedEvents = append(emittedEvents, events)
	})
	adapter.SetConfigOptionsUpdateSink(func(update AgentSessionConfigOptionsUpdate) {
		configUpdates = append(configUpdates, update)
	})

	events, err := adapter.handleACPMessage(context.Background(), nil, session, "turn-foreign", acpMessage{
		Method: acpMethodUpdate,
		Params: json.RawMessage(`{
			"sessionId": "opencode-session-foreign",
			"update": {
				"sessionUpdate": "session_info_update",
				"title": "Foreign title"
			}
		}`),
	}, newACPTurnNormalizer(), nil, nil)
	if err != nil {
		t.Fatalf("handle foreign title update: %v", err)
	}
	if len(events) != 0 || len(emittedEvents) != 0 {
		t.Fatalf("foreign title events = %#v emitted=%#v, want none", events, emittedEvents)
	}

	if _, err := adapter.handleACPMessage(context.Background(), nil, session, "turn-foreign", acpMessage{
		Method: acpMethodUpdate,
		Params: json.RawMessage(`{
			"sessionId": "opencode-session-foreign",
			"update": {
				"sessionUpdate": "available_commands_update",
				"availableCommands": [{
					"name": "foreign-web",
					"description": "Foreign command"
				}]
			}
		}`),
	}, newACPTurnNormalizer(), nil, nil); err != nil {
		t.Fatalf("handle foreign command update: %v", err)
	}
	if _, err := adapter.handleACPMessage(context.Background(), nil, session, "turn-foreign", acpMessage{
		Method: acpMethodUpdate,
		Params: json.RawMessage(`{
			"sessionId": "opencode-session-foreign",
			"update": {
				"sessionUpdate": "config_option_update",
				"key": "model",
				"value": "foreign-model"
			}
		}`),
	}, newACPTurnNormalizer(), nil, nil); err != nil {
		t.Fatalf("handle foreign config update: %v", err)
	}
	if len(commandSnapshots) != 0 {
		t.Fatalf("foreign command snapshots = %#v, want none", commandSnapshots)
	}
	if len(configUpdates) != 0 {
		t.Fatalf("foreign config updates = %#v, want none", configUpdates)
	}

	snapshot, ok := adapter.SessionCommandSnapshot(session)
	if ok {
		if names := agentSessionCommandNames(snapshot.Commands); containsString(names, "foreign-web") {
			t.Fatalf("command names = %#v, want foreign command filtered", names)
		}
	}
	state := adapter.SessionState(session)
	config := payloadObject(state.RuntimeContext["config"])
	if got := asString(config["model"]); got == "foreign-model" {
		t.Fatalf("runtime config model = %q, want foreign config filtered", got)
	}
}

func TestStandardACPAcceptsMatchingProviderSessionUpdate(t *testing.T) {
	t.Parallel()

	transport := newStandardACPTransport("OpenCode", "opencode-session-current")
	adapter := newOpenCodeTestAdapter(transport)
	session := standardTestSession(ProviderOpenCode)
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatalf("Start: %v", err)
	}
	session.ProviderSessionID = "opencode-session-current"

	events, err := adapter.handleACPMessage(context.Background(), nil, session, "turn-current", acpMessage{
		Method: acpMethodUpdate,
		Params: json.RawMessage(`{
			"sessionId": "opencode-session-current",
			"update": {
				"sessionUpdate": "session_info_update",
				"title": "Current title"
			}
		}`),
	}, newACPTurnNormalizer(), nil, nil)
	if err != nil {
		t.Fatalf("handle matching update: %v", err)
	}
	if len(events) != 1 || events[0].Type != activityshared.EventSessionUpdated {
		t.Fatalf("events = %#v, want matching session update projected", events)
	}
	if got := events[0].Payload.Title; got != "Current title" {
		t.Fatalf("title = %q, want Current title", got)
	}
}
