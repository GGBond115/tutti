package tuttimodeexecution

import (
	"errors"
	"strings"
	"time"
)

const WatchdogInterval = 5 * time.Minute

var ErrInvalidExecution = errors.New("invalid Tutti mode execution")
var ErrExecutionNotFound = errors.New("Tutti mode execution not found")
var ErrExecutionConflict = errors.New("Tutti mode execution conflicts with durable state")

func ExecutionID(issueID string) (string, bool) {
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return "", false
	}
	return "tutti-execution:" + issueID, true
}

func InitialCheckpointID(executionID string) (string, bool) {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return "", false
	}
	return executionID + ":checkpoint:initial-schedule", true
}

func NewInitialAggregate(
	workspaceID string,
	issueID string,
	workflowID string,
	sourceSessionID string,
	now time.Time,
) (Aggregate, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	workflowID = strings.TrimSpace(workflowID)
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	now = now.UTC()
	executionID, executionOK := ExecutionID(issueID)
	checkpointID, checkpointOK := InitialCheckpointID(executionID)
	if workspaceID == "" || workflowID == "" || sourceSessionID == "" ||
		now.IsZero() || !executionOK || !checkpointOK {
		return Aggregate{}, ErrInvalidExecution
	}
	execution := Execution{
		ID:                         executionID,
		WorkspaceID:                workspaceID,
		IssueID:                    issueID,
		WorkflowID:                 workflowID,
		SourceSessionID:            sourceSessionID,
		Status:                     StatusAwaitingSchedule,
		GraphRevision:              1,
		ActiveCheckpointID:         checkpointID,
		LastOrchestratorActivityAt: now,
		WatchdogDueAt:              now.Add(WatchdogInterval),
		ReviewMode:                 ReviewModeSelf,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	checkpoint := Checkpoint{
		ID:             checkpointID,
		ExecutionID:    executionID,
		Kind:           CheckpointKindInitialSchedule,
		Status:         CheckpointStatusActive,
		Sequence:       1,
		GraphRevision:  1,
		CreationReason: "accepted_plan_materialized",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return Aggregate{Execution: execution, Checkpoints: []Checkpoint{checkpoint}}, nil
}

func IsStatus(value Status) bool {
	switch value {
	case StatusAwaitingSchedule, StatusRunning, StatusAwaitingMain,
		StatusPendingGoalReview, StatusOrphanedSource, StatusCompleted,
		StatusArchiving, StatusArchived:
		return true
	default:
		return false
	}
}

func IsCheckpointKind(value CheckpointKind) bool {
	switch value {
	case CheckpointKindInitialSchedule, CheckpointKindTaskSettled,
		CheckpointKindTaskFailed, CheckpointKindTaskCanceled,
		CheckpointKindWatchdog, CheckpointKindAllTasksTerminal,
		CheckpointKindMigration:
		return true
	default:
		return false
	}
}

func IsCheckpointStatus(value CheckpointStatus) bool {
	switch value {
	case CheckpointStatusPending, CheckpointStatusActive,
		CheckpointStatusResolved, CheckpointStatusSuperseded,
		CheckpointStatusCanceled:
		return true
	default:
		return false
	}
}

func IsReviewMode(value ReviewMode) bool {
	return value == ReviewModeSelf || value == ReviewModeIndependent
}
