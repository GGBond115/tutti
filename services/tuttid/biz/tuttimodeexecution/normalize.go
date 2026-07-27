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
var ErrScheduleRejected = errors.New("Tutti mode schedule was rejected")
var ErrScheduleMutationConflict = errors.New("Tutti mode schedule request conflicts with durable history")

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
	aggregate := Aggregate{Execution: execution, Checkpoints: []Checkpoint{checkpoint}}
	if err := ValidateInitialAggregate(aggregate); err != nil {
		return Aggregate{}, err
	}
	return aggregate, nil
}

// ValidateInitialAggregate owns the execution-domain invariants for the inert
// state created when an accepted plan is materialized. Persistence adapters
// separately validate relations to their Issue and task rows.
func ValidateInitialAggregate(aggregate Aggregate) error {
	execution := aggregate.Execution
	expectedExecutionID, executionOK := ExecutionID(execution.IssueID)
	expectedCheckpointID, checkpointOK := InitialCheckpointID(expectedExecutionID)
	if strings.TrimSpace(execution.WorkspaceID) == "" ||
		strings.TrimSpace(execution.WorkflowID) == "" ||
		strings.TrimSpace(execution.SourceSessionID) == "" ||
		!executionOK || !checkpointOK ||
		execution.ID != expectedExecutionID ||
		execution.Status != StatusAwaitingSchedule ||
		execution.GraphRevision != 1 ||
		execution.ReviewMode != ReviewModeSelf ||
		len(aggregate.Checkpoints) != 1 {
		return ErrInvalidExecution
	}
	checkpoint := aggregate.Checkpoints[0]
	if checkpoint.ExecutionID != execution.ID ||
		checkpoint.ID != expectedCheckpointID ||
		checkpoint.ID != execution.ActiveCheckpointID ||
		checkpoint.Kind != CheckpointKindInitialSchedule ||
		checkpoint.Status != CheckpointStatusActive ||
		checkpoint.Sequence != 1 ||
		checkpoint.GraphRevision != execution.GraphRevision {
		return ErrInvalidExecution
	}
	return nil
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
