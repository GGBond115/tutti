package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

func (a *CodexAppServerAdapter) Start(ctx context.Context, session Session) (events []activityshared.Event, err error) {
	unlockLifecycle, releaseFenced := a.lockSessionLifecycleForStart(session.AgentSessionID)
	defer unlockLifecycle()
	if releaseFenced {
		return nil, ErrSessionDisconnected
	}
	if err := a.admitCodexReplacementLocked(session.AgentSessionID); err != nil {
		return nil, err
	}
	trace := newCodexAppServerStartupTrace(session)
	defer func() {
		trace.Finish(err)
	}()
	extraSkillRoots, err := tuttiAgentExtraSkillRoots(a.config.skillRootsStrategy, session.Env)
	if err != nil {
		return nil, err
	}
	stableSystemSkillsRoot, err := tuttiAgentStableSystemSkillsRoot(a.config.skillRootsStrategy, session.Env)
	if err != nil {
		return nil, err
	}
	// Starting over replaces only the Session binding. The physical process is
	// owned by the profile-keyed registry and must remain reusable when the
	// replacement has the same process profile.
	if existing := a.getSession(session.AgentSessionID); existing != nil && existing.client != nil {
		a.rejectPendingRequests(session.AgentSessionID, errPermissionRequestCanceled)
		if existing.connection != nil && existing.binding != nil {
			_ = existing.connection.detachBinding(existing.binding)
			a.mu.Lock()
			if a.sessions[session.AgentSessionID] == existing {
				delete(a.sessions, session.AgentSessionID)
			}
			a.mu.Unlock()
		} else {
			_ = a.closeLiveSession(session.AgentSessionID)
		}
	}
	client, initializeResult, _, err := a.startClient(ctx, session, trace, false)
	if err != nil {
		return nil, err
	}
	connection := a.connections.connectionForClient(client)
	binding := connection.bindingForStartup(session.AgentSessionID, session.ProviderSessionID)
	started := false
	keepSession := false
	startedSession := &codexAppServerSession{
		client:          client,
		connection:      connection,
		binding:         binding,
		pendingRequests: make(map[string]*pendingInteractiveRequest),
	}
	defer func() {
		if !started {
			a.closeOrRetainCodexSession(session.AgentSessionID, startedSession)
		}
		if !keepSession {
			a.removeSession(session.AgentSessionID)
		}
	}()
	serverInfo := a.appServerInfo(initializeResult)
	a.storeSession(session.AgentSessionID, &codexAppServerSession{
		client:          client,
		connection:      connection,
		binding:         binding,
		serverInfo:      serverInfo,
		acpLiveState:    newACPLiveState(),
		pendingRequests: make(map[string]*pendingInteractiveRequest),
	})
	if err := a.stabilizeSystemSkillPaths(session, stableSystemSkillsRoot, trace); err != nil {
		return nil, err
	}
	if err := a.configureExtraSkillRoots(ctx, client, session, extraSkillRoots, trace); err != nil {
		return nil, err
	}

	account, authRequired := a.fetchAccount(ctx, client, session, trace)
	if authRequired {
		// Authentication is a live Session state, not a failed startup. Keep
		// the connection and Thread binding that startClient registered so
		// Close can detach the binding and release the shared process lease.
		startedSession.serverInfo = serverInfo
		startedSession.account = account
		startedSession.authState = "auth_required"
		startedSession.authMessage = a.config.authRequiredMessage
		startedSession.acpLiveState = newACPLiveState()
		a.storeSession(session.AgentSessionID, startedSession)
		started = true
		keepSession = true
		return []activityshared.Event{newSessionActivityEvent(session, EventSessionStarted, SessionStatusReady, map[string]any{
			"adapter":          a.commandString(),
			"command":          a.commandString(),
			"agent":            serverInfo,
			"permissionModeId": session.PermissionModeID,
			"authState":        "auth_required",
			"authMessage":      a.config.authRequiredMessage,
		})}, nil
	}
	models := []map[string]any(nil)
	if codexAppServerNeedsSynchronousModels(session) {
		models = a.fetchModels(ctx, client, session, trace)
	}
	if len(models) > 0 {
		effectiveSettings := codexAppServerEffectiveSettings(models, session, nil)
		session.Settings = &effectiveSettings
	}
	planModeMask, defaultModeMask := a.fetchCollaborationModeMasks(ctx, client, session, trace)

	threadParams := appServerThreadStartParams(session, a.sessionCWD(session))
	applyAppServerThreadOverlay(threadParams, binding.overlay)
	trace.Log("thread.start.params", codexAppServerTraceThreadStartParams(session, threadParams, false))
	threadResult, err := trace.TypedCall(acpStartCallTimeout, appServerMethodThreadStart, func() (json.RawMessage, error) {
		return client.ThreadStart(ctx, acpStartCallTimeout, threadParams, nil)
	})
	if err != nil {
		var callErr *acpCallError
		if errors.As(err, &callErr) && callErr.AuthRequired() {
			startedSession.serverInfo = serverInfo
			startedSession.account = account
			startedSession.authState = "auth_required"
			startedSession.authMessage = a.config.authRequiredMessage
			startedSession.acpLiveState = newACPLiveState()
			a.storeSession(session.AgentSessionID, startedSession)
			started = true
			keepSession = true
			return []activityshared.Event{newSessionActivityEvent(session, EventSessionStarted, SessionStatusReady, map[string]any{
				"adapter":          a.commandString(),
				"command":          a.commandString(),
				"agent":            serverInfo,
				"permissionModeId": session.PermissionModeID,
				"authState":        "auth_required",
				"authMessage":      a.config.authRequiredMessage,
			})}, nil
		}
		return nil, err
	}
	threadID, err := appServerThreadID(threadResult)
	if err != nil {
		return nil, err
	}
	if err := connection.bindThread(binding, threadID); err != nil {
		return nil, err
	}
	if err := binding.flush(ctx); err != nil {
		return nil, err
	}
	_, startupUsage, startupUsageKnown := binding.snapshot()
	session.ProviderSessionID = threadID
	trace.Log("thread.id.resolved", map[string]any{
		"thread_id": threadID,
	})
	slog.Info("agent session app-server thread started",
		"event", "agent_session.app_server.thread_start.succeeded",
		"provider", a.config.provider,
		"room_id", session.RoomID,
		"agent_session_id", session.AgentSessionID,
		"provider_session_id", threadID,
	)

	liveState := newACPLiveState()
	liveState.currentMode = codexACPEffectiveModeID(session)
	liveState.availableCommands = codexAppServerCommands()
	liveState.commandsKnown = true
	applyACPConfigOptionDescriptors(&liveState, codexAppServerConfigOptionDescriptors(models, session, threadResult))
	if startupUsageKnown {
		liveState.usage = mergeACPUsageState(liveState.usage, startupUsage)
	}

	started = true
	keepSession = true
	a.storeSession(session.AgentSessionID, &codexAppServerSession{
		client:                 client,
		connection:             connection,
		binding:                binding,
		threadID:               threadID,
		runtimeSession:         session,
		serverInfo:             serverInfo,
		account:                account,
		models:                 cloneCodexAppServerModels(models),
		startupModelsReady:     len(models) > 0,
		startupRateLimitsReady: false,
		planModeMask:           planModeMask,
		defaultModeMask:        defaultModeMask,
		defaultModel:           codexAppServerSessionDefaultModel(session, models),
		authState:              "authenticated",
		acpLiveState:           liveState,
		pendingRequests:        make(map[string]*pendingInteractiveRequest),
	})
	a.refreshStartupMetadataAsync(session, threadResult, len(models) == 0, a.config.rateLimits, trace)
	a.emitCommandSnapshot(AgentSessionCommandSnapshot{
		AgentSessionID: strings.TrimSpace(session.AgentSessionID),
		Commands:       codexAppServerCommands(),
	})
	return []activityshared.Event{newSessionActivityEvent(session, EventSessionStarted, SessionStatusReady, map[string]any{
		"adapter":          a.commandString(),
		"command":          a.commandString(),
		"agent":            serverInfo,
		"permissionModeId": session.PermissionModeID,
	})}, nil
}

