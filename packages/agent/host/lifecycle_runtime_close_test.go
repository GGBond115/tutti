package agenthost

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type closeLiveRuntimeTestRuntime struct {
	RuntimeController
	sessions   map[string]ProviderRuntimeSession
	closeErrs  map[string]error
	closeCalls []RuntimeCloseInput
}

func (r *closeLiveRuntimeTestRuntime) Session(workspaceID, agentSessionID string) (ProviderRuntimeSession, bool) {
	session, ok := r.sessions[runtimePreparationCleanupKey(workspaceID, agentSessionID)]
	return session, ok
}

func (r *closeLiveRuntimeTestRuntime) Close(_ context.Context, input RuntimeCloseInput) error {
	r.closeCalls = append(r.closeCalls, input)
	key := runtimePreparationCleanupKey(input.WorkspaceID, input.AgentSessionID)
	if err := r.closeErrs[key]; err != nil {
		return err
	}
	delete(r.sessions, key)
	return nil
}

func (r *closeLiveRuntimeTestRuntime) LiveRuntimeSessions(context.Context) ([]ProviderRuntimeSession, error) {
	result := make([]ProviderRuntimeSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		result = append(result, session)
	}
	return result, nil
}

type closeLiveRuntimeTestPreparation struct {
	RuntimePreparationPort
	cleanupCalls []RuntimeCleanupInput
	failNext     bool
}

func (p *closeLiveRuntimeTestPreparation) Cleanup(_ context.Context, input RuntimeCleanupInput) error {
	p.cleanupCalls = append(p.cleanupCalls, input)
	if p.failNext {
		p.failNext = false
		return errors.New("transient preparation cleanup failure")
	}
	return nil
}

func TestCloseLiveRuntimeSessionPreservesCanonicalStateAndRetriesPreparationCleanup(t *testing.T) {
	runtime := &closeLiveRuntimeTestRuntime{
		sessions: map[string]ProviderRuntimeSession{
			runtimePreparationCleanupKey("workspace-1", "session-1"): {
				ID: "session-1", WorkspaceID: "workspace-1", Provider: "codex",
			},
		},
		closeErrs: make(map[string]error),
	}
	preparation := &closeLiveRuntimeTestPreparation{failNext: true}
	host := New(Config{Runtime: runtime, RuntimePreparation: preparation})

	first, err := host.CloseLiveRuntimeSession(t.Context(), SessionRef{WorkspaceID: " workspace-1 ", AgentSessionID: " session-1 "})
	if err == nil || !first.Closed || !first.PreparationCleanupAttempted || !first.PreparationCleanupFailed {
		t.Fatalf("first close result/error = %#v/%v", first, err)
	}
	second, err := host.CloseLiveRuntimeSession(t.Context(), SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"})
	if err != nil || second.Closed || !second.PreparationCleanupAttempted || second.PreparationCleanupFailed {
		t.Fatalf("retry close result/error = %#v/%v", second, err)
	}
	if len(runtime.closeCalls) != 1 || !runtime.closeCalls[0].PreserveCanonicalState {
		t.Fatalf("runtime close calls = %#v, want one lossless close", runtime.closeCalls)
	}
	if len(preparation.cleanupCalls) != 2 || preparation.cleanupCalls[0].Provider != "codex" ||
		!preparation.cleanupCalls[0].PreserveRecoverableState || !preparation.cleanupCalls[1].PreserveRecoverableState {
		t.Fatalf("preparation cleanup calls = %#v, want two recoverable retries", preparation.cleanupCalls)
	}
}

func TestCloseAllLiveRuntimeSessionsContinuesAndPreservesCanonicalState(t *testing.T) {
	failedKey := runtimePreparationCleanupKey("workspace-1", "session-failed")
	runtime := &closeLiveRuntimeTestRuntime{
		sessions: map[string]ProviderRuntimeSession{
			runtimePreparationCleanupKey("workspace-1", "session-success"): {
				ID: "session-success", WorkspaceID: "workspace-1", Provider: "codex",
			},
			failedKey: {ID: "session-failed", WorkspaceID: "workspace-1", Provider: "claude-code"},
		},
		closeErrs: map[string]error{failedKey: errors.New("provider close failed")},
	}
	preparation := &closeLiveRuntimeTestPreparation{}
	host := New(Config{Runtime: runtime, RuntimePreparation: preparation})

	result, err := host.CloseAllLiveRuntimeSessions(t.Context())
	if err == nil || result.Scanned != 2 || result.Closed != 1 || result.Failed != 1 ||
		result.PreparationCleanupAttempted != 1 || result.PreparationCleanupFailed != 0 {
		t.Fatalf("close-all result/error = %#v/%v", result, err)
	}
	if got := []string{runtime.closeCalls[0].AgentSessionID, runtime.closeCalls[1].AgentSessionID}; !slices.Contains(got, "session-success") || !slices.Contains(got, "session-failed") {
		t.Fatalf("close calls = %#v, want both sessions attempted", runtime.closeCalls)
	}
	for _, input := range runtime.closeCalls {
		if !input.PreserveCanonicalState {
			t.Fatalf("close input=%#v, want canonical preservation", input)
		}
	}
}
