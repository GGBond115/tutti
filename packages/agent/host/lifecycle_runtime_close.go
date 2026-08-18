package agenthost

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// CloseLiveRuntimeSession closes one live provider connection without
// publishing a destructive canonical completion or deleting provider resume
// state. Runtime preparation cleanup is part of this same Host lifecycle
// boundary. A preparation failure is returned and intentionally remains
// retryable on a later close or generation sweep.
func (h *Host) CloseLiveRuntimeSession(
	ctx context.Context,
	ref SessionRef,
) (CloseLiveRuntimeSessionResult, error) {
	ref.WorkspaceID = strings.TrimSpace(ref.WorkspaceID)
	ref.AgentSessionID = strings.TrimSpace(ref.AgentSessionID)
	if h == nil || h.runtime == nil || ref.WorkspaceID == "" || ref.AgentSessionID == "" {
		return CloseLiveRuntimeSessionResult{}, ErrInvalidArgument
	}

	var result CloseLiveRuntimeSessionResult
	err := h.sessionMutationActor.Do(ctx, ref, func(actorCtx context.Context) error {
		session, found := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID)
		cleanupInput, pending := h.pendingRuntimePreparationCleanup(ref)
		if found {
			closeErr := h.runtime.Close(actorCtx, RuntimeCloseInput{
				WorkspaceID:            ref.WorkspaceID,
				AgentSessionID:         ref.AgentSessionID,
				PreserveCanonicalState: true,
			})
			if closeErr != nil {
				return fmt.Errorf("close live agent session %q runtime: %w", ref.AgentSessionID, closeErr)
			}
			result.Closed = true
			cleanupInput = RuntimeCleanupInput{
				WorkspaceID:              ref.WorkspaceID,
				AgentSessionID:           ref.AgentSessionID,
				Provider:                 strings.TrimSpace(session.Provider),
				PreserveRecoverableState: true,
			}
		} else if !pending {
			return ErrSessionNotFound
		}
		if h.preparation == nil {
			return nil
		}
		result.PreparationCleanupAttempted = true
		cleanupErr := h.preparation.Cleanup(actorCtx, cleanupInput)
		if cleanupErr != nil {
			result.PreparationCleanupFailed = true
			h.rememberPendingRuntimePreparationCleanup(cleanupInput)
			return fmt.Errorf("cleanup live agent session %q runtime preparation: %w", ref.AgentSessionID, cleanupErr)
		}
		h.forgetPendingRuntimePreparationCleanup(cleanupInput)
		return nil
	})
	return result, err
}

// CloseAllLiveRuntimeSessions invalidates the current runtime generation by
// closing every live provider connection through CloseLiveRuntimeSession.
// Canonical sessions, provider identities, terminal results, and history stay
// intact. Every candidate is attempted; errors are aggregated so one failed
// provider or preparation cleanup does not strand the remaining connections.
func (h *Host) CloseAllLiveRuntimeSessions(
	ctx context.Context,
) (CloseAllLiveRuntimeSessionsResult, error) {
	if h == nil || h.runtime == nil {
		return CloseAllLiveRuntimeSessionsResult{}, ErrInvalidArgument
	}
	var sessions []ProviderRuntimeSession
	if lister, ok := h.runtime.(RuntimeLiveSessionLister); ok {
		var err error
		sessions, err = lister.LiveRuntimeSessions(ctx)
		if err != nil {
			return CloseAllLiveRuntimeSessionsResult{}, err
		}
	} else if len(h.pendingRuntimePreparationCleanupSnapshot()) == 0 {
		return CloseAllLiveRuntimeSessionsResult{}, ErrRuntimeLiveSessionListUnavailable
	}
	for _, pending := range h.pendingRuntimePreparationCleanupSnapshot() {
		sessions = append(sessions, ProviderRuntimeSession{
			ID: pending.AgentSessionID, WorkspaceID: pending.WorkspaceID, Provider: pending.Provider,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		left := strings.TrimSpace(sessions[i].WorkspaceID) + "\x00" + strings.TrimSpace(sessions[i].ID)
		right := strings.TrimSpace(sessions[j].WorkspaceID) + "\x00" + strings.TrimSpace(sessions[j].ID)
		return left < right
	})
	result := CloseAllLiveRuntimeSessionsResult{}
	var failures []error
	seen := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		workspaceID := strings.TrimSpace(session.WorkspaceID)
		sessionID := strings.TrimSpace(session.ID)
		if workspaceID == "" || sessionID == "" {
			continue
		}
		key := workspaceID + "\x00" + sessionID
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result.Scanned++
		closed, closeErr := h.CloseLiveRuntimeSession(ctx, SessionRef{
			WorkspaceID: workspaceID, AgentSessionID: sessionID,
		})
		if closed.Closed {
			result.Closed++
		}
		if closed.PreparationCleanupAttempted {
			result.PreparationCleanupAttempted++
		}
		if closed.PreparationCleanupFailed {
			result.PreparationCleanupFailed++
		}
		if closeErr != nil {
			result.Failed++
			failures = append(failures, closeErr)
			if errors.Is(closeErr, context.Canceled) || errors.Is(closeErr, context.DeadlineExceeded) {
				break
			}
		}
	}
	return result, errors.Join(failures...)
}
