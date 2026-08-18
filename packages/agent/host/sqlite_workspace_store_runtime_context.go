package agenthost

import (
	"context"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

func (s *SQLiteWorkspaceStore) UpdateSessionRuntimeContext(
	ctx context.Context,
	workspaceID, sessionID string,
	patch map[string]any,
) (storesqlite.Session, bool, error) {
	store, err := s.store(workspaceID)
	if err != nil {
		return storesqlite.Session{}, false, err
	}
	if len(patch) == 0 {
		return store.GetSession(ctx, workspaceID, sessionID)
	}
	if existing, found, err := store.GetSession(ctx, workspaceID, sessionID); err != nil {
		return storesqlite.Session{}, false, err
	} else if found {
		alreadyApplied := true
		for key, value := range patch {
			if stringValue(existing.InternalRuntimeContext[key]) != stringValue(value) {
				alreadyApplied = false
				break
			}
		}
		if alreadyApplied {
			return existing, false, nil
		}
	}
	result, err := store.ReportSessionState(ctx, storesqlite.SessionStateReport{
		WorkspaceID: workspaceID, AgentSessionID: sessionID,
		RuntimeContextPatch: &canonical.RuntimeContextPatch{Set: cloneStringAnyMap(patch)},
		OccurredAtUnixMS:    s.now().UnixMilli(),
	})
	if err != nil {
		return storesqlite.Session{}, false, err
	}
	if result.Accepted || result.StateApplied {
		NotifyCommitted(ctx, s.Observer, CanonicalDelta(result.CommitDelta))
	}
	persisted, _, err := store.GetSession(ctx, workspaceID, sessionID)
	if err != nil {
		return storesqlite.Session{}, false, err
	}
	return persisted, result.StateApplied, nil
}
