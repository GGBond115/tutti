package agentruntime

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

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
