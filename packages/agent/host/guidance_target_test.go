package agenthost_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

type recordingGuidanceTerminalFailureObserver struct {
	failures []agenthost.TerminalFailure
}

type guidanceDeleteFailureStore struct {
	agenthost.CanonicalStore
	err error
}

func (s guidanceDeleteFailureStore) DeleteSubmitClaim(context.Context, string, string, string) (bool, error) {
	return false, s.err
}

type guidanceDeleteAlreadyAbsentStore struct {
	agenthost.CanonicalStore
}

type guidanceAcceptFailureStore struct {
	agenthost.CanonicalStore
	err error
}

type guidancePrepareSignalStore struct {
	agenthost.CanonicalStore
	prepared chan struct{}
	once     sync.Once
}

type guidanceMutableEffectiveHistory struct {
	agenthost.EffectiveHistoryStore
	mu      sync.Mutex
	history storesqlite.SessionHistory
}

func (s *guidanceMutableEffectiveHistory) GetSessionHistory(
	context.Context,
	string,
	string,
) (storesqlite.SessionHistory, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.history, true, nil
}

func (s *guidanceMutableEffectiveHistory) setRecoveryState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history.RecoveryState = state
}

func (s guidanceAcceptFailureStore) AcceptSubmitClaim(
	context.Context,
	string,
	string,
	string,
	string,
	int64,
) (storesqlite.SubmitClaim, bool, error) {
	return storesqlite.SubmitClaim{}, false, s.err
}

func (s *guidancePrepareSignalStore) PrepareSubmitClaim(
	ctx context.Context,
	input storesqlite.SubmitClaimPrepare,
) (storesqlite.SubmitClaim, bool, error) {
	claim, created, err := s.CanonicalStore.PrepareSubmitClaim(ctx, input)
	if created && err == nil {
		s.once.Do(func() { close(s.prepared) })
	}
	return claim, created, err
}

func (s guidanceDeleteAlreadyAbsentStore) DeleteSubmitClaim(
	ctx context.Context,
	workspaceID string,
	agentSessionID string,
	clientSubmitID string,
) (bool, error) {
	if _, err := s.CanonicalStore.DeleteSubmitClaim(ctx, workspaceID, agentSessionID, clientSubmitID); err != nil {
		return false, err
	}
	// Model an idempotent cleanup retry whose first attempt committed but its
	// acknowledgement was lost. The durable postcondition is already true.
	return false, nil
}

func (o *recordingGuidanceTerminalFailureObserver) ObserveTerminalFailure(_ context.Context, failure agenthost.TerminalFailure) {
	o.failures = append(o.failures, failure)
}

func TestHostGuidanceRequiresExactTargetBeforeCreatingClaim(t *testing.T) {
	observer := &recordingGuidanceTerminalFailureObserver{}
	_, store, runtime := newHostEditRetryFixture(t)
	host := agenthost.New(agenthost.Config{
		CanonicalStore:          sqliteCanonicalStore{Store: store},
		TurnSubmissions:         store,
		EffectiveHistory:        store,
		RuntimeOperations:       store,
		Runtime:                 runtime,
		HistoryRuntime:          runtime,
		GoalRuntime:             runtime,
		OperationOwner:          "worker-1",
		TerminalFailureObserver: observer,
	})
	result, err := host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}, agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "missing target"}}, Guidance: true,
		ClientSubmitID: "guidance-required",
	})
	if err != agenthost.ErrActiveTurnTargetRequired {
		t.Fatalf("SendInput() error = %v, want ErrActiveTurnTargetRequired", err)
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		t.Fatalf("guidance disposition = %q, want precondition failed", result.GuidanceDisposition)
	}
	if _, found, claimErr := store.GetSubmitClaim(t.Context(), "workspace-1", "session-1", "guidance-required"); claimErr != nil || found {
		t.Fatalf("guidance claim found=%v error=%v, want no claim", found, claimErr)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.execCalls != 0 {
		t.Fatalf("runtime exec calls = %d, want 0", runtime.execCalls)
	}
	if len(observer.failures) != 1 {
		t.Fatalf("terminal failures = %#v, want 1", observer.failures)
	}
	got := observer.failures[0]
	if got.Flow != "guidance" || got.FailureStage != "guidance_target" || got.ErrorCode != "active_turn_target_required" {
		t.Fatalf("guidance terminal failure = %#v", got)
	}
}

