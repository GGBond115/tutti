package agenthost

import (
	"context"
	"errors"
	"testing"
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

	host.observeStep(context.Background(), "message_send", "runtime_exec", "session-1", "claude", host.now(), cause)

	if len(observer.failures) != 1 {
		t.Fatalf("terminal failures = %d, want 1", len(observer.failures))
	}
	got := observer.failures[0]
	if got.Flow != "message_send" || got.FailureStage != "runtime_exec" || got.AgentSessionID != "session-1" {
		t.Fatalf("failure identity = %#v", got)
	}
	if got.ErrorCode != "provider_timeout" || got.ErrorMessage != "provider timed out after 30s" {
		t.Fatalf("failure payload = %#v", got)
	}
}

func TestObserveStepSkipsTerminalFailureOnSuccess(t *testing.T) {
	observer := &recordingTerminalFailureObserver{}
	host := New(Config{TerminalFailureObserver: observer})
	host.observeStep(context.Background(), "message_send", "runtime_exec", "session-1", "claude", host.now(), nil)
	if len(observer.failures) != 0 {
		t.Fatalf("terminal failures = %#v, want none", observer.failures)
	}
}
