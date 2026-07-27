package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	workspaceissues "github.com/tutti-os/tutti/packages/workspace/issues"
	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
	workflowbiz "github.com/tutti-os/tutti/services/tuttid/biz/workspaceworkflow"
	executionservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution"
)

var tuttiModeExecutionTables = []string{
	"workspace_tutti_executions",
	"workspace_tutti_execution_checkpoints",
	"workspace_tutti_execution_wakes",
	"workspace_tutti_goal_reviews",
	"workspace_tutti_archive_operations",
	"workspace_tutti_execution_mutations",
	"workspace_source_session_deletion_admissions",
	"workspace_issue_run_launch_intents",
}

func TestTuttiModeExecutionMigrationCreatesForwardCompatibleSchema(t *testing.T) {
	t.Parallel()
	store := openTuttiModeExecutionStore(t)
	ctx := context.Background()

	for _, table := range tuttiModeExecutionTables {
		var count int
		if err := store.writeDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?
`, table).Scan(&count); err != nil {
			t.Fatalf("inspect table %q error = %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %q count = %d, want 1", table, count)
		}
	}
	for table, columns := range map[string][]string{
		"workspace_tutti_executions": {
			"graph_revision", "watchdog_due_at_unix_ms", "last_orchestrator_activity_at_unix_ms",
		},
		"workspace_tutti_execution_wakes": {
			"lease_owner", "lease_expires_at_unix_ms", "attempt_count", "client_submit_id",
		},
		"workspace_tutti_archive_operations": {
			"lease_owner", "lease_expires_at_unix_ms", "attempt_count",
		},
		"workspace_issue_run_launch_intents": {
			"lease_owner", "lease_expires_at_unix_ms", "attempt_count", "client_submit_id",
		},
	} {
		for _, column := range columns {
			if !sqliteTableHasColumn(t, store, table, column) {
				t.Fatalf("table %q missing forward-compatible column %q", table, column)
			}
		}
	}
}

func TestTuttiModeExecutionMigrationUpgradesExistingWorkspaceDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "upgrade.sqlite")
	store, err := OpenSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(initial) error = %v", err)
	}
	if err := store.Create(ctx, workspacebiz.Summary{ID: "workspace-upgrade", Name: "Upgrade"}); err != nil {
		t.Fatalf("Create() workspace error = %v", err)
	}
	if err := dropTuttiModeExecutionMigrationForUpgradeTest(ctx, store); err != nil {
		t.Fatalf("prepare pre-execution schema error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(initial) error = %v", err)
	}

	upgraded, err := OpenSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLiteStore(upgrade) error = %v", err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	if err := upgraded.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(upgrade) error = %v", err)
	}
	if _, err := upgraded.Get(ctx, "workspace-upgrade"); err != nil {
		t.Fatalf("Get() preserved workspace error = %v", err)
	}
	for _, table := range tuttiModeExecutionTables {
		var count int
		if err := upgraded.writeDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?
`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("upgraded table %q count=%d error=%v", table, count, err)
		}
	}
}

