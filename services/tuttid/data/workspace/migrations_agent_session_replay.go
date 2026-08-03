package workspace

import (
	"context"
	"fmt"
	"time"
)

func (s *SQLiteStore) applyAgentSessionReplayV1(ctx context.Context) error {
	applied, err := s.hasMigration(ctx, schemaMigrationAgentSessionReplayV1)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	now := unixMs(time.Now().UTC())
	_, err = s.writeDB.ExecContext(ctx, `
CREATE TABLE agent_session_recordings (
  recording_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  agent_target_id TEXT NOT NULL,
  mode TEXT NOT NULL,
  root_agent_session_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  cassette_id TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at_unix_ms INTEGER NOT NULL,
  recording_at_unix_ms INTEGER NOT NULL DEFAULT 0,
  stopped_at_unix_ms INTEGER NOT NULL DEFAULT 0,
  updated_at_unix_ms INTEGER NOT NULL
);

CREATE INDEX idx_agent_session_recordings_workspace_updated
  ON agent_session_recordings(workspace_id, updated_at_unix_ms DESC);

CREATE UNIQUE INDEX idx_agent_session_recordings_cassette
  ON agent_session_recordings(cassette_id)
  WHERE cassette_id <> '';

CREATE TABLE agent_session_cassettes (
  cassette_id TEXT PRIMARY KEY,
  source_recording_id TEXT NOT NULL UNIQUE,
  workspace_id TEXT NOT NULL,
  agent_target_id TEXT NOT NULL,
  root_agent_session_id TEXT NOT NULL,
  mode TEXT NOT NULL,
  total_bytes INTEGER NOT NULL,
  manifest_sha256 TEXT NOT NULL,
  created_at_unix_ms INTEGER NOT NULL
);

CREATE INDEX idx_agent_session_cassettes_workspace_created
  ON agent_session_cassettes(workspace_id, created_at_unix_ms DESC);

CREATE TABLE agent_session_replay_runs (
  replay_run_id TEXT PRIMARY KEY,
  cassette_id TEXT NOT NULL,
  status TEXT NOT NULL,
  checkpoint INTEGER NOT NULL DEFAULT 0,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at_unix_ms INTEGER NOT NULL,
  started_at_unix_ms INTEGER NOT NULL DEFAULT 0,
  completed_at_unix_ms INTEGER NOT NULL DEFAULT 0,
  updated_at_unix_ms INTEGER NOT NULL,
  FOREIGN KEY (cassette_id) REFERENCES agent_session_cassettes(cassette_id) ON DELETE CASCADE
);

CREATE INDEX idx_agent_session_replay_runs_cassette_created
  ON agent_session_replay_runs(cassette_id, created_at_unix_ms DESC);

INSERT INTO tuttid_schema_migrations (id, applied_at_unix_ms)
  VALUES (?, ?);
`, schemaMigrationAgentSessionReplayV1, now)
	if err != nil {
		return fmt.Errorf("migrate Agent Session Replay v1: %w", err)
	}
	return nil
}

func (s *SQLiteStore) applyAgentSessionReplayV2(ctx context.Context) error {
	applied, err := s.hasMigration(ctx, schemaMigrationAgentSessionReplayV2)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	now := unixMs(time.Now().UTC())
	_, err = s.writeDB.ExecContext(ctx, `
ALTER TABLE agent_session_recordings ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_session_cassettes ADD COLUMN name TEXT NOT NULL DEFAULT '';

UPDATE agent_session_recordings
SET name = strftime('%Y-%m-%dT%H:%M:%fZ', created_at_unix_ms / 1000.0, 'unixepoch');
UPDATE agent_session_cassettes
SET name = strftime('%Y-%m-%dT%H:%M:%fZ', created_at_unix_ms / 1000.0, 'unixepoch');

INSERT INTO tuttid_schema_migrations (id, applied_at_unix_ms)
  VALUES (?, ?);
`, schemaMigrationAgentSessionReplayV2, now)
	if err != nil {
		return fmt.Errorf("migrate Agent Session Replay v2: %w", err)
	}
	return nil
}

// applyAgentSessionReplayV3 migrates the Agent Session Replay schema shipped
// on main to the current cassette-owned replay model. Workspace ownership is
// derived from the source Recording, and replay runs are no longer persisted.
func (s *SQLiteStore) applyAgentSessionReplayV3(ctx context.Context) error {
	applied, err := s.hasMigration(ctx, schemaMigrationAgentSessionReplayV3)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Agent Session Replay v3 migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
DROP TABLE IF EXISTS agent_session_replay_runs;
DROP INDEX IF EXISTS idx_agent_session_cassettes_workspace_created;
`); err != nil {
		return fmt.Errorf("remove obsolete Agent Session Replay schema: %w", err)
	}
	var workspaceColumnCount int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM pragma_table_info('agent_session_cassettes')
WHERE name = 'workspace_id'
`).Scan(&workspaceColumnCount); err != nil {
		return fmt.Errorf("inspect Agent Session Cassette workspace column: %w", err)
	}
	if workspaceColumnCount > 0 {
		if _, err := tx.ExecContext(
			ctx,
			`ALTER TABLE agent_session_cassettes DROP COLUMN workspace_id`,
		); err != nil {
			return fmt.Errorf("drop Agent Session Cassette workspace column: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO tuttid_schema_migrations (id, applied_at_unix_ms)
VALUES (?, ?)
`, schemaMigrationAgentSessionReplayV3, unixMs(time.Now().UTC())); err != nil {
		return fmt.Errorf("record Agent Session Replay v3 migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Agent Session Replay v3 migration: %w", err)
	}
	return nil
}

func (s *SQLiteStore) applyAgentSessionReplayV4(ctx context.Context) error {
	applied, err := s.hasMigration(ctx, schemaMigrationAgentSessionReplayV4)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	_, err = s.writeDB.ExecContext(ctx, `
ALTER TABLE agent_session_recordings
  ADD COLUMN replay_prerequisites_json TEXT NOT NULL DEFAULT '{}';

INSERT INTO tuttid_schema_migrations (id, applied_at_unix_ms)
VALUES (?, ?);
`, schemaMigrationAgentSessionReplayV4, unixMs(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("migrate Agent Session Replay v4: %w", err)
	}
	return nil
}
