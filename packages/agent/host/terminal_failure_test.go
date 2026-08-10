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
