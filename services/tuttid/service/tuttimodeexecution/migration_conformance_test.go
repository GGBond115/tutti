package tuttimodeexecution_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	agentactivitybiz "github.com/tutti-os/tutti/services/tuttid/biz/agentactivity"
	tuttimodeexecutionconformance "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution/conformance"
	workspaceservice "github.com/tutti-os/tutti/services/tuttid/service/workspace"
)

func (driver *sqliteConformanceDriver) SeedLegacyExecution(
	ctx context.Context,
	input tuttimodeexecutionconformance.LegacyExecutionInput,
) (tuttimodeexecutionconformance.LegacyExecution, error) {
	issueID, err := driver.AcceptPlan(ctx, input.Plan)
	if err != nil {
		return tuttimodeexecutionconformance.LegacyExecution{}, err
	}
	switch input.SourceState {
	case "active", "tombstoned":
		if _, err := driver.store.ReportSessionState(ctx, agentactivitybiz.SessionStateReport{
			WorkspaceID:      input.Plan.WorkspaceID,
			AgentSessionID:   input.Plan.SourceSessionID,
			Provider:         "codex",
			Status:           "completed",
			OccurredAtUnixMS: driver.clock.Now().UnixMilli(),
		}); err != nil {
			return tuttimodeexecutionconformance.LegacyExecution{}, err
		}
		if input.SourceState == "tombstoned" {
			if _, err := driver.store.AgentCanonicalStore().DeleteSession(
				ctx, input.Plan.WorkspaceID, input.Plan.SourceSessionID,
			); err != nil {
				return tuttimodeexecutionconformance.LegacyExecution{}, err
			}
		}
	case "missing":
	default:
		return tuttimodeexecutionconformance.LegacyExecution{}, fmt.Errorf(
			"unsupported legacy source state %q", input.SourceState,
		)
	}

	legacyDB, err := sql.Open("sqlite", driver.dbPath)
	if err != nil {
		return tuttimodeexecutionconformance.LegacyExecution{}, err
	}
	legacyDB.SetMaxOpenConns(1)
	defer legacyDB.Close()
	if _, err := legacyDB.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return tuttimodeexecutionconformance.LegacyExecution{}, err
	}
	if _, err := legacyDB.ExecContext(ctx, `
DELETE FROM workspace_tutti_executions
WHERE workspace_id = ? AND issue_id = ?
`, input.Plan.WorkspaceID, issueID); err != nil {
		return tuttimodeexecutionconformance.LegacyExecution{}, err
	}

	result := tuttimodeexecutionconformance.LegacyExecution{IssueID: issueID}
	if input.RunningTaskID != "" {
		result.RunID = "legacy-run:" + issueID + ":" + input.RunningTaskID
		run, err := driver.issues.CreateRun(
			ctx,
			input.Plan.WorkspaceID,
			issueID,
			input.RunningTaskID,
			workspaceservice.CreateIssueManagerRunInput{
				RunID:          result.RunID,
				AgentTargetID:  "local:codex",
				AgentSessionID: "legacy-run-session:" + input.RunningTaskID,
			},
		)
		if err != nil {
			return tuttimodeexecutionconformance.LegacyExecution{}, err
		}
		result.RunID = run.RunID
	}
	return result, nil
}

func (driver *sqliteConformanceDriver) StartupRepairLegacyExecutions(
	ctx context.Context,
) error {
	legacyDB, err := sql.Open("sqlite", driver.dbPath)
	if err != nil {
		return err
	}
	legacyDB.SetMaxOpenConns(1)
	defer legacyDB.Close()
	if _, err := legacyDB.ExecContext(ctx, `
DELETE FROM tuttid_schema_migrations
WHERE id = 'workspace_tutti_mode_legacy_repair_v5'
`); err != nil {
		return err
	}
	return driver.store.Migrate(ctx)
}

func (driver *sqliteConformanceDriver) SourceSessionState(
	ctx context.Context,
	workspaceID string,
	sessionID string,
) (string, error) {
	if _, found, err := driver.store.GetSession(
		ctx, workspaceID, sessionID,
	); err != nil {
		return "", err
	} else if found {
		return "active", nil
	}
	deleted, err := driver.store.SessionDeleted(ctx, workspaceID, sessionID)
	if err != nil {
		return "", err
	}
	if deleted {
		return "tombstoned", nil
	}
	return "missing", nil
}

func TestMigrationSQLiteServiceConformance(t *testing.T) {
	for _, scenario := range tuttimodeexecutionconformance.MigrationCatalog() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			if err := tuttimodeexecutionconformance.Run(
				context.Background(),
				newSQLiteConformanceDriver(t),
				scenario,
			); err != nil {
				t.Fatal(err)
			}
		})
	}
}
