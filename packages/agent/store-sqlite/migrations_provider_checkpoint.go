package storesqlite

import (
	"context"
	"fmt"
)

func (s *Store) applyWorkspaceAgentProviderCheckpointV1(ctx context.Context) error {
	applied, err := s.hasMigration(ctx, schemaMigrationWorkspaceAgentProviderCheckpointV1)
	if err != nil || applied {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin workspace agent provider checkpoint v1: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	columns := []struct {
		table      string
		name       string
		definition string
	}{
		{"workspace_agent_turns", "provider_checkpoint_message_id", "TEXT"},
		{"workspace_agent_session_fork_operations", "source_provider_checkpoint_message_id", "TEXT"},
		{"workspace_agent_session_fork_operations", "target_provider_checkpoint_message_id", "TEXT"},
	}
	for _, column := range columns {
		exists, err := hasColumnTx(ctx, tx, column.table, column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tx.ExecContext(
			ctx,
			`ALTER TABLE `+column.table+` ADD COLUMN `+column.name+` `+column.definition,
		); err != nil {
			return fmt.Errorf(
				"add %s.%s: %w",
				column.table,
				column.name,
				err,
			)
		}
	}
	if err := recordMigrationTx(
		ctx,
		tx,
		schemaMigrationWorkspaceAgentProviderCheckpointV1,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workspace agent provider checkpoint v1: %w", err)
	}
	return nil
}
