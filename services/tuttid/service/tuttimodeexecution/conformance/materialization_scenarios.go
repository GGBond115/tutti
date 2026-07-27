package conformance

import (
	"context"
	"fmt"
)

func runMaterializedPlanRequiresInitialSchedule(ctx context.Context, driver Driver) error {
	input := MaterializeAcceptedPlanInput{
		WorkspaceID:     "workspace-materialization",
		WorkflowID:      "workflow-materialization",
		RevisionID:      "revision-materialization",
		SourceSessionID: "session-materialization",
		TopicID:         "default",
		Title:           "Inert accepted plan",
		Content:         "The source plan body",
		TaskID:          "task-1",
		TaskTitle:       "Implement the plan",
		AgentTargetID:   "local:codex",
	}

	issueID, err := driver.MaterializeAcceptedPlan(ctx, input)
	if err != nil {
		return fmt.Errorf("MaterializeAcceptedPlan() error = %w", err)
	}
	if calls := driver.LauncherCallCount(); calls != 0 {
		return fmt.Errorf("launcher calls = %d, want 0", calls)
	}
	runCount, err := driver.CountRuns(ctx, input.WorkspaceID, issueID)
	if err != nil {
		return fmt.Errorf("CountRuns() error = %w", err)
	}
	if runCount != 0 {
		return fmt.Errorf("run count = %d, want 0", runCount)
	}

	execution, checkpoints, err := driver.GetExecutionByIssue(ctx, input.WorkspaceID, issueID)
	if err != nil {
		return fmt.Errorf("GetExecutionByIssue() error = %w", err)
	}
	if execution.Status != "awaiting_schedule" {
		return fmt.Errorf("execution status = %q, want awaiting_schedule", execution.Status)
	}
	if execution.GraphRevision != 1 {
		return fmt.Errorf("execution graph revision = %d, want 1", execution.GraphRevision)
	}
	if len(checkpoints) != 1 {
		return fmt.Errorf("checkpoint count = %d, want 1", len(checkpoints))
	}
	checkpoint := checkpoints[0]
	if checkpoint.Kind != "initial_schedule" || checkpoint.Status != "active" {
		return fmt.Errorf("initial checkpoint = %#v, want active initial_schedule", checkpoint)
	}
	if checkpoint.Sequence != 1 || checkpoint.GraphRevision != 1 {
		return fmt.Errorf("initial checkpoint revision/sequence = %#v, want 1/1", checkpoint)
	}
	return nil
}
