package conformance

import (
	"context"
	"time"
)

type AcceptPlanInput struct {
	WorkspaceID     string
	WorkflowID      string
	RevisionID      string
	CheckpointID    string
	SourceSessionID string
	TopicID         string
	Title           string
	Content         string
	BudgetMode      string
	TokenLimit      int64
	Tasks           []Task
}

type Issue struct {
	WorkspaceID     string
	IssueID         string
	TopicID         string
	Title           string
	Content         string
	PlanningSource  string
	SourceSessionID string
}

type Task struct {
	TaskID             string
	Title              string
	Content            string
	Status             string
	Priority           string
	SortIndex          int
	AgentTargetID      string
	Model              string
	PermissionModeID   string
	ExecutionDirectory string
	DependencyTaskIDs  []string
	Parallelizable     bool
	AutoAccept         bool
}

type RunSnapshot struct {
	RunID  string
	TaskID string
	Status string
}

type Execution struct {
	WorkspaceID     string
	IssueID         string
	WorkflowID      string
	SourceSessionID string
	Status          string
	GraphRevision   int64
}

type Checkpoint struct {
	CheckpointID  string
	Kind          string
	Status        string
	Sequence      int64
	GraphRevision int64
}

type Snapshot struct {
	Issue       Issue
	Tasks       []Task
	Execution   Execution
	Checkpoints []Checkpoint
	RunCount    int
	Runs        []RunSnapshot
}

type ScheduleInput struct {
	WorkspaceID           string
	IssueID               string
	SourceSessionID       string
	CheckpointID          string
	ExpectedGraphRevision int64
	TaskIDs               []string
	RequestID             string
}

type ScheduleResult struct {
	ExecutionID   string
	CheckpointID  string
	GraphRevision int64
	RunIDs        []string
	Replayed      bool
}

// Driver is the narrow public contract exercised by Tutti execution product
// conformance. Implementations may compose real services and persistence, but
// scenarios do not reach through this seam to implementation details.
type Driver interface {
	AcceptPlan(context.Context, AcceptPlanInput) (string, error)
	GetSnapshot(context.Context, string, string) (Snapshot, error)
	Schedule(context.Context, ScheduleInput) (ScheduleResult, error)
	ScheduleReplica(context.Context, ScheduleInput) (ScheduleResult, error)
	SeedActiveRun(context.Context, string, string, string) error
	FailNextLaunch()
	HoldNextLaunch() (<-chan struct{}, func())
	AdvanceClock(time.Duration) error
	StopLeaseRenewal()
	AdvanceClockWithoutRenewal(time.Duration)
	StartupRecoverReplica(context.Context, string) error
	PeriodicRecoverReplica(context.Context, string) error
	RecoverLaunches(context.Context, string) error
	EnableAutomaticRecovery(context.Context)
	AwaitLauncherCalls(context.Context, int) error
	LauncherClientSubmitIDs() []string
	LauncherCanonicalTurnCount() int
	LauncherCallCount() int
}