func (a *CodexAppServerAdapter) Resume(ctx context.Context, session Session) (err error) {
	if strings.TrimSpace(session.ProviderSessionID) == "" {
		return missingProviderSessionResumeError(session)
	}
	unlockLifecycle := a.lockSessionLifecycleForResume(session.AgentSessionID)
	defer unlockLifecycle()
	if err := a.admitCodexReplacementLocked(session.AgentSessionID); err != nil {
		return err
	}
	// Resume may run over a session that still holds a live client. Unlike
	// Start, the old client is kept alive until the replacement has resumed
	// successfully (storeSession closes it on replace): if the new spawn or
	// thread/resume fails, the previous session must remain usable.
	trace := newCodexAppServerStartupTrace(session)
	defer func() {
		trace.Finish(err)
	}()
	trace.Log("resume.begin", map[string]any{
		"thread_id": strings.TrimSpace(session.ProviderSessionID),
	})
	extraSkillRoots, err := tuttiAgentExtraSkillRoots(a.config.skillRootsStrategy, session.Env)
	if err != nil {
		return err
	}
	stableSystemSkillsRoot, err := tuttiAgentStableSystemSkillsRoot(a.config.skillRootsStrategy, session.Env)
	if err != nil {
		return err
	}
	client, initializeResult, attachedCheckpoint, err := a.startClient(ctx, session, trace, true)
	if err != nil {
		return err
	}
	connection := a.connections.connectionForClient(client)
	binding := connection.bindingForStartup(session.AgentSessionID, session.ProviderSessionID)
	started := false
	keepSession := false
	previousSession := a.getSession(session.AgentSessionID)
	startedSession := &codexAppServerSession{
		client:          client,
		connection:      connection,
		binding:         binding,
		pendingRequests: make(map[string]*pendingInteractiveRequest),
	}
	if previousSession == nil {
		// Cold Resume still needs an adapter-visible provisional owner before
		// thread/resume: Codex may replay usage, output, or interactions before
		// returning the RPC response. Failure cleanup below removes it again.
		a.storeSession(session.AgentSessionID, startedSession)
	}
	defer func() {
		if !started {
			a.closeOrRetainCodexSession(session.AgentSessionID, startedSession)
		}
		if !keepSession {
			if previousSession != nil {
				a.storeSession(session.AgentSessionID, previousSession)
			} else {
				a.removeSession(session.AgentSessionID)
			}
		}
	}()
	if attachedCheckpoint {
		planModeMask, defaultModeMask, defaultModel, checkpointFound :=
			codexAppServerProtocolCheckpointFromRuntimeContext(session.RuntimeContext)
		if !checkpointFound {
			return errors.New(
				"attached live Codex replay is missing provider resume checkpoint; record a new cassette",
			)
		}
		liveState := newACPLiveState()
		liveState.currentMode = codexACPEffectiveModeID(session)
		liveState.availableCommands = codexAppServerCommands()
		liveState.commandsKnown = true
		applyACPConfigOptionDescriptors(
			&liveState,
			codexAppServerConfigOptionDescriptors(nil, session, nil),
		)
		started = true
		keepSession = true
		a.storeSession(session.AgentSessionID, &codexAppServerSession{
			client:               client,
			connection:           connection,
			binding:              binding,
			threadID:             strings.TrimSpace(session.ProviderSessionID),
			resumeRuntimeContext: clonePayload(session.RuntimeContext),
			planModeMask:         planModeMask,
			defaultModeMask:      defaultModeMask,
			defaultModel: firstNonEmpty(
				strings.TrimSpace(session.SettingsValue().Model),
				defaultModel,
			),
			authState:       "authenticated",
			acpLiveState:    liveState,
			pendingRequests: make(map[string]*pendingInteractiveRequest),
		})
		a.emitCommandSnapshot(AgentSessionCommandSnapshot{
			AgentSessionID: strings.TrimSpace(session.AgentSessionID),
			Commands:       codexAppServerCommands(),
		})
		return nil
	}
	serverInfo := a.appServerInfo(initializeResult)
	if err := a.stabilizeSystemSkillPaths(session, stableSystemSkillsRoot, trace); err != nil {
		return err
	}
	if err := a.configureExtraSkillRoots(ctx, client, session, extraSkillRoots, trace); err != nil {
		return err
	}

	account, authRequired := a.fetchAccount(ctx, client, session, trace)
	if authRequired {
		// Keep the provisional binding on an auth-required resume as well. The
		// caller owns this live Session and Close must release its Thread lease.
		startedSession.threadID = strings.TrimSpace(session.ProviderSessionID)
		startedSession.serverInfo = serverInfo
		startedSession.account = account
		startedSession.authState = "auth_required"
		startedSession.authMessage = a.config.authRequiredMessage
		startedSession.acpLiveState = newACPLiveState()
		a.storeSession(session.AgentSessionID, startedSession)
		started = true
		keepSession = true
		return nil
	}
	models := []map[string]any(nil)
	if codexAppServerNeedsSynchronousModels(session) {
		models = a.fetchModels(ctx, client, session, trace)
	}
	if len(models) > 0 && strings.TrimSpace(session.SettingsValue().ReasoningEffort) != "" {
		hasExplicitModel := strings.TrimSpace(session.SettingsValue().Model) != ""
		effectiveSettings := codexAppServerEffectiveSettings(models, session, nil)
		// The catalog default is needed to validate an effort-only persisted
		// setting, but it must not become a thread/resume model override. The
		// existing thread remains authoritative until the resume result reports
		// its actual model.
		if !hasExplicitModel {
			effectiveSettings.Model = ""
		}
		session.Settings = &effectiveSettings
	}
	planModeMask, defaultModeMask := a.fetchCollaborationModeMasks(ctx, client, session, trace)

	params := appServerThreadStartParams(session, a.sessionCWD(session))
	applyAppServerThreadOverlay(params, binding.overlay)
	params["threadId"] = strings.TrimSpace(session.ProviderSessionID)
	trace.Log("thread.start.params", codexAppServerTraceThreadStartParams(session, params, true))
	threadResult, err := trace.TypedCall(acpStartCallTimeout, appServerMethodThreadResume, func() (json.RawMessage, error) {
		return client.ThreadResume(ctx, acpStartCallTimeout, params, nil)
	})
	if err != nil {
		return classifyACPResumeError(session, appServerMethodThreadResume, err)
	}
	resumedThreadID, threadIDErr := appServerThreadID(threadResult)
	if threadIDErr == nil {
		if err := connection.bindThread(binding, resumedThreadID); err != nil {
			return err
		}
	}
	if err := binding.flush(ctx); err != nil {
		return err
	}
	sharedReplacement := binding.replacementOf.Load() != nil
	if err := connection.commitBindingReplacement(binding); err != nil {
		return err
	}
	if sharedReplacement {
		a.publishReplacementBinding(session.AgentSessionID, previousSession, binding)
	}
	if sharedReplacement && a.appServerReplacementCommittedHook != nil {
		a.appServerReplacementCommittedHook(binding)
	}
	_, replayedUsage, replayedUsageKnown := binding.snapshot()
	if len(models) > 0 {
		effectiveSettings := codexAppServerEffectiveSettings(models, session, threadResult)
		session.Settings = &effectiveSettings
	}
	liveState := newACPLiveState()
	liveState.currentMode = codexACPEffectiveModeID(session)
	liveState.availableCommands = codexAppServerCommands()
	liveState.commandsKnown = true
	applyACPConfigOptionDescriptors(&liveState, codexAppServerConfigOptionDescriptors(models, session, threadResult))
	if replayedUsageKnown {
		liveState.usage = mergeACPUsageState(liveState.usage, replayedUsage)
	}
	if previousSession != nil {
		liveState.usage = mergeACPUsageState(previousSession.usage, liveState.usage)
	}

	started = true
	keepSession = true
	nextSession := &codexAppServerSession{
		client:                 client,
		connection:             connection,
		binding:                binding,
		threadID:               strings.TrimSpace(session.ProviderSessionID),
		runtimeSession:         session,
		serverInfo:             serverInfo,
		account:                account,
		models:                 cloneCodexAppServerModels(models),
		startupModelsReady:     len(models) > 0,
		startupRateLimitsReady: false,
		planModeMask:           planModeMask,
		defaultModeMask:        defaultModeMask,
		defaultModel:           codexAppServerSessionDefaultModel(session, models),
		authState:              "authenticated",
		acpLiveState:           liveState,
		pendingRequests:        make(map[string]*pendingInteractiveRequest),
	}
	if sharedReplacement {
		a.storeReplacementSession(session.AgentSessionID, binding, nextSession)
		connection.publishBindingReplacement(binding)
	} else {
		a.storeSession(session.AgentSessionID, nextSession)
	}
	a.refreshStartupMetadataAsync(session, threadResult, len(models) == 0, a.config.rateLimits, trace)
	// Mirror Start: push the command snapshot so a resumed session advertises
	// review/compact/undo to the GUI (otherwise the slash palette and the
	// review picker only work on freshly created sessions).
	a.emitCommandSnapshot(AgentSessionCommandSnapshot{
		AgentSessionID: strings.TrimSpace(session.AgentSessionID),
		Commands:       codexAppServerCommands(),
	})
	return nil
}

