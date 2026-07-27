// Package tuttimodeexecution defines Tutti-owned Issue orchestration state.
// Agent Session and Turn lifecycle facts remain owned by Agent Host.
package tuttimodeexecution

import "time"

type Status string

const (
	StatusAwaitingSchedule  Status = "awaiting_schedule"
	StatusRunning           Status = "running"
	StatusAwaitingMain      Status = "awaiting_main"
	StatusPendingGoalReview Status = "pending_goal_review"
	StatusOrphanedSource    Status = "orphaned_source"
	StatusCompleted         Status = "completed"
	StatusArchiving         Status = "archiving"
	StatusArchived          Status = "archived"
)

type ReviewMode string

const (
	ReviewModeSelf        ReviewMode = "self"
	ReviewModeIndependent ReviewMode = "independent"
)

type CheckpointKind string

const (
	CheckpointKindInitialSchedule  CheckpointKind = "initial_schedule"
	CheckpointKindTaskSettled      CheckpointKind = "task_settled"
	CheckpointKindTaskFailed       CheckpointKind = "task_failed"
	CheckpointKindTaskCanceled     CheckpointKind = "task_canceled"
	CheckpointKindWatchdog         CheckpointKind = "watchdog"
	CheckpointKindAllTasksTerminal CheckpointKind = "all_tasks_terminal"
	CheckpointKindMigration        CheckpointKind = "migration"
)

type CheckpointStatus string

const (
	CheckpointStatusPending    CheckpointStatus = "pending"
	CheckpointStatusActive     CheckpointStatus = "active"
	CheckpointStatusResolved   CheckpointStatus = "resolved"
	CheckpointStatusSuperseded CheckpointStatus = "superseded"
	CheckpointStatusCanceled   CheckpointStatus = "canceled"
)

type Execution struct {
	ID                         string
	WorkspaceID                string
	IssueID                    string
	WorkflowID                 string
	SourceSessionID            string
	Status                     Status
	GraphRevision              int64
	ActiveCheckpointID         string
	LastOrchestratorActivityAt time.Time
	WatchdogDueAt              time.Time
	ReviewMode                 ReviewMode
	ReviewAgentTargetID        string
	CompletedAt                time.Time
	ArchivedAt                 time.Time
	ArchivedBy                 string
	ArchiveReason              string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type Checkpoint struct {
	ID                 string
	ExecutionID        string
	Kind               CheckpointKind
	Status             CheckpointStatus
	Sequence           int64
	GraphRevision      int64
	SubjectTaskID      string
	SubjectRunID       string
	CreationReason     string
	RequiresGoalReview bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ResolvedAt         time.Time
}

type Aggregate struct {
	Execution   Execution
	Checkpoints []Checkpoint
}
