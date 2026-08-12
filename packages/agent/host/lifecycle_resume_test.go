package agenthost

import (
	"context"
	"errors"
	"testing"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

type liveResumeCanonicalStore struct {
	CanonicalStore
	session  storesqlite.Session
	evidence storesqlite.ProviderSessionResumeEvidence
}

func (s liveResumeCanonicalStore) GetSession(context.Context, string, string) (storesqlite.Session, bool, error) {
	return s.session, true, nil
}

func (liveResumeCanonicalStore) SessionDeleted(context.Context, string, string) (bool, error) {
	return false, nil
}

func (s liveResumeCanonicalStore) GetProviderSessionResumeEvidence(
	context.Context,
	string,
	string,
) (storesqlite.ProviderSessionResumeEvidence, error) {
	return s.evidence, nil
}

type liveResumeRuntime struct {
	RuntimeController
	session ProviderRuntimeSession
}

type failingResumeRuntime struct {
	RuntimeController
	err         error
	resumeCalls int
	closeCalls  int
}

func (*failingResumeRuntime) Session(string, string) (ProviderRuntimeSession, bool) {
	return ProviderRuntimeSession{}, false
}

func (r *failingResumeRuntime) Resume(context.Context, RuntimeResumeInput) (ProviderRuntimeSession, error) {
	r.resumeCalls++
	return ProviderRuntimeSession{}, r.err
}

func (r *failingResumeRuntime) Close(context.Context, RuntimeCloseInput) error {
	r.closeCalls++
	return nil
}

type trackingResumePreparation struct {
	cleanupCalls int
	cleanupInput RuntimeCleanupInput
}

func (*trackingResumePreparation) Prepare(context.Context, RuntimePreparationInput) (PreparedRuntime, error) {
	return PreparedRuntime{MCPServers: []MCPServerBinding{{Name: "connector", Type: "http"}}}, nil
}

func (p *trackingResumePreparation) Cleanup(_ context.Context, input RuntimeCleanupInput) error {
	p.cleanupCalls++
	p.cleanupInput = input
	return nil
}

func (r liveResumeRuntime) Session(workspaceID, sessionID string) (ProviderRuntimeSession, bool) {
	return r.session, workspaceID == r.session.WorkspaceID && sessionID == r.session.ID
}

func TestEnsureRuntimeSessionPreservesLiveResumableObservation(t *testing.T) {
	store := liveResumeCanonicalStore{session: storesqlite.Session{
		ID: "session-1", WorkspaceID: "workspace-1", Kind: storesqlite.SessionKindRoot,
		Provider: "codex", ProviderSessionID: "provider-session-1",
	}}
	runtime := liveResumeRuntime{session: ProviderRuntimeSession{
		ID: "session-1", WorkspaceID: "workspace-1", Provider: "codex",
		ProviderSessionID: "provider-session-1", Resumable: true,
	}}
	host := New(Config{CanonicalStore: store, Runtime: runtime})

	session, err := host.EnsureRuntimeSession(t.Context(), SessionRef{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1",
	})
	if err != nil {
		t.Fatalf("EnsureRuntimeSession() error = %v", err)
	}
	if !session.Resumable {
		t.Fatal("EnsureRuntimeSession() discarded live resumable observation")
	}
}

func TestEnsureRuntimeSessionCleansPreparedResourcesWhenResumeFails(t *testing.T) {
	resumeErr := errors.New("resume failed")
	store := liveResumeCanonicalStore{
		session: storesqlite.Session{
			ID: "session-1", WorkspaceID: "workspace-1", Kind: storesqlite.SessionKindRoot,
			Provider: "codex", ProviderSessionID: "provider-session-1",
		},
		evidence: storesqlite.ProviderSessionResumeEvidence{Established: true},
	}
	runtime := &failingResumeRuntime{err: resumeErr}
	preparation := &trackingResumePreparation{}
	host := New(Config{CanonicalStore: store, Runtime: runtime, RuntimePreparation: preparation})

	_, err := host.EnsureRuntimeSession(t.Context(), SessionRef{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1",
	})
	if !errors.Is(err, resumeErr) {
		t.Fatalf("EnsureRuntimeSession() error = %v, want %v", err, resumeErr)
	}
	if runtime.resumeCalls != 1 || runtime.closeCalls != 1 {
		t.Fatalf("runtime calls resume=%d close=%d, want 1/1", runtime.resumeCalls, runtime.closeCalls)
	}
	if preparation.cleanupCalls != 1 {
		t.Fatalf("preparation cleanup calls = %d, want 1", preparation.cleanupCalls)
	}
	if preparation.cleanupInput.WorkspaceID != "workspace-1" ||
		preparation.cleanupInput.AgentSessionID != "session-1" ||
		preparation.cleanupInput.Provider != "codex" {
		t.Fatalf("cleanup input = %#v", preparation.cleanupInput)
	}
}