func TestTuttiModeExecutionMaterializationIsAtomicAndReadable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openTuttiModeExecutionStore(t)
	now := time.UnixMilli(1_700_000_000_000).UTC()
	prepareTuttiModeExecutionWorkspace(t, store, "workspace-atomic", "workflow-atomic", "session-atomic", now)
	executions := &executionservice.Service{Store: store, Clock: func() time.Time { return now }}
	issues := workspaceissues.Service{Store: store, Clock: func() time.Time { return now }}
	issue, tasks := prepareTuttiModeIssueGraph(t, issues, "workspace-atomic", "workflow-atomic", "session-atomic")
	createdIssue, _, _, err := executions.Materialize(ctx, executionservice.MaterializeInput{
		Issue: issue, Tasks: tasks, WorkflowID: "workflow-atomic",
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	aggregate, err := executions.GetByIssue(ctx, issue.WorkspaceID, createdIssue.IssueID)
	if err != nil {
		t.Fatalf("GetByIssue() error = %v", err)
	}
	if aggregate.Execution.Status != executionbiz.StatusAwaitingSchedule ||
		aggregate.Execution.GraphRevision != 1 ||
		len(aggregate.Checkpoints) != 1 ||
		aggregate.Checkpoints[0].Kind != executionbiz.CheckpointKindInitialSchedule ||
		aggregate.Checkpoints[0].Status != executionbiz.CheckpointStatusActive {
		t.Fatalf("materialized aggregate = %#v", aggregate)
	}
	detail, err := issues.GetIssueDetail(ctx, issue.WorkspaceID, createdIssue.IssueID)
	if err != nil {
		t.Fatalf("GetIssueDetail() error = %v", err)
	}
	if len(detail.Tasks) != 1 || detail.Tasks[0].Status != workspaceissues.StatusNotStarted {
		t.Fatalf("materialized tasks = %#v", detail.Tasks)
	}
	runs, err := issues.ListRuns(ctx, issue.WorkspaceID, createdIssue.IssueID, "")
	if err != nil || len(runs) != 0 {
		t.Fatalf("materialized runs = %#v error=%v, want none", runs, err)
	}

	_, err = store.writeDB.ExecContext(ctx, `
INSERT INTO workspace_tutti_execution_checkpoints (
  workspace_id, execution_id, checkpoint_id, kind, status, sequence,
  graph_revision, creation_reason, created_at_unix_ms, updated_at_unix_ms
) VALUES (?, ?, ?, 'watchdog', 'active', 2, 1, 'duplicate active', ?, ?)
`, issue.WorkspaceID, aggregate.Execution.ID, "duplicate-active", now.UnixMilli(), now.UnixMilli())
	if err == nil {
		t.Fatal("duplicate active checkpoint insertion succeeded, want unique constraint")
	}
}

func TestTuttiModeExecutionMaterializationRejectsDuplicateAndPreservesOriginal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openTuttiModeExecutionStore(t)
	now := time.UnixMilli(1_700_000_100_000).UTC()
	prepareTuttiModeExecutionWorkspace(t, store, "workspace-replay", "workflow-replay", "session-replay", now)
	executions := &executionservice.Service{Store: store, Clock: func() time.Time { return now }}
	issues := workspaceissues.Service{Store: store, Clock: func() time.Time { return now }}
	issue, tasks := prepareTuttiModeIssueGraph(t, issues, "workspace-replay", "workflow-replay", "session-replay")

	firstIssue, _, firstAggregate, err := executions.Materialize(ctx, executionservice.MaterializeInput{
		Issue: issue, Tasks: tasks, WorkflowID: "workflow-replay",
	})
	if err != nil {
		t.Fatalf("Materialize(first) error = %v", err)
	}
	if _, _, _, err := executions.Materialize(ctx, executionservice.MaterializeInput{
		Issue: issue, Tasks: tasks, WorkflowID: "workflow-replay",
	}); !errors.Is(err, workspaceissues.ErrIssueAlreadyExists) {
		t.Fatalf("Materialize(duplicate) error = %v, want ErrIssueAlreadyExists", err)
	}
	var issueCount, executionCount, checkpointCount int
	if err := store.writeDB.QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(*) FROM workspace_issues WHERE workspace_id = ? AND issue_id = ?),
  (SELECT COUNT(*) FROM workspace_tutti_executions WHERE workspace_id = ? AND issue_id = ?),
  (SELECT COUNT(*) FROM workspace_tutti_execution_checkpoints WHERE workspace_id = ?)
