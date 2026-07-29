package agentruntime

import (
	"strings"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func claudeSDKSidecarContinuationEvents(
	adapterSession *claudeSDKAdapterSession,
	session Session,
	rootTurnID string,
	event claudeSDKSidecarEvent,
) ([]activityshared.Event, bool) {
	switch event.Type {
	case "turn_waiting":
		return []activityshared.Event{newTurnActivityEvent(
			session,
			EventTurnUpdated,
			rootTurnID,
			SessionStatusWaiting,
			"",
			"",
			map[string]any{
				"phase":  string(activityshared.TurnPhaseWaiting),
				"reason": payloadString(event.Payload, "reason"),
			},
		), claudeSDKProviderContinuationActivityEvent(
			session,
			rootTurnID,
			false,
		)}, true
	case "turn_running":
		return []activityshared.Event{newTurnActivityEvent(
			session,
			EventTurnUpdated,
			rootTurnID,
			SessionStatusWorking,
			"",
			"",
			map[string]any{
				"phase":  string(activityshared.TurnPhaseRunning),
				"reason": payloadString(event.Payload, "reason"),
			},
		), claudeSDKProviderContinuationActivityEvent(
			session,
			rootTurnID,
			true,
		)}, true
	case "background_tasks_changed", "continuation_delayed":
		return nil, true
	case "background_tasks_quiesced":
		if payloadInt64(event.Payload, "runningCount") != 0 {
			return nil, true
		}
		return adapterSession.endUnresolvedClaudeSDKBackgroundChildren(session), true
	default:
		return nil, false
	}
}

func claudeSDKProviderContinuationActivityEvent(
	session Session,
	turnID string,
	completed bool,
) activityshared.Event {
	activityKey := "provider-continuation:" + strings.TrimSpace(turnID)
	ctx, ok := activityEventContext(
		session,
		"claude-sdk:"+activityKey+":"+newID(),
		turnID,
	)
	if !ok {
		return activityshared.Event{}
	}
	metadata := map[string]any{
		"kind":   "provider_continuation",
		"status": string(activityshared.ActivityStatusRunning),
	}
	if completed {
		metadata["status"] = string(activityshared.ActivityStatusCompleted)
		return activityshared.NewActivityCompleted(ctx, activityKey, metadata)
	}
	return activityshared.NewActivityStarted(ctx, activityKey, metadata)
}
