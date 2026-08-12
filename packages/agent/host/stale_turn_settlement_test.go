package agenthost

import (
	"context"
	"testing"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

type recordingStaleTurnFailureObserver struct {
	failures []TerminalFailure
}

func (o *recordingStaleTurnFailureObserver) ObserveTerminalFailure(_ context.Context, failure TerminalFailure) {
	o.failures = append(o.failures, failure)
}

func TestObserveTerminalFailuresFromStaleTurnSettlementDelta(t *testing.T) {
	delta := StaleTurnSettlementDelta([]storesqlite.StaleTurnSettlement{
		{WorkspaceID: "ws-1", AgentSessionID: "session-1", TurnID: "turn-stale"},
		{WorkspaceID: "ws-1", AgentSessionID: "session-2", TurnID: ""},
	})
	if len(delta.RootTurnsSettled) != 1 {
		t.Fatalf("root turns settled = %#v, want only the settlement carrying a turn", delta.RootTurnsSettled)
	}
	settled := delta.RootTurnsSettled[0]
	if settled.Turn.Phase != storesqlite.TurnPhaseSettled || settled.Turn.Outcome != storesqlite.TurnOutcomeInterrupted {
		t.Fatalf("settled turn = %#v, want settled phase with interrupted outcome", settled.Turn)
	}
	if !settled.StartupReconciled {
		t.Fatalf("settled turn = %#v, want the startup reconciliation marker", settled)
	}

	observer := &recordingStaleTurnFailureObserver{}
	ObserveTerminalFailuresFromDelta(context.Background(), observer, delta)
	if len(observer.failures) != 1 {
		t.Fatalf("terminal failures = %#v, want 1", observer.failures)
	}
	got := observer.failures[0]
	if got.Flow != "turn" || got.FailureStage != "settled" {
		t.Fatalf("failure identity = %#v", got)
	}
	if got.WorkspaceID != "ws-1" || got.AgentSessionID != "session-1" || got.TurnID != "turn-stale" {
		t.Fatalf("failure session identity = %#v", got)
	}
	if got.ErrorMessage != "stale turn settled on daemon startup" {
		t.Fatalf("failure message = %q", got.ErrorMessage)
	}
	if got.TurnOutcome != storesqlite.TurnOutcomeInterrupted || !got.StartupReconciled {
		t.Fatalf("failure settlement metadata = %#v", got)
	}
}

func TestStaleTurnSettlementDeltaKeepsChildSessionIdentityFromCallSite(t *testing.T) {
	delta := StaleTurnSettlementDelta([]storesqlite.StaleTurnSettlement{
		{WorkspaceID: "ws-1", AgentSessionID: "child-1", TurnID: "turn-stale"},
	})
	if len(delta.RootTurnsSettled) != 1 || delta.RootTurnsSettled[0].IsChildSession {
		t.Fatalf("root turns settled = %#v, want child identity left to the call site", delta.RootTurnsSettled)
	}
	delta.RootTurnsSettled[0].IsChildSession = true

	observer := &recordingStaleTurnFailureObserver{}
	ObserveTerminalFailuresFromDelta(context.Background(), observer, delta)
	if len(observer.failures) != 1 || !observer.failures[0].IsChildSession {
		t.Fatalf("terminal failures = %#v, want one child-session turn failure", observer.failures)
	}
}
