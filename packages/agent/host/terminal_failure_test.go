package agenthost

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

type recordingTerminalFailureObserver struct {
	failures []TerminalFailure
}

func (o *recordingTerminalFailureObserver) ObserveTerminalFailure(_ context.Context, failure TerminalFailure) {
	o.failures = append(o.failures, failure)
}

func TestObserveStepEmitsAggregatedTerminalFailure(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	host := New(Config{TerminalFailureObserver: observer})
	cause := NewProviderError("provider_timeout", "provider timed out after 30s", "debug", errors.New("deadline"))

	host.observeStep(context.Background(), "message_send", "runtime_exec", "workspace-1", "session-1", "claude", host.now(), cause)

	if len(observer.failures) != 1 {
		t.Fatalf("terminal failures = %d, want 1", len(observer.failures))
	}
	got := observer.failures[0]
	if got.Flow != "message_send" || got.FailureStage != "runtime_exec" || got.WorkspaceID != "workspace-1" || got.AgentSessionID != "session-1" {
		t.Fatalf("failure identity = %#v", got)
	}
	if got.ErrorCode != "provider_timeout" || got.ErrorMessage != "provider timed out after 30s" {
		t.Fatalf("failure payload = %#v", got)
	}
}

func TestObserveStepSkipsTerminalFailureOnSuccess(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	host := New(Config{TerminalFailureObserver: observer})
	host.observeStep(context.Background(), "message_send", "runtime_exec", "workspace-1", "session-1", "claude", host.now(), nil)
	if len(observer.failures) != 0 {
		t.Fatalf("terminal failures = %#v, want none", observer.failures)
	}
}

func TestTerminalFailuresFromDeltaCoversInteractivePlanToolAndTurn(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	delta := CommittedDelta{
		RuntimeOperation: &RuntimeOperationCommitted{
			Stage: RuntimeOperationFailed,
			Operation: storesqlite.RuntimeOperation{
				WorkspaceID: "ws-1", AgentSessionID: "session-1", OperationID: "op-interactive",
				Kind: storesqlite.RuntimeOperationKindInteractiveResponse, RequestID: "request-1", TurnID: "turn-1",
				LastError: "interactive submit rejected", Payload: map[string]any{"interactionKind": "plan"},
			},
		},
		GoalOperation: &GoalOperationCommitted{
			Stage: GoalOperationFailed,
			Operation: storesqlite.GoalControlOperation{
				WorkspaceID: "ws-1", AgentSessionID: "session-1", OperationID: "op-goal",
				ClientSubmitID: "goal-1", LastError: "goal runtime unavailable",
			},
		},
		RootTurnsSettled: []RootTurnSettled{{
			WorkspaceID: "ws-1", AgentSessionID: "session-1",
			Turn: storesqlite.Turn{
				TurnID: "turn-2", Outcome: storesqlite.TurnOutcomeFailed,
				ErrorCode: "provider_timeout", ErrorMessage: "turn timed out",
			},
		}},
		SessionMessages: &SessionMessagesCommitted{
			Input: canonical.ReportSessionMessagesInput{WorkspaceID: "ws-1", AgentSessionID: "session-1"},
			Result: storesqlite.MessageReportResult{
				Messages: []storesqlite.Message{{
					MessageID: "toolcall:1", AgentSessionID: "session-1", TurnID: "turn-2",
					Kind: "tool_call", Status: "failed",
					Payload: map[string]any{"toolName": "Bash", "errorMessage": "command exited 1"},
				}},
			},
		},
	}

	ObserveTerminalFailuresFromDelta(context.Background(), observer, delta)
	if len(observer.failures) != 4 {
		t.Fatalf("failures = %#v, want 4", observer.failures)
	}
	byFlow := map[string]TerminalFailure{}
	for _, failure := range observer.failures {
		byFlow[failure.Flow] = failure
	}
	if byFlow["interactive_response"].InteractionKind != "plan" || byFlow["interactive_response"].ErrorMessage != "interactive submit rejected" {
		t.Fatalf("interactive failure = %#v", byFlow["interactive_response"])
	}
	if byFlow["goal_control"].ClientSubmitID != "goal-1" || byFlow["goal_control"].ErrorMessage != "goal runtime unavailable" {
		t.Fatalf("goal failure = %#v", byFlow["goal_control"])
	}
	if byFlow["turn"].TurnID != "turn-2" || byFlow["turn"].ErrorCode != "provider_timeout" {
		t.Fatalf("turn failure = %#v", byFlow["turn"])
	}
	if byFlow["tool_call"].ToolNameFamily != "bash" || byFlow["tool_call"].ErrorMessage != "command exited 1" {
		t.Fatalf("tool failure = %#v", byFlow["tool_call"])
	}
}