`, issue.WorkspaceID, firstIssue.IssueID, issue.WorkspaceID, firstIssue.IssueID, issue.WorkspaceID).
		Scan(&issueCount, &executionCount, &checkpointCount); err != nil {
		t.Fatalf("count replay rows error = %v", err)
	}
	if issueCount != 1 || executionCount != 1 || checkpointCount != 1 {
		t.Fatalf("replay row counts issue=%d execution=%d checkpoint=%d, want 1/1/1", issueCount, executionCount, checkpointCount)
	}
	persisted, err := executions.GetByIssue(ctx, issue.WorkspaceID, issue.IssueID)
	if err != nil || persisted.Execution.ID != firstAggregate.Execution.ID {
		t.Fatalf("GetByIssue() after duplicate aggregate=%#v error=%v", persisted, err)
	}
}

func TestTuttiModeExecutionMaterializationRollsBackIssueOnExecutionFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openTuttiModeExecutionStore(t)
	now := time.UnixMilli(1_700_000_200_000).UTC()
	if err := store.Create(ctx, workspacebiz.Summary{ID: "workspace-rollback", Name: "Rollback"}); err != nil {
		t.Fatalf("Create() workspace error = %v", err)
	}
	executions := &executionservice.Service{Store: store, Clock: func() time.Time { return now }}
	issues := workspaceissues.Service{Store: store, Clock: func() time.Time { return now }}
	issue, tasks := prepareTuttiModeIssueGraph(t, issues, "workspace-rollback", "missing-workflow", "session-rollback")

	_, _, _, err := executions.Materialize(ctx, executionservice.MaterializeInput{
		Issue: issue, Tasks: tasks, WorkflowID: "missing-workflow",
	})
	if err == nil {
		t.Fatal("Materialize() error = nil, want missing workflow foreign-key failure")
	}
	if _, err := store.GetIssue(ctx, issue.WorkspaceID, issue.IssueID); !errors.Is(err, workspaceissues.ErrIssueNotFound) {
		t.Fatalf("GetIssue() after rollback error = %v, want ErrIssueNotFound", err)
	}
	if _, err := executions.GetByIssue(ctx, issue.WorkspaceID, issue.IssueID); !errors.Is(err, executionbiz.ErrExecutionNotFound) {
		t.Fatalf("GetByIssue() after rollback error = %v, want ErrExecutionNotFound", err)
	}
}

func openTuttiModeExecutionStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "tutti.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func prepareTuttiModeExecutionWorkspace(
	t *testing.T,
	store *SQLiteStore,
	workspaceID string,
	workflowID string,
	sourceSessionID string,
	now time.Time,
) {
	t.Helper()
	ctx := context.Background()
	if err := store.Create(ctx, workspacebiz.Summary{ID: workspaceID, Name: workspaceID}); err != nil {
		t.Fatalf("Create() workspace error = %v", err)
	}
	revisionID := "revision-" + workflowID
	if err := store.CreateWorkspaceWorkflowProposal(ctx, workflowbiz.ProposalAggregate{
		Workflow: workflowbiz.Workflow{
			ID:                workflowID,
			WorkspaceID:       workspaceID,
			Type:              workflowbiz.WorkflowTypeTuttiModePlan,
			Owner:             workflowbiz.WorkflowOwnerTutti,
			TriggerKind:       workflowbiz.TriggerKindAgentCLI,
			SourceSessionID:   sourceSessionID,
			Status:            workflowbiz.WorkflowStatusPendingReview,
			CurrentRevisionID: revisionID,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		Plan: workflowbiz.TuttiModePlan{WorkflowID: workflowID},
		Revision: workflowbiz.PlanRevision{
			ID:            revisionID,
			WorkflowID:    workflowID,
			Sequence:      1,
			SchemaVersion: "tutti-mode-plan/v1",
			DocumentPath:  "plans/" + revisionID + ".md",
			SHA256:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt:     now,
		},
		Checkpoint: workflowbiz.WorkflowCheckpoint{
			ID:         "review-" + workflowID,
			WorkflowID: workflowID,
			Kind:       workflowbiz.CheckpointKindTaskReview,
			RevisionID: revisionID,
			Status:     workflowbiz.CheckpointStatusPending,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}); err != nil {
		t.Fatalf("CreateWorkspaceWorkflowProposal() error = %v", err)
	}
}

func prepareTuttiModeIssueGraph(
	t *testing.T,
	issues workspaceissues.Service,
	workspaceID string,
	workflowID string,
	sourceSessionID string,
) (workspaceissues.Issue, []workspaceissues.Task) {
	t.Helper()
	issueID, ok := workflowbiz.TuttiModePlanIssueID(workflowID)
	if !ok {
		t.Fatal("TuttiModePlanIssueID() rejected fixture")
	}
	issue, tasks, err := issues.PrepareIssueWithTasks(context.Background(), workspaceissues.CreateIssueWithTasksInput{
		Issue: workspaceissues.CreateIssueInput{
			IssueID:             issueID,
			TopicID:             workspaceissues.DefaultTopicID,
			WorkspaceID:         workspaceID,
			ActorUserID:         "local",
			Title:               "Materialized execution",
			Content:             "Accepted plan",
			PlanningSource:      string(workspaceissues.PlanningSourceTuttiModePlan),
			SourceSessionID:     sourceSessionID,
			SequentialExecution: true,
			HasBudget:           true,
			Budget:              workspaceissues.Budget{Mode: workspaceissues.BudgetModeAuto},
		},
		Tasks: []workspaceissues.CreateTaskItemInput{{TaskID: "task-1", Title: "Implement"}},
	})
	if err != nil {
		t.Fatalf("PrepareIssueWithTasks() error = %v", err)
	}
	return issue, tasks
}

func sqliteTableHasColumn(t *testing.T, store *SQLiteStore, table, column string) bool {
	t.Helper()
	rows, err := store.writeDB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%q) error = %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan table info %q error = %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info %q error = %v", table, err)
	}
	return false
}

func dropTuttiModeExecutionMigrationForUpgradeTest(ctx context.Context, store *SQLiteStore) error {
	_, err := store.writeDB.ExecContext(ctx, `
DROP TABLE workspace_issue_run_launch_intents;
DROP TABLE workspace_source_session_deletion_admissions;
DROP TABLE workspace_tutti_execution_mutations;
DROP TABLE workspace_tutti_archive_operations;
DROP TABLE workspace_tutti_goal_reviews;
DROP TABLE workspace_tutti_execution_wakes;
DROP TABLE workspace_tutti_execution_checkpoints;
DROP TABLE workspace_tutti_executions;
DELETE FROM tuttid_schema_migrations WHERE id = ?;
`, schemaMigrationWorkspaceTuttiModeExecutionV1)
	return err
}