func (*CodexAppServerAdapter) CanResume(session Session) bool {
	return strings.TrimSpace(session.ProviderSessionID) != ""
}

func (a *CodexAppServerAdapter) HasLiveSession(session Session) bool {
	a.mu.Lock()
	appSession := a.sessions[strings.TrimSpace(session.AgentSessionID)]
	if appSession == nil || appSession.client == nil || appSession.releasing || appSession.releaseFailed {
		a.mu.Unlock()
		return false
	}
	client := appSession.client
	a.mu.Unlock()
	select {
	case <-client.Done():
		return false
	default:
		return true
	}
}

func (a *CodexAppServerAdapter) Close(ctx context.Context, session Session) error {
	if a == nil {
		return nil
	}
	agentSessionID := strings.TrimSpace(session.AgentSessionID)
	unlockLifecycle := a.lockSessionLifecycle(agentSessionID)
	defer unlockLifecycle()
	a.rejectPendingRequests(agentSessionID, errPermissionRequestCanceled)
	return a.detachLiveSession(ctx, agentSessionID, false)
}

func (a *CodexAppServerAdapter) QuiesceForClose(
	ctx context.Context,
	session Session,
) error {
	if a == nil {
		return nil
	}
	appTurn := a.sessionActiveTurn(session.AgentSessionID)
	if appTurn == nil &&
		a.sessionActiveTurnID(session.AgentSessionID) == "" {
		return nil
	}
	appSession := a.getSession(session.AgentSessionID)
	_, err := a.Cancel(ctx, session, "session closed")
	if errors.Is(err, ErrSessionDisconnected) {
		return nil
	}
	if err != nil && !errors.Is(err, ErrSessionNoActiveTurn) {
		return err
	}
	if appTurn == nil {
		return nil
	}
	select {
	case <-appTurn.terminated:
		return nil
	default:
	}

	// Cancel queues an interrupt when turn/start has been sent but has not
	// returned the provider Turn id yet. Close must not detach the session while
	// that queued interrupt still depends on the session registry. Wait for the
	// normal binding/interrupt path, then tear down the shared transport if the
	// provider never supplies an interruptible identity within the ordinary
	// cancellation grace window.
	grace := a.cancelGraceWindow
	if grace <= 0 {
		grace = defaultCodexAppServerCancelGraceWindow
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-appTurn.terminated:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	a.markTurnForceCanceled(appTurn)
	slog.Warn(
		"agent session app-server force-closing turn with unresolved provider identity",
		"event", "agent_session.app_server.close.pending_turn_start_forced",
		"agent_session_id", session.AgentSessionID,
		"provider_session_id", session.ProviderSessionID,
		"turn_id", appTurn.turnID,
		"grace_ms", grace.Milliseconds(),
	)
	if appSession != nil && appSession.client != nil {
		_ = appSession.client.Close()
	}
	select {
	case <-appTurn.terminated:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *CodexAppServerAdapter) ReleaseLiveSession(ctx context.Context, session Session) error {
	if a == nil {
		return nil
	}
	agentSessionID := strings.TrimSpace(session.AgentSessionID)
	unlockLifecycle := a.lockSessionLifecycleForRelease(agentSessionID)
	defer unlockLifecycle()
	if a.hasLiveSessionWork(agentSessionID) {
		return ErrLiveSessionBusy
	}
	return a.detachLiveSession(ctx, agentSessionID, true)
}

// DisconnectLiveSession resolves pending interactions and drops only the
// app-server transport. The Codex thread remains resumable; no provider
// thread/session deletion request is sent.
func (a *CodexAppServerAdapter) DisconnectLiveSession(ctx context.Context, session Session) error {
	if a == nil {
		return nil
	}
	agentSessionID := strings.TrimSpace(session.AgentSessionID)
	unlockLifecycle := a.lockSessionLifecycle(agentSessionID)
	defer unlockLifecycle()
	a.rejectPendingRequests(agentSessionID, ErrSessionDisconnected)
	return a.detachLiveSession(ctx, agentSessionID, true)
}

func (a *CodexAppServerAdapter) closeLiveSession(agentSessionID string) error {
	return a.detachLiveSession(context.Background(), agentSessionID, true)
}

func (a *CodexAppServerAdapter) detachLiveSession(ctx context.Context, agentSessionID string, closePhysical bool) error {
	a.mu.Lock()
	appSession := a.sessions[agentSessionID]
	var client *codexAppServerClient
	var connection *appServerConnection
	var binding *appServerThreadBinding
	var threadID string
	if appSession != nil {
		client = appSession.client
		connection = appSession.connection
		binding = appSession.binding
		threadID = appSession.threadID
		if client != nil {
			appSession.releasing = true
		}
	}
	a.mu.Unlock()
	if appSession != nil && connection != nil {
		var unsubscribeErr error
		if threadID = strings.TrimSpace(threadID); threadID != "" {
			unsubscribeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, unsubscribeErr = client.ThreadUnsubscribe(unsubscribeCtx, 5*time.Second, threadID)
			cancel()
		}
		var detachErr error
		if binding != nil {
			detachErr = connection.detachBinding(binding)
			a.mu.Lock()
			if a.sessions[agentSessionID] == appSession {
				appSession.binding = nil
				appSession.threadID = ""
			}
			a.mu.Unlock()
		}
		var closeErr error
		if closePhysical {
			_, closeErr = a.connections.closeIfIdle(connection)
		}
		a.mu.Lock()
		if closeErr != nil {
			if a.sessions[agentSessionID] == appSession {
				appSession.releasing = false
				appSession.releaseFailed = true
			}
		} else if a.sessions[agentSessionID] == appSession {
			delete(a.sessions, agentSessionID)
		}
		a.mu.Unlock()
		return errors.Join(unsubscribeErr, detachErr, closeErr)
	}
	if appSession != nil && client != nil {
		if err := client.Close(); err != nil {
			a.mu.Lock()
			if a.sessions[agentSessionID] == appSession {
				appSession.releasing = false
				appSession.releaseFailed = true
			}
			a.mu.Unlock()
			return err
		}
		a.mu.Lock()
		if a.sessions[agentSessionID] == appSession {
			delete(a.sessions, agentSessionID)
		}
		a.mu.Unlock()
	}
	return nil
}

func (a *CodexAppServerAdapter) startInitializedClient(
	ctx context.Context,
	session Session,
	trace *codexAppServerStartupTrace,
) (*codexAppServerClient, json.RawMessage, error) {
	launch, err := a.prepareInitializedClientLaunch(ctx, session)
	if err != nil {
		return nil, nil, err
	}
	client, initializeResult, _, err := a.startClientPrepared(ctx, session, trace, launch, false, false)
	return client, initializeResult, err
}

func (a *CodexAppServerAdapter) startClient(
	ctx context.Context,
	session Session,
	trace *codexAppServerStartupTrace,
	allowAttachedCheckpoint bool,
) (*codexAppServerClient, json.RawMessage, bool, error) {
	launch, err := a.prepareInitializedClientLaunch(ctx, session)
	if err != nil {
		trace.Log("process.prepare.failed", map[string]any{
			"error": err.Error(),
		})
		return nil, nil, false, err
	}
	client, initializeResult, attachedCheckpoint, err := a.startClientPrepared(
		ctx,
		session,
		trace,
		launch,
		allowAttachedCheckpoint,
		true,
	)
	if err != nil {
		return nil, nil, false, errors.Join(err, a.connections.cleanupOrRetain(launch.threadCleanup))
	}
	connection := a.connections.connectionForClient(client)
	if connection == nil {
		cleanupErr := a.connections.cleanupOrRetain(launch.threadCleanup)
		_ = client.Close()
		return nil, nil, false, errors.Join(errors.New("app-server connection registry lost the started client"), cleanupErr)
	}
	expectedThreadID := ""
	if allowAttachedCheckpoint || strings.TrimSpace(session.ProviderSessionID) != "" {
		expectedThreadID = strings.TrimSpace(session.ProviderSessionID)
	}
	binding, err := connection.registerBinding(session, expectedThreadID, launch.overlay, launch.threadCleanup)
	if err != nil {
		return nil, nil, false, errors.Join(err, a.connections.cleanupOrRetain(launch.threadCleanup))
	}
	binding.startupTrace = trace
	return client, initializeResult, attachedCheckpoint, nil
}

func (a *CodexAppServerAdapter) startClientPrepared(
	ctx context.Context,
	session Session,
	trace *codexAppServerStartupTrace,
	launch appServerPreparedLaunch,
	allowAttachedCheckpoint bool,
	shared bool,
) (*codexAppServerClient, json.RawMessage, bool, error) {
	if !shared {
		connection, err := a.startPhysicalAppServerConnection(
			ctx, session, trace, launch, allowAttachedCheckpoint, 0,
		)
		if err != nil {
			return nil, nil, false, err
		}
		return connection.client, connection.initializeResult, connection.attachedCheckpoint, nil
	}
	key, err := appServerConnectionKeyForLaunch(a.transport, launch)
	if err != nil {
		return nil, nil, false, errors.Join(err, a.connections.cleanupOrRetain(launch.processCleanup))
	}
	connection, reused, err := a.connections.acquire(ctx, key, func(generation uint64) (*appServerConnection, error) {
		return a.startPhysicalAppServerConnection(
			ctx, session, trace, launch, allowAttachedCheckpoint, generation,
		)
	})
	if err != nil {
		return nil, nil, false, err
	}
	if reused {
		// This acquisition did not consume a new process lease. Release the
		// preparation reference; the existing Connection retains its own lease.
		if cleanupErr := a.connections.cleanupOrRetain(launch.processCleanup); cleanupErr != nil {
			return nil, nil, false, cleanupErr
		}
	}
	return connection.client, connection.initializeResult, connection.attachedCheckpoint, nil
}

func (a *CodexAppServerAdapter) startPhysicalAppServerConnection(
	ctx context.Context,
	session Session,
	trace *codexAppServerStartupTrace,
	launch appServerPreparedLaunch,
	allowAttachedCheckpoint bool,
	generation uint64,
) (*appServerConnection, error) {
	if launch.profile == nil {
		return nil, errors.Join(
			errors.New("app-server launch requires explicit process profile preparation"),
			a.connections.cleanupOrRetain(launch.processCleanup),
		)
	}
	connectionKey, err := keyForUnregisteredConnection(launch)
	if err != nil {
		return nil, errors.Join(err, a.connections.cleanupOrRetain(launch.processCleanup))
	}
	spec := launch.spec
	cleanup := launch.processCleanup
	trace.Log("process.start.begin", map[string]any{
		"command": strings.Join(spec.Command, " "),
		"cwd":     spec.CWD,
	})
	processStartedAt := time.Now()
	conn, err := a.transport.Start(ctx, spec)
	if err != nil {
		cleanupErr := a.connections.cleanupOrRetain(cleanup)
		trace.Log("process.start.failed", map[string]any{
			"duration_ms": time.Since(processStartedAt).Milliseconds(),
			"error":       err.Error(),
		})
		return nil, errors.Join(err, cleanupErr)
	}
	conn = wrapProviderLaunchCleanup(conn, cleanup)
	trace.Log("process.start.succeeded", map[string]any{
		"duration_ms": time.Since(processStartedAt).Milliseconds(),
	})
	client := newCodexAppServerClient(conn)
	client.SetStderrSink(trace.LogStderr)
	connection := &appServerConnection{
		key: connectionKey, generation: generation, client: client,
		bindingsBySession:     make(map[string]*appServerThreadBinding),
		ownerByThread:         make(map[string]string),
		replacementByThread:   make(map[string]*appServerThreadBinding),
		unknownByThread:       make(map[string][]appServerRoutedMessage),
		retiredThreadCleanups: make(map[*appServerThreadBinding]struct{}),
	}
	client.SetMessageHandler(connection.route)
	started := false
	defer func() {
		if !started {
			client.SetMessageHandler(nil)
			_ = client.Close()
		}
	}()
	captureOrigin := processCassetteCaptureOrigin(conn)
	if captureOrigin == ProcessCassetteCaptureOriginAttachedLiveConnection {
		if !allowAttachedCheckpoint {
			return nil, errors.New(
				"attached live provider checkpoint cannot start a new Codex session",
			)
		}
		connection.attachedCheckpoint = true
		started = true
		return connection, nil
	}

	connection.setInitializingObserver(func(ctx context.Context, message acpMessage) error {
		trace.LogMessage(message.Method, len(message.ID) > 0, len(message.Params))
		_, err := a.handleAppServerMessage(ctx, client, session, "", message, nil, nil, nil)
		return err
	})
	initializeResult, err := trace.TypedCall(acpStartCallTimeout, appServerMethodInitialize, func() (json.RawMessage, error) {
		return client.Initialize(ctx, acpStartCallTimeout, map[string]any{
			"clientInfo": a.clientInfoParams(spec.Env),
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		}, nil)
	})
	connection.setInitializingObserver(nil)
	if err != nil {
		slog.Warn("agent session app-server initialize failed",
			"event", "agent_session.app_server.initialize.failed",
			"room_id", session.RoomID,
			"agent_session_id", session.AgentSessionID,
			"error", err.Error(),
		)
		return nil, err
	}
	trace.Log("initialized.notify.begin", nil)
	notifyStartedAt := time.Now()
	if err := client.Initialized(ctx); err != nil {
		trace.Log("initialized.notify.failed", map[string]any{
			"duration_ms": time.Since(notifyStartedAt).Milliseconds(),
			"error":       err.Error(),
		})
		return nil, err
	}
	trace.Log("initialized.notify.succeeded", map[string]any{
		"duration_ms": time.Since(notifyStartedAt).Milliseconds(),
	})
	connection.initializeResult = initializeResult
	started = true
	return connection, nil
}

func keyForUnregisteredConnection(launch appServerPreparedLaunch) (appServerConnectionKey, error) {
	if launch.profile == nil {
		return appServerConnectionKey{}, errors.New(
			"app-server launch requires explicit process profile preparation",
		)
	}
	return appServerConnectionKey{
		Provider:             strings.TrimSpace(launch.spec.Provider),
		ExecutionHostID:      strings.TrimSpace(launch.profile.ExecutionHostID),
		RuntimeGeneration:    strings.TrimSpace(launch.profile.RuntimeGeneration),
		TransportScopeID:     strings.TrimSpace(launch.profile.TransportScopeID),
		ProcessProfileDigest: strings.TrimSpace(launch.profile.ProcessProfileDigest),
	}, nil
}

func appServerEventsForActiveRootTurn(
	rootAgentSessionID string,
	activeRootTurnID string,
	events []activityshared.Event,
) ([]activityshared.Event, []activityshared.Event) {
	rootAgentSessionID = strings.TrimSpace(rootAgentSessionID)
	activeRootTurnID = strings.TrimSpace(activeRootTurnID)
	turnEvents := make([]activityshared.Event, 0, len(events))
	detachedChildEvents := make([]activityshared.Event, 0, len(events))
	for _, event := range events {
		eventAgentSessionID := strings.TrimSpace(event.AgentSessionID)
		if eventAgentSessionID != "" && eventAgentSessionID != rootAgentSessionID {
			if activeRootTurnID == "" || strings.TrimSpace(event.RootTurnID) != activeRootTurnID {
				detachedChildEvents = append(detachedChildEvents, event)
				continue
			}
		}
		if activeRootTurnID != "" {
			turnEvents = append(turnEvents, event)
		}
	}
	return turnEvents, detachedChildEvents
}