func TestHostGuidanceTargetMismatchCleansPreparedClaim(t *testing.T) {
	observer := &recordingGuidanceTerminalFailureObserver{}
	_, store, runtime := newHostEditRetryFixture(t)
	host := agenthost.New(agenthost.Config{
		CanonicalStore:          sqliteCanonicalStore{Store: store},
		TurnSubmissions:         store,
		EffectiveHistory:        store,
		RuntimeOperations:       store,
		Runtime:                 runtime,
		HistoryRuntime:          runtime,
		GoalRuntime:             runtime,
		OperationOwner:          "worker-1",
		TerminalFailureObserver: observer,
	})
	runtime.mu.Lock()
	runtime.guidanceMismatch = true
	runtime.mu.Unlock()
	result, err := host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}, agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "stale guidance"}}, Guidance: true,
		TurnID: "turn-original", ClientSubmitID: "guidance-mismatch",
	})
	if err == nil {
		t.Fatal("SendInput() error = nil, want target mismatch")
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionTargetInactive {
		t.Fatalf("guidance disposition = %q, want target inactive", result.GuidanceDisposition)
	}
	if _, found, claimErr := store.GetSubmitClaim(t.Context(), "workspace-1", "session-1", "guidance-mismatch"); claimErr != nil || found {
		t.Fatalf("prepared claim found=%v error=%v, want cleanup", found, claimErr)
	}
	claim, created, claimErr := store.PrepareSubmitClaim(t.Context(), storesqlite.SubmitClaimPrepare{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", ClientSubmitID: "guidance-mismatch",
		CanonicalTurnID: "turn-retry", NowUnixMS: 2,
	})
	if claimErr != nil || !created || claim.CanonicalTurnID != "turn-retry" {
		t.Fatalf("retry claim=%#v created=%v error=%v, want a fresh claim", claim, created, claimErr)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.execCalls != 1 {
		t.Fatalf("runtime exec calls = %d, want 1", runtime.execCalls)
	}
	if len(observer.failures) != 1 {
		t.Fatalf("terminal failures = %#v, want 1", observer.failures)
	}
	got := observer.failures[0]
	if got.Flow != "guidance" || got.FailureStage != "guidance_target" || got.TurnID != "turn-original" {
		t.Fatalf("guidance terminal failure = %#v", got)
	}
}

func TestHostGuidanceTransportFailureReportsMessageSendFailure(t *testing.T) {
	observer := &recordingGuidanceTerminalFailureObserver{}
	_, store, runtime := newHostEditRetryFixture(t)
	host := agenthost.New(agenthost.Config{
		CanonicalStore:          sqliteCanonicalStore{Store: store},
		TurnSubmissions:         store,
		EffectiveHistory:        store,
		RuntimeOperations:       store,
		Runtime:                 runtime,
		HistoryRuntime:          runtime,
		GoalRuntime:             runtime,
		OperationOwner:          "worker-1",
		TerminalFailureObserver: observer,
	})
	runtime.mu.Lock()
	runtime.guidanceTransportFailure = true
	runtime.mu.Unlock()
	result, err := host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}, agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "guidance"}}, Guidance: true,
		TurnID: "turn-original", ClientSubmitID: "guidance-transport",
	})
	if err == nil {
		t.Fatal("SendInput() error = nil, want transport failure")
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		t.Fatalf("guidance disposition = %q, want precondition failed", result.GuidanceDisposition)
	}
	if _, found, claimErr := store.GetSubmitClaim(t.Context(), "workspace-1", "session-1", "guidance-transport"); claimErr != nil || !found {
		t.Fatalf("prepared claim found=%v error=%v, want retained fence", found, claimErr)
	}
	if len(observer.failures) != 1 {
		t.Fatalf("terminal failures = %#v, want 1", observer.failures)
	}
	got := observer.failures[0]
	if got.Flow != "message_send" || got.FailureStage != "runtime_exec" {
		t.Fatalf("guidance transport failure = %#v, want message_send/runtime_exec", got)
	}
	_, reuseErr := host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "must not convert"}},
		ClientSubmitID: "guidance-transport",
	})
	if !errors.Is(reuseErr, agenthost.ErrSubmitDeliveryUnknown) {
		t.Fatalf("ordinary reuse error = %v, want retained-claim delivery unknown", reuseErr)
	}
	runtime.mu.Lock()
	if runtime.execCalls != 1 {
		t.Fatalf("runtime exec calls = %d, want no ordinary redispatch", runtime.execCalls)
	}
	runtime.mu.Unlock()
}

