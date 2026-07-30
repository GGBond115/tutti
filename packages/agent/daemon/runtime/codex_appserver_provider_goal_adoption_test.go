package agentruntime

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCodexProviderNativeGoalAdoptionProvesAutomaticContinuation(t *testing.T) {
	t.Parallel()

	adapter, transport, session := startedAppServerAdapter(t)
	adapter.goalProvenanceGraceWindow = 10 * time.Millisecond
	controller := NewController([]Adapter{adapter}, nil)
	adapter.SetGoalProvenanceDurableSink(&memoryGoalProvenanceLedger{
		bindings: make(map[string]GoalProvenanceBinding),
	})
	var (
		adoptionMu sync.Mutex
		requests   []ProviderGoalAdoptionRequest
	)
	controller.SetProviderGoalAdoptionSink(func(
		_ context.Context,
		gotSession Session,
		request ProviderGoalAdoptionRequest,
	) (GoalProvenanceBinding, error) {
		if gotSession.RoomID != session.RoomID ||
			gotSession.AgentSessionID != session.AgentSessionID ||
			gotSession.ProviderSessionID != session.ProviderSessionID {
			t.Errorf("adoption session = %#v", gotSession)
		}
		adoptionMu.Lock()
		requests = append(requests, request)
		adoptionMu.Unlock()
		return GoalProvenanceBinding{OperationID: "provider-goal-operation", Revision: 1}, nil
	})
	goal := map[string]any{
		"threadId": session.ProviderSessionID, "objective": "count from one to five",
		"status": "active", "createdAt": int64(100), "updatedAt": int64(101),
	}

	transport.conn.notify(appServerNotifyThreadGoalUpdated, map[string]any{
		"threadId": session.ProviderSessionID,
		"goal":     goal,
	})
	transport.conn.notify(appServerNotifyTurnStarted, map[string]any{
		"threadId": session.ProviderSessionID,
		"turn": map[string]any{
			"id": "provider-native-goal-turn", "status": "inProgress", "items": []any{},
		},
	})

	waitForCondition(t, func() bool {
		active := adapter.sessionActiveTurn(session.AgentSessionID)
		return active != nil &&
			adapter.sessionActiveTurnID(session.AgentSessionID) == "provider-native-goal-turn" &&
			active.goalIdentity.operationID == "provider-goal-operation" &&
			active.goalIdentity.revision == 1 &&
			active.goalProvenance == "ordered_goal_continuation_claim"
	})
	adoptionMu.Lock()
	if len(requests) != 1 ||
		requests[0].Fingerprint != codexGoalGenerationFingerprint(goal) ||
		asString(requests[0].Goal["objective"]) != "count from one to five" {
		t.Fatalf("adoption requests = %#v", requests)
	}
	adoptionMu.Unlock()
	if interrupts := appServerRequestParamsList(t, transport.conn, appServerMethodTurnInterrupt); len(interrupts) != 0 {
		t.Fatalf("provider-native Goal continuation was interrupted: %#v", interrupts)
	}
}

func TestCodexProviderNativeGoalAdoptionDoesNotBlockReadLoop(t *testing.T) {
	t.Parallel()

	adapter, transport, session := startedAppServerAdapter(t)
	adapter.goalProvenanceGraceWindow = 5 * time.Millisecond
	controller := NewController([]Adapter{adapter}, nil)
	adapter.SetGoalProvenanceDurableSink(&memoryGoalProvenanceLedger{
		bindings: make(map[string]GoalProvenanceBinding),
	})
	adoptionStarted := make(chan struct{})
	releaseAdoption := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseAdoption) }) })
	controller.SetProviderGoalAdoptionSink(func(
		ctx context.Context,
		_ Session,
		_ ProviderGoalAdoptionRequest,
	) (GoalProvenanceBinding, error) {
		close(adoptionStarted)
		select {
		case <-releaseAdoption:
			return GoalProvenanceBinding{OperationID: "provider-goal-slow", Revision: 1}, nil
		case <-ctx.Done():
			return GoalProvenanceBinding{}, ctx.Err()
		}
	})
	goal := map[string]any{
		"threadId": session.ProviderSessionID, "objective": "wait for durable adoption",
		"status": "active", "createdAt": int64(200), "updatedAt": int64(201),
	}

	transport.conn.notify(appServerNotifyThreadGoalUpdated, map[string]any{
		"threadId": session.ProviderSessionID,
		"goal":     goal,
	})
	select {
	case <-adoptionStarted:
	case <-time.After(time.Second):
		t.Fatal("provider Goal adoption did not start")
	}
	transport.conn.notify(appServerNotifyTurnStarted, map[string]any{
		"threadId": session.ProviderSessionID,
		"turn": map[string]any{
			"id": "provider-native-goal-slow-turn", "status": "inProgress", "items": []any{},
		},
	})
	waitForCondition(t, func() bool {
		adapter.mu.Lock()
		defer adapter.mu.Unlock()
		appSession := adapter.sessions[session.AgentSessionID]
		return appSession != nil && appSession.pendingGoalTurns["provider-native-goal-slow-turn"] != nil
	})
	time.Sleep(3 * adapter.goalProvenanceGraceWindow)
	if interrupts := appServerRequestParamsList(t, transport.conn, appServerMethodTurnInterrupt); len(interrupts) != 0 {
		t.Fatalf("in-flight adoption allowed pending turn to expire: %#v", interrupts)
	}

	releaseOnce.Do(func() { close(releaseAdoption) })
	waitForCondition(t, func() bool {
		active := adapter.sessionActiveTurn(session.AgentSessionID)
		return active != nil &&
			adapter.sessionActiveTurnID(session.AgentSessionID) == "provider-native-goal-slow-turn" &&
			active.goalIdentity.operationID == "provider-goal-slow"
	})
}

