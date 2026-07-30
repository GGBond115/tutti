package storesqlite

import (
	"context"
	"errors"
	"testing"
)

func TestRecoverProviderTurnBindingIsCASAndIdempotent(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	seedSettledTurnWithoutProviderBinding(t, store, "turn-1", 10)

	input := ProviderTurnBindingRecovery{
		WorkspaceID: "ws-recovery", AgentSessionID: "root", TurnID: "turn-1",
		ExpectedProviderSessionID: "provider-session", ProviderTurnID: "provider-turn-1",
		ProviderCheckpointMessageID: "checkpoint-1", OccurredAtUnixMS: 20,
	}
	result, err := store.RecoverProviderTurnBinding(ctx, input)
	if err != nil || !result.Changed ||
		result.Turn.RootProviderTurnID != "provider-turn-1" ||
		result.Turn.ProviderCheckpointMessageID != "checkpoint-1" {
		t.Fatalf("first recovery = %#v error=%v", result, err)
	}
	replay, err := store.RecoverProviderTurnBinding(ctx, input)
	if err != nil || replay.Changed ||
		replay.Turn.RootProviderTurnID != "provider-turn-1" {
		t.Fatalf("replayed recovery = %#v error=%v", replay, err)
	}
	conflict := input
	conflict.ProviderTurnID = "provider-turn-other"
	if _, err := store.RecoverProviderTurnBinding(ctx, conflict); !errors.Is(
		err,
		ErrProviderTurnBindingConflict,
	) {
		t.Fatalf("conflicting recovery error = %v", err)
	}
}

func TestRecoverProviderTurnBindingRejectsDuplicateProviderIdentity(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	seedSettledTurnWithoutProviderBinding(t, store, "turn-1", 10)
	seedSettledTurnWithoutProviderBinding(t, store, "turn-2", 30)
	first := ProviderTurnBindingRecovery{
		WorkspaceID: "ws-recovery", AgentSessionID: "root", TurnID: "turn-1",
		ExpectedProviderSessionID: "provider-session", ProviderTurnID: "provider-turn",
		OccurredAtUnixMS: 40,
	}
	if _, err := store.RecoverProviderTurnBinding(ctx, first); err != nil {
		t.Fatal(err)
	}
	first.TurnID = "turn-2"
	first.OccurredAtUnixMS = 50
	if _, err := store.RecoverProviderTurnBinding(ctx, first); !errors.Is(
		err,
		ErrProviderTurnBindingConflict,
	) {
		t.Fatalf("duplicate provider identity error = %v", err)
	}
}

func TestRecoverProviderTurnBindingRejectsDuplicateAcrossCanonicalSessions(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	seedSettledTurnWithoutProviderBindingInSession(
		t,
		store,
		"root-a",
		"turn-a",
		10,
	)
	seedSettledTurnWithoutProviderBindingInSession(
		t,
		store,
		"root-b",
		"turn-b",
		30,
	)
	input := ProviderTurnBindingRecovery{
		WorkspaceID: "ws-recovery", AgentSessionID: "root-a", TurnID: "turn-a",
		ExpectedProviderSessionID: "provider-session", ProviderTurnID: "provider-turn",
		OccurredAtUnixMS: 40,
	}
	if _, err := store.RecoverProviderTurnBinding(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.AgentSessionID = "root-b"
	input.TurnID = "turn-b"
	input.OccurredAtUnixMS = 50
	if _, err := store.RecoverProviderTurnBinding(ctx, input); !errors.Is(
		err,
		ErrProviderTurnBindingConflict,
	) {
		t.Fatalf("cross-session duplicate provider identity error = %v", err)
	}
}

func seedSettledTurnWithoutProviderBinding(
	t *testing.T,
	store *Store,
	turnID string,
	occurredAtUnixMS int64,
) {
	seedSettledTurnWithoutProviderBindingInSession(
		t,
		store,
		"root",
		turnID,
		occurredAtUnixMS,
	)
}

func seedSettledTurnWithoutProviderBindingInSession(
	t *testing.T,
	store *Store,
	sessionID string,
	turnID string,
	occurredAtUnixMS int64,
) {
	t.Helper()
	ctx := context.Background()
	reportSessionWithTurn(t, store, SessionStateReport{
		WorkspaceID: "ws-recovery", AgentSessionID: sessionID, Kind: SessionKindRoot,
		Provider: "codex", ProviderSessionID: "provider-session",
		OccurredAtUnixMS: occurredAtUnixMS,
	}, turnID, occurredAtUnixMS)
	if _, accepted, err := store.RecordTurnTransition(ctx, TurnTransition{
		WorkspaceID: "ws-recovery", AgentSessionID: sessionID, TurnID: turnID,
		Phase: TurnPhaseSettled, Outcome: TurnOutcomeCompleted,
		OccurredAtUnixMS: occurredAtUnixMS + 1,
	}); err != nil || !accepted {
		t.Fatalf("settle turn accepted=%v error=%v", accepted, err)
	}
}