func TestTerminalFailuresFromDeltaEmitsPlanDecisionFailures(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	ObserveTerminalFailuresFromDelta(context.Background(), observer, CommittedDelta{
		RuntimeOperation: &RuntimeOperationCommitted{
			Stage: RuntimeOperationFailed,
			Operation: storesqlite.RuntimeOperation{
				WorkspaceID: "ws-1", AgentSessionID: "session-1", OperationID: "op-plan",
				Kind: storesqlite.RuntimeOperationKindPlanDecision, TurnID: "turn-plan",
				LastError: "plan decision send failed",
			},
		},
	})
	if len(observer.failures) != 1 || observer.failures[0].Flow != "plan_decision" {
		t.Fatalf("failures = %#v", observer.failures)
	}
}

func TestTerminalFailuresFromDeltaEmitsEditRetryFailures(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	ObserveTerminalFailuresFromDelta(context.Background(), observer, CommittedDelta{
		RuntimeOperation: &RuntimeOperationCommitted{
			Stage: RuntimeOperationFailed,
			Operation: storesqlite.RuntimeOperation{
				WorkspaceID: "ws-1", AgentSessionID: "session-1", OperationID: "op-edit-retry",
				Kind: storesqlite.RuntimeOperationKindEditRetry, TurnID: "turn-edit",
				LastError: "edit retry disabled",
			},
		},
	})
	if len(observer.failures) != 1 || observer.failures[0].Flow != "edit_retry" {
		t.Fatalf("failures = %#v", observer.failures)
	}
}

func TestTerminalFailuresFromDeltaMarksChildSessionTurnAndTool(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	ObserveTerminalFailuresFromDelta(context.Background(), observer, CommittedDelta{
		ActivityState: &ActivityStateCommitted{
			Input: canonical.ReportSessionStateInput{
				WorkspaceID: "ws-1", AgentSessionID: "child-1",
				State: canonical.WorkspaceAgentSessionStateUpdate{
					Kind: storesqlite.SessionKindChild, ParentToolCallID: "call-1",
				},
			},
		},
		RootTurnsSettled: []RootTurnSettled{{
			WorkspaceID: "ws-1", AgentSessionID: "child-1", IsChildSession: true,
			Turn: storesqlite.Turn{
				TurnID: "turn-child", Outcome: storesqlite.TurnOutcomeFailed,
				ErrorMessage: "child turn failed",
			},
		}},
		SessionMessages: &SessionMessagesCommitted{
			Input: canonical.ReportSessionMessagesInput{WorkspaceID: "ws-1", AgentSessionID: "child-1"},
			Result: storesqlite.MessageReportResult{
				Messages: []storesqlite.Message{{
					MessageID: "toolcall:child", AgentSessionID: "child-1", TurnID: "turn-child",
					Kind: "tool_call", Status: "failed",
					Payload: map[string]any{"toolName": "Bash", "errorMessage": "child tool failed"},
				}},
			},
		},
	})
	if len(observer.failures) != 2 {
		t.Fatalf("failures = %#v, want 2", observer.failures)
	}
	for _, failure := range observer.failures {
		if !failure.IsChildSession {
			t.Fatalf("expected child session marker on %#v", failure)
		}
	}
}

func TestObserveGuidanceTargetFailureEmitsAggregatedTerminalFailure(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	host := New(Config{TerminalFailureObserver: observer})
	host.observeGuidanceTargetFailure(
		context.Background(),
		SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"},
		"codex", "turn-1", "guidance-1", host.now(), ErrActiveTurnTargetMismatch,
	)
	if len(observer.failures) != 1 {
		t.Fatalf("terminal failures = %#v, want 1", observer.failures)
	}
	got := observer.failures[0]
	if got.Flow != "guidance" || got.FailureStage != "guidance_target" || got.TurnID != "turn-1" {
		t.Fatalf("failure identity = %#v", got)
	}
	if got.ErrorCode != "active_turn_target_mismatch" || got.ClientSubmitID != "guidance-1" {
		t.Fatalf("failure payload = %#v", got)
	}
}
