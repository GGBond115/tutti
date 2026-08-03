package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WaitAgentSessionGraphSettled keeps the capture window open until every
// already-accepted canonical operation in the selected graph is terminal.
func (s *SQLiteStore) WaitAgentSessionGraphSettled(
	ctx context.Context,
	workspaceID string,
	rootAgentSessionID string,
) error {
	if s == nil || s.readDB == nil {
		return errors.New("workspace database is not initialized")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var active int
		err := s.readDB.QueryRowContext(ctx, `
WITH graph(agent_session_id) AS (
  SELECT agent_session_id
  FROM workspace_agent_sessions
  WHERE workspace_id = ?
    AND deleted_at_unix_ms = 0
    AND (agent_session_id = ? OR root_agent_session_id = ?)
)
SELECT
  (SELECT COUNT(*)
   FROM workspace_agent_sessions
   WHERE workspace_id = ? AND agent_session_id IN (SELECT agent_session_id FROM graph) AND active_turn_id IS NOT NULL)
  +
  (SELECT COUNT(*)
   FROM workspace_agent_runtime_operations
   WHERE workspace_id = ? AND agent_session_id IN (SELECT agent_session_id FROM graph) AND status IN ('prepared','leased'))
  +
  (SELECT COUNT(*)
   FROM workspace_agent_goal_control_operations
   WHERE workspace_id = ? AND agent_session_id IN (SELECT agent_session_id FROM graph) AND status IN ('prepared','dispatched'))
  +
  (SELECT COUNT(*)
   FROM workspace_workflow_operations
   WHERE workspace_id = ?
     AND status IN ('pending','running')
     AND workflow_id IN (
       SELECT workflow_id FROM workspace_workflows
       WHERE workspace_id = ? AND source_session_id IN (SELECT agent_session_id FROM graph)
     ))
`, workspaceID, rootAgentSessionID, rootAgentSessionID,
			workspaceID, workspaceID, workspaceID, workspaceID, workspaceID,
		).Scan(&active)
		if err != nil {
			return fmt.Errorf("read Agent Session graph finalization state: %w", err)
		}
		if active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for Agent Session graph to settle: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// ResolveRootAgentSession returns the canonical root for either a root or
// child Session. It does not infer graph identity from provider state.
func (s *SQLiteStore) ResolveRootAgentSession(
	ctx context.Context,
	workspaceID string,
	agentSessionID string,
) (string, error) {
	if s == nil || s.readDB == nil {
		return "", errors.New("workspace database is not initialized")
	}
	var rootID string
	err := s.readDB.QueryRowContext(ctx, `
SELECT CASE
  WHEN TRIM(COALESCE(root_agent_session_id, '')) = '' THEN agent_session_id
  ELSE root_agent_session_id
END
FROM workspace_agent_sessions
WHERE workspace_id = ? AND agent_session_id = ? AND deleted_at_unix_ms = 0
`, strings.TrimSpace(workspaceID), strings.TrimSpace(agentSessionID)).Scan(&rootID)
	if err != nil {
		return "", fmt.Errorf("resolve Agent Session root: %w", err)
	}
	return strings.TrimSpace(rootID), nil
}
