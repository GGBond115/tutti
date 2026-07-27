package tuttimodeexecution_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
	workflowbiz "github.com/tutti-os/tutti/services/tuttid/biz/workspaceworkflow"
	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
	tuttimodeexecutionservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution"
	tuttimodeexecutionconformance "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution/conformance"
	tuttimodeplanservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeplan"
	workspaceservice "github.com/tutti-os/tutti/services/tuttid/service/workspace"
)

type sqliteConformanceDriver struct {
	store      *workspacedata.SQLiteStore
	issues     workspaceservice.IssueManagerService
	executions *tuttimodeexecutionservice.Service
	launcher   *recordingLauncher
}

type recordingLauncher struct {
	calls int
}

func (launcher *recordingLauncher) Launch(context.Context, workspaceservice.IssueRunLaunch) error {
	launcher.calls++
	return nil
}

func newSQLiteConformanceDriver(t *testing.T) *sqliteConformanceDriver {
	t.Helper()
	store, err := workspacedata.OpenSQLiteStore(filepath.Join(t.TempDir(), "tutti.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := store.Create(context.Background(), workspacebiz.Summary{
		ID:   "workspace-materialization",
		Name: "Materialization",
	}); err != nil {
		t.Fatalf("Create() workspace error = %v", err)
	}
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	if err := store.CreateWorkspaceWorkflowProposal(context.Background(), workflowbiz.ProposalAggregate{
		Workflow: workflowbiz.Workflow{
			ID:                "workflow-materialization",
			WorkspaceID:       "workspace-materialization",
			Type:              workflowbiz.WorkflowTypeTuttiModePlan,
			Owner:             workflowbiz.WorkflowOwnerTutti,
			TriggerKind:       workflowbiz.TriggerKindAgentCLI,
			SourceSessionID:   "session-materialization",
			Status:            workflowbiz.WorkflowStatusPendingReview,
			CurrentRevisionID: "revision-materialization",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		Plan: workflowbiz.TuttiModePlan{WorkflowID: "workflow-materialization"},
		Revision: workflowbiz.PlanRevision{
			ID:            "revision-materialization",
			WorkflowID:    "workflow-materialization",
			Sequence:      1,
			SchemaVersion: "tutti-mode-plan/v1",
			DocumentPath:  "plans/revision-materialization.md",
			SHA256:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt:     now,
		},
		Checkpoint: workflowbiz.WorkflowCheckpoint{
			ID:         "review-materialization",
			WorkflowID: "workflow-materialization",
			Kind:       workflowbiz.CheckpointKindTaskReview,
			RevisionID: "revision-materialization",
			Status:     workflowbiz.CheckpointStatusPending,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}); err != nil {
		t.Fatalf("CreateWorkspaceWorkflowProposal() error = %v", err)
	}
	launcher := &recordingLauncher{}
	executions := &tuttimodeexecutionservice.Service{
		Store: store,
		Clock: func() time.Time { return now },
	}
	return &sqliteConformanceDriver{
		store:      store,
		issues:     workspaceservice.IssueManagerService{Store: store, RunLauncher: launcher, TuttiModeExecutions: executions},
		executions: executions,
		launcher:   launcher,
	}
}

func (driver *sqliteConformanceDriver) MaterializeAcceptedPlan(
	ctx context.Context,
	input tuttimodeexecutionconformance.MaterializeAcceptedPlanInput,
) (string, error) {
	return (tuttimodeplanservice.WorkspaceIssueMaterializer{Issues: &driver.issues}).MaterializeIssue(
		ctx,
		tuttimodeplanservice.MaterializeIssueInput{
			WorkspaceID:     input.WorkspaceID,
			WorkflowID:      input.WorkflowID,
			RevisionID:      input.RevisionID,
			SourceSessionID: input.SourceSessionID,
			TopicID:         input.TopicID,
			Title:           input.Title,
			Content:         input.Content,
			Execution: tuttimodeplanservice.PlanExecution{
				Mode: "sequential",
			},
			Budget: tuttimodeplanservice.PlanBudget{Mode: "auto"},
			ActionableItems: []tuttimodeplanservice.ActionableItem{{
				Ordinal: 1,
				Task: tuttimodeplanservice.PlanTask{
					ID:            input.TaskID,
					Title:         input.TaskTitle,
					AgentTargetID: input.AgentTargetID,
				},
			}},
		},
	)
}

func (driver *sqliteConformanceDriver) GetExecutionByIssue(
	ctx context.Context,
	workspaceID string,
	issueID string,
) (tuttimodeexecutionconformance.Execution, []tuttimodeexecutionconformance.Checkpoint, error) {
	aggregate, err := driver.executions.GetByIssue(ctx, workspaceID, issueID)
	if err != nil {
		return tuttimodeexecutionconformance.Execution{}, nil, err
	}
	execution := tuttimodeexecutionconformance.Execution{
		WorkspaceID:     aggregate.Execution.WorkspaceID,
		IssueID:         aggregate.Execution.IssueID,
		WorkflowID:      aggregate.Execution.WorkflowID,
		SourceSessionID: aggregate.Execution.SourceSessionID,
		Status:          string(aggregate.Execution.Status),
		GraphRevision:   aggregate.Execution.GraphRevision,
	}
	checkpoints := make([]tuttimodeexecutionconformance.Checkpoint, 0, len(aggregate.Checkpoints))
	for _, checkpoint := range aggregate.Checkpoints {
		checkpoints = append(checkpoints, tuttimodeexecutionconformance.Checkpoint{
			CheckpointID:  checkpoint.ID,
			Kind:          string(checkpoint.Kind),
			Status:        string(checkpoint.Status),
			Sequence:      checkpoint.Sequence,
			GraphRevision: checkpoint.GraphRevision,
		})
	}
	return execution, checkpoints, nil
}

func (driver *sqliteConformanceDriver) CountRuns(ctx context.Context, workspaceID, issueID string) (int, error) {
	runs, err := driver.issues.ListRuns(ctx, workspaceID, issueID, "")
	return len(runs), err
}

func (driver *sqliteConformanceDriver) LauncherCallCount() int {
	return driver.launcher.calls
}

func TestMaterializationSQLiteServiceConformance(t *testing.T) {
	for _, scenario := range tuttimodeexecutionconformance.Catalog() {
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
