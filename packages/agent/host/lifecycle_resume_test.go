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
	prepareInput RuntimePreparationInput
	prepared     PreparedRuntime
}

func (p *trackingResumePreparation) Prepare(_ context.Context, input RuntimePreparationInput) (PreparedRuntime, error) {
	p.prepareInput = input
	if len(p.prepared.MCPServers) > 0 {
		return p.prepared, nil
	}
	return PreparedRuntime{MCPServers: []MCPServerBinding{{Name: "connector", Type: "http"}}}, nil
}

type reprepareRuntime struct {
	RuntimeController
	session        ProviderRuntimeSession
	reprepareCalls int
	reprepareInput RuntimeResumeInput
}

func (r *reprepareRuntime) Session(workspaceID, sessionID string) (ProviderRuntimeSession, bool) {
	return r.session, workspaceID == r.session.WorkspaceID && sessionID == r.session.ID
}

func (r *reprepareRuntime) Reprepare(_ context.Context, input RuntimeResumeInput) (ProviderRuntimeSession, error) {
	r.reprepareCalls++
	r.reprepareInput = input
	r.session.MCPServers = cloneHostMCPServerBindings(input.MCPServers)
	return r.session, nil
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

func TestReprepareRuntimeSessionUsesRequestScopedPreparationContextAndPreservesIdentity(t *testing.T) {
	store := liveResumeCanonicalStore{
		session: storesqlite.Session{
			ID: "session-1", WorkspaceID: "workspace-1", Kind: storesqlite.SessionKindRoot,
			Provider: "codex", ProviderSessionID: "provider-session-1", Cwd: "/workspace",
			InternalRuntimeContext: map[string]any{"canonical": true, "authority": "owner"},
		},
		evidence: storesqlite.ProviderSessionResumeEvidence{Established: true},
	}
	runtime := &reprepareRuntime{session: ProviderRuntimeSession{
		ID: "session-1", WorkspaceID: "workspace-1", Provider: "codex",
		ProviderSessionID: "provider-session-1", Cwd: "/workspace",
	}}
	preparation := &trackingResumePreparation{prepared: PreparedRuntime{
		Cwd:        "/workspace",
		MCPServers: []MCPServerBinding{{Name: "connectors", Type: "http", URL: "http://127.0.0.1/new"}},
	}}
	host := New(Config{CanonicalStore: store, Runtime: runtime, RuntimePreparation: preparation})

	result, err := host.ReprepareRuntimeSession(t.Context(), ReprepareRuntimeSessionInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1",
		RuntimeContextOverlay: map[string]any{"invocationId": "invocation-1", "authority": "caller"},
	})
	if err != nil {
		t.Fatalf("ReprepareRuntimeSession() error = %v", err)
	}
	if preparation.prepareInput.RuntimeContext["canonical"] != true ||
		preparation.prepareInput.RuntimeContext["invocationId"] != "invocation-1" ||
		preparation.prepareInput.RuntimeContext["authority"] != "caller" {
		t.Fatalf("preparation runtime context = %#v", preparation.prepareInput.RuntimeContext)
	}
	if runtime.reprepareCalls != 1 || runtime.reprepareInput.ProviderSessionID != "provider-session-1" ||
		len(runtime.reprepareInput.MCPServers) != 1 || runtime.reprepareInput.MCPServers[0].URL != "http://127.0.0.1/new" {
		t.Fatalf("runtime reprepare input = %#v calls=%d", runtime.reprepareInput, runtime.reprepareCalls)
	}
	if runtime.reprepareInput.RuntimeContext["canonical"] != true || runtime.reprepareInput.RuntimeContext["invocationId"] != nil {
		t.Fatalf("request-scoped overlay leaked into provider runtime context: %#v", runtime.reprepareInput.RuntimeContext)
	}
	if result.ID != "session-1" || result.ProviderSessionID != "provider-session-1" {
		t.Fatalf("reprepared identity = %#v", result)
	}
}

func TestReprepareRuntimeSessionRejectsCanonicalActiveTurnBeforePreparation(t *testing.T) {
	store := liveResumeCanonicalStore{
		session: storesqlite.Session{
			ID: "session-1", WorkspaceID: "workspace-1", Kind: storesqlite.SessionKindRoot,
			Provider: "codex", ProviderSessionID: "provider-session-1", ActiveTurnID: "turn-1",
		},
		evidence: storesqlite.ProviderSessionResumeEvidence{Established: true},
	}
	runtime := &reprepareRuntime{session: ProviderRuntimeSession{
		ID: "session-1", WorkspaceID: "workspace-1", Provider: "codex", ProviderSessionID: "provider-session-1",
	}}
	preparation := &trackingResumePreparation{}
	host := New(Config{CanonicalStore: store, Runtime: runtime, RuntimePreparation: preparation})

	_, err := host.ReprepareRuntimeSession(t.Context(), ReprepareRuntimeSessionInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1",
	})
	if !errors.Is(err, ErrRuntimeSessionActive) {
		t.Fatalf("ReprepareRuntimeSession() error = %v, want ErrRuntimeSessionActive", err)
	}
	if preparation.prepareInput.WorkspaceID != "" || runtime.reprepareCalls != 0 {
		t.Fatalf("active reprepare reached preparation/runtime: prepare=%#v calls=%d", preparation.prepareInput, runtime.reprepareCalls)
	}
}

func TestReprepareRuntimeSessionRejectsRuntimeActiveTurnBeforePreparation(t *testing.T) {
	store := liveResumeCanonicalStore{
		session: storesqlite.Session{
			ID: "session-1", WorkspaceID: "workspace-1", Kind: storesqlite.SessionKindRoot,
			Provider: "codex", ProviderSessionID: "provider-session-1",
		},
		evidence: storesqlite.ProviderSessionResumeEvidence{Established: true},
	}
	activeTurnID := "turn-1"
	runtime := &reprepareRuntime{session: ProviderRuntimeSession{
		ID: "session-1", WorkspaceID: "workspace-1", Provider: "codex", ProviderSessionID: "provider-session-1",
		TurnLifecycle: &TurnLifecycle{ActiveTurnID: &activeTurnID},
	}}
	preparation := &trackingResumePreparation{}
	host := New(Config{CanonicalStore: store, Runtime: runtime, RuntimePreparation: preparation})

	_, err := host.ReprepareRuntimeSession(t.Context(), ReprepareRuntimeSessionInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1",
	})
	if !errors.Is(err, ErrRuntimeSessionActive) {
		t.Fatalf("ReprepareRuntimeSession() error = %v, want ErrRuntimeSessionActive", err)
	}
	if preparation.prepareInput.WorkspaceID != "" || runtime.reprepareCalls != 0 {
		t.Fatalf("active reprepare reached preparation/runtime: prepare=%#v calls=%d", preparation.prepareInput, runtime.reprepareCalls)
	}
}