func TestHostGuidanceTargetInactiveDoesNotAuthorizeReuseWhenClaimCleanupFails(t *testing.T) {
	wantErr := errors.New("delete submit claim unavailable")
	_, store, runtime := newHostEditRetryFixture(t)
	host := agenthost.New(agenthost.Config{
		CanonicalStore: guidanceDeleteFailureStore{
			CanonicalStore: sqliteCanonicalStore{Store: store},
			err:            wantErr,
		},
		TurnSubmissions:   store,
		EffectiveHistory:  store,
		RuntimeOperations: store,
		Runtime:           runtime,
		HistoryRuntime:    runtime,
		GoalRuntime:       runtime,
		OperationOwner:    "worker-1",
	})
	runtime.mu.Lock()
	runtime.guidanceMismatch = true
	runtime.mu.Unlock()

	result, err := host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}, agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "stale guidance"}}, Guidance: true,
		TurnID: "turn-original", ClientSubmitID: "guidance-cleanup-failure",
	})
	if !errors.Is(err, agenthost.ErrGuidanceSubmitClaimCleanupFailed) || !errors.Is(err, wantErr) {
		t.Fatalf("SendInput() error = %v, want cleanup barrier failure", err)
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		t.Fatalf("guidance disposition = %q, want precondition failed", result.GuidanceDisposition)
	}
	if _, found, claimErr := store.GetSubmitClaim(t.Context(), "workspace-1", "session-1", "guidance-cleanup-failure"); claimErr != nil || !found {
		t.Fatalf("prepared claim found=%v error=%v, want retained fence", found, claimErr)
	}
}

func TestHostGuidanceClaimCleanupTreatsAlreadyAbsentAsIdempotentSuccess(t *testing.T) {
	_, store, runtime := newHostEditRetryFixture(t)
	host := agenthost.New(agenthost.Config{
		CanonicalStore: guidanceDeleteAlreadyAbsentStore{
			CanonicalStore: sqliteCanonicalStore{Store: store},
		},
		TurnSubmissions:   store,
		EffectiveHistory:  store,
		RuntimeOperations: store,
		Runtime:           runtime,
		HistoryRuntime:    runtime,
		GoalRuntime:       runtime,
		OperationOwner:    "worker-1",
	})
	runtime.mu.Lock()
	runtime.guidanceMismatch = true
	runtime.mu.Unlock()
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}

	result, err := host.SendInput(t.Context(), ref, agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "stale guidance"}}, Guidance: true,
		TurnID: "turn-original", ClientSubmitID: "guidance-cleanup-retry",
	})
	if !errors.Is(err, agenthost.ErrActiveTurnTargetMismatch) {
		t.Fatalf("SendInput() error = %v, want target mismatch", err)
	}
	if result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionTargetInactive {
		t.Fatalf("guidance disposition = %q, want target inactive", result.GuidanceDisposition)
	}
	if _, found, claimErr := store.GetSubmitClaim(t.Context(), "workspace-1", "session-1", "guidance-cleanup-retry"); claimErr != nil || found {
		t.Fatalf("prepared claim found=%v error=%v, want already absent", found, claimErr)
	}

	if _, retryErr := host.SendInput(t.Context(), ref, agenthost.SendInput{
		Content:        []agenthost.PromptContentBlock{{Type: "text", Text: "ordinary fallback"}},
		ClientSubmitID: "guidance-cleanup-retry",
	}); retryErr != nil {
		t.Fatalf("ordinary fallback after idempotent cleanup: %v", retryErr)
	}
}

