package agent

import (
	"context"
	"errors"
	"testing"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

type unavailableSessionReader struct {
	delegate SessionReader
}

func (unavailableSessionReader) GetSession(string, string) (PersistedSession, bool) {
	return PersistedSession{}, false
}

func (r unavailableSessionReader) ListSessions(workspaceID string) ([]PersistedSession, bool) {
	return r.delegate.ListSessions(workspaceID)
}

func (r unavailableSessionReader) SessionDeleted(ctx context.Context, workspaceID, agentSessionID string) (bool, error) {
	return r.delegate.SessionDeleted(ctx, workspaceID, agentSessionID)
}

type failAfterSessionReads struct {
	delegate     SessionReader
	allowedReads int
	reads        int
}

func (r *failAfterSessionReads) GetSession(workspaceID, agentSessionID string) (PersistedSession, bool) {
	if r.reads >= r.allowedReads {
		return PersistedSession{}, false
	}
	r.reads++
	return r.delegate.GetSession(workspaceID, agentSessionID)
}

func (r *failAfterSessionReads) ListSessions(workspaceID string) ([]PersistedSession, bool) {
	return r.delegate.ListSessions(workspaceID)
}

func (r *failAfterSessionReads) SessionDeleted(ctx context.Context, workspaceID, agentSessionID string) (bool, error) {
	return r.delegate.SessionDeleted(ctx, workspaceID, agentSessionID)
}

func TestHostCreateWithInitialInputRollsBackTurnlessCanonicalShell(t *testing.T) {
	execErr := errors.New("provider rejected initial input")
	runtime := newFakeRuntime()
	runtime.execErr = execErr
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(context.Background(), workspacebiz.Summary{ID: "ws-1", Name: "Workspace"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	projection := NewActivityProjection(store)
	publisher := &activityUpdatePublisherStub{}
	projection.SetPublisher(publisher)
	service := newTestService(runtime)
	service.SessionReader = projection
	service.SessionInitializer = projection

	created, err := service.CreateWithResult(context.Background(), "ws-1", CreateSessionInput{
		AgentSessionID: "session-no-turnless-shell", AgentTargetID: agenttargetbiz.IDLocalCodex,
		InitialContent: TextPromptContent("start atomically"),
	})
	if !errors.Is(err, execErr) {
		t.Fatalf("Create() error=%v, want %v", err, execErr)
	}
	if created.SessionStatus != agenthost.CreateSessionStatusNotCreated ||
		created.InitialGoalStatus != agenthost.CreateSessionInitialGoalStatusNotRequested {
		t.Fatalf(
			"create outcome session=%q goal=%q, want not_created/not_requested",
			created.SessionStatus,
			created.InitialGoalStatus,
		)
	}
	if _, ok, err := store.GetSession(context.Background(), "ws-1", "session-no-turnless-shell"); err != nil || ok {
		t.Fatalf("canonical shell after failed initial input ok=%v error=%v", ok, err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("failed provisional create published turnless session events=%#v", publisher.events)
	}
}

func TestHostCreateRetainsVisibleFailedTurnAfterExplicitProviderRejection(t *testing.T) {
	ctx := context.Background()
	execErr := &agenthost.ProviderError{
		Code:    "auth_required",
		Message: "Claude Code needs authentication",
	}
	runtime := newFakeRuntime()
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "ws-rejected", Name: "Workspace"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	projection := NewActivityProjection(store)
	publisher := &activityUpdatePublisherStub{}
	projection.SetPublisher(publisher)
	runtime.execHook = func(input RuntimeExecInput) (RuntimeExecResult, error) {
		if err := projection.Report(ctx, agentsessionstore.ReportActivityInput{
			WorkspaceID: input.WorkspaceID,
			Source: canonical.EventSource{
				AgentID: input.AgentSessionID, Provider: "claude-code",
				SessionOrigin: agentsessionstore.WorkspaceAgentSessionOriginRuntime,
			},
			StatePatches: []agentsessionstore.WorkspaceAgentStatePatch{{
				AgentSessionID: input.AgentSessionID, Kind: storesqlite.SessionKindRoot,
				Provider: "claude-code", CurrentPhase: "submitted", OccurredAtUnixMS: 2,
				RuntimeContext: map[string]any{"visible": true, "provisional": false},
				Turn: &agentsessionstore.WorkspaceAgentTurnPatch{
					TurnID: input.TurnID, Origin: storesqlite.TurnOriginUserPrompt,
					Phase: storesqlite.TurnPhaseSubmitted,
				},
			}},
		}); err != nil {
			return RuntimeExecResult{}, err
		}
		return RuntimeExecResult{
			AgentSessionID: input.AgentSessionID,
			TurnID:         input.TurnID,
			ProviderDispatch: agenthost.RuntimeProviderDispatchResult{
				Disposition: agenthost.RuntimeDispatchDispositionRejected,
			},
		}, execErr
	}
	runtime.provenanceHook = func(input RuntimeSubmitProvenanceInput) error {
		if err := projection.Report(ctx, agentsessionstore.ReportActivityInput{
			WorkspaceID: input.WorkspaceID,
			Source: canonical.EventSource{
				AgentID: input.AgentSessionID, Provider: "claude-code",
				SessionOrigin: agentsessionstore.WorkspaceAgentSessionOriginRuntime,
			},
			StatePatches: []agentsessionstore.WorkspaceAgentStatePatch{{
				AgentSessionID: input.AgentSessionID, Kind: storesqlite.SessionKindRoot,
				Provider: "claude-code", CurrentPhase: "failed", OccurredAtUnixMS: input.CanonicalSubmitOccurredAtUnixMS + 1,
				RuntimeContext: map[string]any{"visible": true, "provisional": false},
				Turn: &agentsessionstore.WorkspaceAgentTurnPatch{
					TurnID: input.TurnID, Origin: storesqlite.TurnOriginUserPrompt,
					Phase: storesqlite.TurnPhaseSettled, Outcome: storesqlite.TurnOutcomeFailed,
					ErrorCode: execErr.Code, ErrorMessage: execErr.Message,
				},
			}},
		}); err != nil {
			return err
		}
		return projection.UpdateSubmitProvenance(ctx, agentsessionstore.SubmitProvenanceInput{
			WorkspaceID: input.WorkspaceID, AgentSessionID: input.AgentSessionID, ClientSubmitID: input.ClientSubmitID,
			CanonicalTurnID: input.TurnID, CanonicalMessageID: "client-submit:user:" + input.ClientSubmitID,
			ProviderSessionID: input.ProviderSessionID, ProviderTurnID: input.ProviderTurnID,
			DispatchStatus: "failed", DeliveryStatus: "failed", FailureReason: execErr.Error(),
			OccurredAtUnixMS: input.CanonicalSubmitOccurredAtUnixMS + 1,
		})
	}
	service := newTestService(runtime)
	service.SessionReader = projection
	service.SessionInitializer = projection
	service.SubmitClaimStore = store
	service.TurnStore = store

	input := CreateSessionInput{
		AgentSessionID: "session-rejected", AgentTargetID: agenttargetbiz.IDLocalClaudeCode,
		InitialContent: TextPromptContent("start atomically"),
		ClientSubmitID: "submit-rejected",
	}
	_, err := service.Create(ctx, "ws-rejected", input)
	if !errors.Is(err, execErr) {
		t.Fatalf("Create() error=%v, want %v", err, execErr)
	}
	session, ok, err := store.GetSession(ctx, "ws-rejected", "session-rejected")
	if err != nil || !ok {
		t.Fatalf("canonical session after rejection ok=%v error=%v", ok, err)
	}
	if !session.Metadata.Visible {
		t.Fatalf("canonical session after rejection visible=%v, want true", session.Metadata.Visible)
	}
	turn, ok, err := store.GetTurn(ctx, "ws-rejected", "session-rejected", runtime.execCalls[0].TurnID)
	if err != nil || !ok {
		t.Fatalf("canonical Turn after rejection ok=%v error=%v", ok, err)
	}
	if turn.Phase != storesqlite.TurnPhaseSettled || turn.Outcome != storesqlite.TurnOutcomeFailed {
		t.Fatalf("canonical Turn after rejection = %#v, want settled/failed", turn)
	}
	page, ok, err := store.ListSessionMessages(ctx, storesqlite.ListSessionMessagesInput{
		WorkspaceID: "ws-rejected", AgentSessionID: "session-rejected", Order: storesqlite.MessageOrderAsc, Limit: 10,
	})
	if err != nil || !ok || len(page.Messages) != 0 {
		t.Fatalf("canonical messages after rejection page=%#v ok=%v error=%v session=%#v provenance=%#v, want no hook replay", page, ok, err, session, runtime.provenanceCalls)
	}
	if len(runtime.provenanceCalls) != 1 {
		t.Fatalf("submit provenance calls=%d, want 1", len(runtime.provenanceCalls))
	}
	if len(runtime.closeCalls) != 1 || !runtime.closeCalls[0].PreserveCanonicalState {
		t.Fatalf("runtime close calls=%#v, want one canonical-preserving discard", runtime.closeCalls)
	}
	if len(publisher.events) == 0 {
		t.Fatal("rejected visible create did not publish canonical updates")
	}
	claim, found, err := store.GetSubmitClaim(ctx, "ws-rejected", input.AgentSessionID, input.ClientSubmitID)
	if err != nil || !found || claim.Status != "rejected" || claim.TurnID != runtime.execCalls[0].TurnID {
		t.Fatalf("rejected submit claim=%#v found=%v error=%v", claim, found, err)
	}
	startCalls, execCalls, provenanceCalls, closeCalls := len(runtime.startCalls), len(runtime.execCalls), len(runtime.provenanceCalls), len(runtime.closeCalls)
	replayed, retryErr := service.CreateWithResult(ctx, "ws-rejected", input)
	if retryErr != nil || replayed.TurnID != claim.TurnID {
		t.Fatalf("replayed CreateWithResult=%#v error=%v, want terminal failed Turn %q", replayed, retryErr, claim.TurnID)
	}
	if len(runtime.startCalls) != startCalls || len(runtime.execCalls) != execCalls || len(runtime.provenanceCalls) != provenanceCalls || len(runtime.closeCalls) != closeCalls {
		t.Fatalf("replayed rejected submit touched runtime: starts=%d exec=%d provenance=%d close=%d, want %d/%d/%d/%d", len(runtime.startCalls), len(runtime.execCalls), len(runtime.provenanceCalls), len(runtime.closeCalls), startCalls, execCalls, provenanceCalls, closeCalls)
	}
}

func TestHostCreateWithInvalidTypedGoalPreservesPublishedSession(t *testing.T) {
	ctx := context.Background()
	runtime := newFakeRuntime()
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "ws-goal", Name: "Goal workspace"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	projection := NewActivityProjection(store)
	publisher := &activityUpdatePublisherStub{}
	projection.SetPublisher(publisher)
	service := newTestService(runtime)
	service.SessionReader = unavailableSessionReader{delegate: projection}
	service.SessionInitializer = projection
	service.GoalStateStore = store

	created, err := service.CreateWithResult(ctx, "ws-goal", CreateSessionInput{
		AgentSessionID: "session-invalid-goal", AgentTargetID: agenttargetbiz.IDLocalCodex,
		InitialContent: TextPromptContent("/goal pause"),
	})
	if !errors.Is(err, storesqlite.ErrGoalStateAbsent) {
		t.Fatalf("Create() error=%v, want %v", err, storesqlite.ErrGoalStateAbsent)
	}
	if created.Session.ID != "session-invalid-goal" {
		t.Fatalf("created Session ID=%q, want preserved canonical Session", created.Session.ID)
	}
	if created.SessionStatus != agenthost.CreateSessionStatusCreated ||
		created.InitialGoalStatus != agenthost.CreateSessionInitialGoalStatusFailed {
		t.Fatalf(
			"create outcome session=%q goal=%q, want created/failed",
			created.SessionStatus,
			created.InitialGoalStatus,
		)
	}
	if _, found, getErr := store.GetSession(ctx, "ws-goal", "session-invalid-goal"); getErr != nil || !found {
		t.Fatalf("published canonical session found=%v error=%v", found, getErr)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published session event count=%d, want 1", len(publisher.events))
	}
}

func TestHostCreateWithSuccessfulTypedGoalPreservesSessionWhenResponseReadFails(t *testing.T) {
	ctx := context.Background()
	runtime := newFakeRuntime()
	runtime.goalControlHook = func(_ context.Context, input RuntimeGoalControlInput) (RuntimeGoalControlResult, error) {
		return RuntimeGoalControlResult{
			Goal:          map[string]any{"objective": input.Objective, "status": "active"},
			ProviderPhase: "accepted",
			Evidence:      map[string]any{"phase": "accepted"},
		}, nil
	}
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "ws-goal-success", Name: "Goal workspace"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	projection := NewActivityProjection(store)
	goalStore := &recordingGoalStateStore{}
	service := newTestService(runtime)
	// Canonical initialization now performs one durable preflight read while
	// holding the Host session lock. Allow that additional internal read so this
	// test still fails at the response projection boundary after GoalControl has
	// durably succeeded.
	service.SessionReader = &failAfterSessionReads{delegate: projection, allowedReads: 3}
	service.SessionInitializer = projection
	service.GoalStateStore = goalStore

	created, err := service.CreateWithResult(ctx, "ws-goal-success", CreateSessionInput{
		AgentSessionID: "session-goal-success",
		AgentTargetID:  agenttargetbiz.IDLocalCodex,
		InitialContent: TextPromptContent("/goal ship it"),
		Metadata:       map[string]any{"clientSubmitId": "submit-goal-success"},
	})
	if err == nil {
		t.Fatal("CreateWithResult() error=nil, want response projection failure")
	}
	if created.Session.ID != "session-goal-success" {
		t.Fatalf("created Session ID=%q, want preserved Host Session", created.Session.ID)
	}
	if created.SessionStatus != agenthost.CreateSessionStatusCreated ||
		created.InitialGoalStatus != agenthost.CreateSessionInitialGoalStatusSucceeded {
		t.Fatalf(
			"create outcome session=%q goal=%q, want created/succeeded",
			created.SessionStatus,
			created.InitialGoalStatus,
		)
	}
	if _, found, getErr := store.GetSession(ctx, "ws-goal-success", "session-goal-success"); getErr != nil || !found {
		t.Fatalf("published canonical session found=%v error=%v", found, getErr)
	}
}

func TestProvisionalRuntimeSessionShellIsHiddenAndUnpublished(t *testing.T) {
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(context.Background(), workspacebiz.Summary{ID: "ws-1", Name: "Workspace"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	projection := NewActivityProjection(store)
	publisher := &activityUpdatePublisherStub{}
	projection.SetPublisher(publisher)

	persisted, err := projection.InitializeRuntimeSession(context.Background(), ProviderRuntimeSession{
		ID: "session-provisional", WorkspaceID: "ws-1", Provider: "codex",
		Status: "ready", Visible: true, Provisional: true, CreatedAtUnixMS: 1, UpdatedAtUnixMS: 1,
	}, nil)
	if err != nil {
		t.Fatalf("InitializeRuntimeSession() error=%v", err)
	}
	if persisted.Metadata.Visible {
		t.Fatalf("provisional canonical shell visible=%v, want false", persisted.Metadata.Visible)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("provisional canonical shell published events=%#v", publisher.events)
	}
}