func TestCodexProviderNativeGoalAdoptionAdvancesAfterTerminalGeneration(t *testing.T) {
	t.Parallel()

	adapter, transport, session := startedAppServerAdapter(t)
	adapter.goalProvenanceGraceWindow = 10 * time.Millisecond
	adapter.goalContinuationGraceWindow = time.Hour
	controller := NewController([]Adapter{adapter}, nil)
	adapter.SetGoalProvenanceDurableSink(&memoryGoalProvenanceLedger{
		bindings: make(map[string]GoalProvenanceBinding),
	})
	var (
		adoptionMu sync.Mutex
		requests   []ProviderGoalAdoptionRequest
	)
	controller.SetProviderGoalAdoptionSink(func(
		_ context.Context,
		_ Session,
		request ProviderGoalAdoptionRequest,
	) (GoalProvenanceBinding, error) {
		adoptionMu.Lock()
		defer adoptionMu.Unlock()
		requests = append(requests, request)
		revision := int64(len(requests))
		return GoalProvenanceBinding{
			OperationID: "provider-goal-operation-" + asString(request.Goal["objective"]),
			Revision:    revision,
		}, nil
	})
	firstGoal := map[string]any{
		"threadId": session.ProviderSessionID, "objective": "first",
		"status": "active", "createdAt": int64(300), "updatedAt": int64(301),
	}
	transport.conn.notify(appServerNotifyThreadGoalUpdated, map[string]any{
		"threadId": session.ProviderSessionID, "goal": firstGoal,
	})
	transport.conn.notify(appServerNotifyTurnStarted, map[string]any{
		"threadId": session.ProviderSessionID,
		"turn": map[string]any{
			"id": "provider-native-goal-first-turn", "status": "inProgress", "items": []any{},
		},
	})
	waitForCondition(t, func() bool {
		active := adapter.sessionActiveTurn(session.AgentSessionID)
		return active != nil && active.goalIdentity.revision == 1
	})
	transport.conn.notify(appServerNotifyTurnCompleted, map[string]any{
		"threadId": session.ProviderSessionID,
		"turn": map[string]any{
			"id": "provider-native-goal-first-turn", "status": "completed", "items": []any{},
		},
	})
	waitForCondition(t, func() bool {
		return adapter.sessionActiveTurn(session.AgentSessionID) == nil
	})
	firstGoal["status"] = "complete"
	transport.conn.notify(appServerNotifyThreadGoalUpdated, map[string]any{
		"threadId": session.ProviderSessionID, "goal": firstGoal,
	})

	secondGoal := map[string]any{
		"threadId": session.ProviderSessionID, "objective": "second",
		"status": "active", "createdAt": int64(400), "updatedAt": int64(401),
	}
	transport.conn.notify(appServerNotifyThreadGoalUpdated, map[string]any{
		"threadId": session.ProviderSessionID, "goal": secondGoal,
	})
	transport.conn.notify(appServerNotifyTurnStarted, map[string]any{
		"threadId": session.ProviderSessionID,
		"turn": map[string]any{
			"id": "provider-native-goal-second-turn", "status": "inProgress", "items": []any{},
		},
	})
	waitForCondition(t, func() bool {
		active := adapter.sessionActiveTurn(session.AgentSessionID)
		return active != nil &&
			adapter.sessionActiveTurnID(session.AgentSessionID) == "provider-native-goal-second-turn" &&
			active.goalIdentity.operationID == "provider-goal-operation-second" &&
			active.goalIdentity.revision == 2
	})
	adoptionMu.Lock()
	defer adoptionMu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider Goal adoption requests = %#v", requests)
	}
	if interrupts := appServerRequestParamsList(t, transport.conn, appServerMethodTurnInterrupt); len(interrupts) != 0 {
		t.Fatalf("later provider-native Goal continuation was interrupted: %#v", interrupts)
	}
}