func TestHostGuidanceFailureDispositionSurvivesRestartBeforeConsumerIntent(t *testing.T) {
	tests := []agenthost.GuidanceDeliveryDisposition{
		agenthost.GuidanceDeliveryDispositionPreconditionFailed,
		agenthost.GuidanceDeliveryDispositionExplicitRejection,
		agenthost.GuidanceDeliveryDispositionOutcomeUnknown,
	}
	for _, disposition := range tests {
		disposition := disposition
		t.Run(string(disposition), func(t *testing.T) {
			firstHost, store, runtime := newHostEditRetryFixture(t)
			runtime.mu.Lock()
			runtime.guidanceFailureDisposition = disposition
			runtime.mu.Unlock()
			ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}
			input := agenthost.SendInput{
				Content: []agenthost.PromptContentBlock{{Type: "text", Text: "guidance"}}, Guidance: true,
				TurnID: "turn-original", ClientSubmitID: "guidance-restart-" + string(disposition),
			}

			first, firstErr := firstHost.SendInput(t.Context(), ref, input)
			if firstErr == nil || first.GuidanceDisposition != disposition {
				t.Fatalf("first result=%#v error=%v, want %q failure", first, firstErr, disposition)
			}
			claim, found, claimErr := store.GetSubmitClaim(
				t.Context(), ref.WorkspaceID, ref.AgentSessionID, input.ClientSubmitID,
			)
			if claimErr != nil || !found || string(claim.GuidanceDisposition) != string(disposition) {
				t.Fatalf("durable claim=%#v found=%v error=%v, want %q", claim, found, claimErr, disposition)
			}

			restarted := newGuidanceTestHost(sqliteCanonicalStore{Store: store}, store, runtime)
			replayed, replayErr := restarted.SendInput(t.Context(), ref, input)
			if replayErr == nil || replayed.GuidanceDisposition != disposition {
				t.Fatalf("restart replay=%#v error=%v, want %q failure", replayed, replayErr, disposition)
			}
			runtime.mu.Lock()
			defer runtime.mu.Unlock()
			if runtime.execCalls != 1 {
				t.Fatalf("provider exec calls=%d, want one before restart", runtime.execCalls)
			}
		})
	}
}

func TestHostGuidanceAppliedErrorSurvivesRestartBeforeConsumerIntent(t *testing.T) {
	acceptErr := errors.New("accept acknowledgement unavailable")
	_, store, runtime := newHostEditRetryFixture(t)
	firstHost := newGuidanceTestHost(guidanceAcceptFailureStore{
		CanonicalStore: sqliteCanonicalStore{Store: store},
		err:            acceptErr,
	}, store, runtime)
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}
	input := agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "guidance"}}, Guidance: true,
		TurnID: "turn-original", ClientSubmitID: "guidance-applied-restart",
	}

	first, firstErr := firstHost.SendInput(t.Context(), ref, input)
	if !errors.Is(firstErr, acceptErr) || first.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionApplied {
		t.Fatalf("first result=%#v error=%v, want applied accept error", first, firstErr)
	}
	claim, found, claimErr := store.GetSubmitClaim(
		t.Context(), ref.WorkspaceID, ref.AgentSessionID, input.ClientSubmitID,
	)
	if claimErr != nil || !found || claim.Status != "prepared" ||
		claim.GuidanceDisposition != storesqlite.SubmitClaimGuidanceDispositionApplied {
		t.Fatalf("durable applied claim=%#v found=%v error=%v", claim, found, claimErr)
	}

	restarted := newGuidanceTestHost(sqliteCanonicalStore{Store: store}, store, runtime)
	replayed, replayErr := restarted.SendInput(t.Context(), ref, input)
	if replayErr != nil || replayed.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionApplied {
		t.Fatalf("restart replay=%#v error=%v, want applied", replayed, replayErr)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.execCalls != 1 {
		t.Fatalf("provider exec calls=%d, want one before restart", runtime.execCalls)
	}
}

func TestHostGuidanceEffectiveHistoryPreconditionSurvivesRestart(t *testing.T) {
	_, store, runtime := newHostEditRetryFixture(t)
	history := &guidanceMutableEffectiveHistory{history: storesqlite.SessionHistory{
		RecoveryState: storesqlite.SessionHistoryRecoveryRollbackPending,
	}}
	firstHost := agenthost.New(agenthost.Config{
		CanonicalStore: sqliteCanonicalStore{Store: store}, TurnSubmissions: store,
		EffectiveHistory: history, RuntimeOperations: store,
		Runtime: runtime, HistoryRuntime: runtime, GoalRuntime: runtime,
		OperationOwner: "worker-1",
	})
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}
	input := agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "guidance"}}, Guidance: true,
		TurnID: "turn-original", ClientSubmitID: "guidance-history-restart",
	}

	first, firstErr := firstHost.SendInput(t.Context(), ref, input)
	if !errors.Is(firstErr, agenthost.ErrEditRetryInProgress) ||
		first.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		t.Fatalf("first result=%#v error=%v, want durable history precondition", first, firstErr)
	}
	history.setRecoveryState(storesqlite.SessionHistoryRecoveryReady)
	restarted := newGuidanceTestHost(sqliteCanonicalStore{Store: store}, store, runtime)
	replayed, replayErr := restarted.SendInput(t.Context(), ref, input)
	if !errors.Is(replayErr, agenthost.ErrGuidancePreconditionFailed) ||
		replayed.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		t.Fatalf("restart replay=%#v error=%v, want original precondition", replayed, replayErr)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.execCalls != 0 {
		t.Fatalf("provider exec calls=%d, want none", runtime.execCalls)
	}
}

func TestHostGuidanceActorWaitCancellationSurvivesRestart(t *testing.T) {
	_, store, runtime := newHostEditRetryFixture(t)
	actor := agenthost.NewSessionActor()
	prepared := make(chan struct{})
	canonicalStore := &guidancePrepareSignalStore{
		CanonicalStore: sqliteCanonicalStore{Store: store},
		prepared:       prepared,
	}
	firstHost := agenthost.New(agenthost.Config{
		CanonicalStore: canonicalStore, TurnSubmissions: store,
		EffectiveHistory: store, RuntimeOperations: store,
		Runtime: runtime, HistoryRuntime: runtime, GoalRuntime: runtime,
		OperationOwner: "worker-1", SessionMutationActor: actor,
	})
	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}
	input := agenthost.SendInput{
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "guidance"}}, Guidance: true,
		TurnID: "turn-original", ClientSubmitID: "guidance-actor-cancel-restart",
	}

	actorEntered := make(chan struct{})
	releaseActor := make(chan struct{})
	actorDone := make(chan error, 1)
	go func() {
		actorDone <- actor.Do(context.Background(), ref, func(context.Context) error {
			close(actorEntered)
			<-releaseActor
			return nil
		})
	}()
	<-actorEntered

	ctx, cancel := context.WithCancel(context.Background())
	type sendOutcome struct {
		result agenthost.SendInputResult
		err    error
	}
	sendDone := make(chan sendOutcome, 1)
	go func() {
		result, err := firstHost.SendInput(ctx, ref, input)
		sendDone <- sendOutcome{result: result, err: err}
	}()
	select {
	case <-prepared:
	case <-time.After(time.Second):
		t.Fatal("guidance claim was not prepared before waiting for SessionActor")
	}
	cancel()
	first := <-sendDone
	if !errors.Is(first.err, context.Canceled) ||
		first.result.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		t.Fatalf("canceled result=%#v error=%v, want durable precondition", first.result, first.err)
	}
	claim, found, claimErr := store.GetSubmitClaim(
		t.Context(), ref.WorkspaceID, ref.AgentSessionID, input.ClientSubmitID,
	)
	if claimErr != nil || !found ||
		claim.GuidanceDisposition != storesqlite.SubmitClaimGuidanceDispositionPreconditionFailed {
		t.Fatalf("durable canceled claim=%#v found=%v error=%v", claim, found, claimErr)
	}

	close(releaseActor)
	if err := <-actorDone; err != nil {
		t.Fatal(err)
	}
	restarted := newGuidanceTestHost(sqliteCanonicalStore{Store: store}, store, runtime)
	replayed, replayErr := restarted.SendInput(t.Context(), ref, input)
	if !errors.Is(replayErr, agenthost.ErrGuidancePreconditionFailed) ||
		replayed.GuidanceDisposition != agenthost.GuidanceDeliveryDispositionPreconditionFailed {
		t.Fatalf("restart replay=%#v error=%v, want original precondition", replayed, replayErr)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.execCalls != 0 {
		t.Fatalf("provider exec calls=%d, want none", runtime.execCalls)
	}
}

func newGuidanceTestHost(
	canonicalStore agenthost.CanonicalStore,
	store *storesqlite.Store,
	runtime *hostEditRetryRuntime,
) *agenthost.Host {
	return agenthost.New(agenthost.Config{
		CanonicalStore: canonicalStore, TurnSubmissions: store,
		EffectiveHistory: store, RuntimeOperations: store,
		Runtime: runtime, HistoryRuntime: runtime, GoalRuntime: runtime,
		OperationOwner: "worker-1",
	})
}
