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
	AcceptanceState    string
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
	SubjectTaskID string
	SubjectRunID  string
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

type SettleRunInput struct {
	WorkspaceID string
	IssueID     string
	TaskID      string
	RunID       string
	Status      string
}

type AcknowledgeInput struct {
	WorkspaceID           string
	IssueID               string
	SourceSessionID       string
	CheckpointID          string
	ExpectedGraphRevision int64
	RequestID             string
}

type AcknowledgeResult struct {
	ExecutionID         string
	CheckpointID        string
	GraphRevision       int64
	NextCheckpointID    string
	NextCheckpointKind  string
	NextCheckpointState string
	Replayed            bool
}

type Wake struct {
	WakeID             string
	ExecutionID        string
	CheckpointID       string
	TargetKind         string
	WakeSequence       int64
	ClientSubmitID     string
	TargetSessionID    string
	CanonicalSessionID string
	CanonicalTurnID    string
	Status             string
	AttemptCount       int
	LeaseOwner         string
	DueAt              time.Time
	LeaseExpiresAt     time.Time
}

type WakeDelivery struct {
	TargetSessionID string
	ClientSubmitID  string
	Prompt          string
	HadDeadline     bool
	DeadlineBudget  time.Duration
}

// Driver is the narrow public contract exercised by Tutti execution product
// conformance. Implementations may compose real services and persistence, but
// scenarios do not reach through this seam to implementation details.
type Driver interface {
	AcceptPlan(context.Context, AcceptPlanInput) (string, error)
	GetSnapshot(context.Context, string, string) (Snapshot, error)
	Schedule(context.Context, ScheduleInput) (ScheduleResult, error)
	ScheduleReplica(context.Context, ScheduleInput) (ScheduleResult, error)
	SettleRun(context.Context, SettleRunInput) error
	SettleRunReplica(context.Context, SettleRunInput) error
	TimeoutRun(context.Context, SettleRunInput) error
	ClaimRunLaunchReplica(context.Context, string, string, string) (bool, error)
	FailNextLaunchAuthoritatively()
	FailNextCancellation()
	ReturnUnknownNextCancellation()
	HoldNextLaunchThenFailAuthoritatively() (<-chan struct{}, func())
	PersistTerminalRunWithoutCheckpoint(context.Context, SettleRunInput) error
	RepairSettlements(context.Context, string) error
	Acknowledge(context.Context, AcknowledgeInput) (AcknowledgeResult, error)
	AcknowledgeReplica(context.Context, AcknowledgeInput) (AcknowledgeResult, error)
	SeedActiveRun(context.Context, string, string, string) error
	FailNextLaunch()
	HoldNextLaunch() (<-chan struct{}, func())
	AdvanceClock(time.Duration) error
	StopLeaseRenewal()
	AdvanceClockWithoutRenewal(time.Duration)
	StartupRecoverReplica(context.Context, string) error
	StartupReconcileReplica(context.Context, string) error
	PeriodicRecoverReplica(context.Context, string) error
	RecoverLaunches(context.Context, string) error
	EnableAutomaticRecovery(context.Context)
	AwaitLauncherCalls(context.Context, int) error
	LauncherClientSubmitIDs() []string
	LauncherCanonicalTurnCount() int
	LauncherCallCount() int
	CancellationCallCount() int
	CancellationClientSubmitIDs() []string
	PreparedCancelCompensationCount(context.Context, string) (int, error)
	ListWakes(context.Context, string, string) ([]Wake, error)
	ClaimWake(context.Context, string, string, string, time.Duration) (bool, error)
	DispatchClaimedWake(context.Context, string, string, string) error
	DispatchClaimedWakeWithCallerCancellation(
		context.Context, string, string, string, string,
	) error
	RecoverWakes(context.Context, string, string) error
	// StartupRecoverWakes must construct a fresh execution service over the
	// same durable store before running startup recovery.
	StartupRecoverWakes(context.Context, string, string) error
	// SetSourceBusy changes only the exact workspace/session observation in
	// the canonical liveness port used by the production wake service; it must
	// not mutate a wake row directly.
	SetSourceBusy(string, string, bool)
	FailNextWakeBeforeCanonical()
	FailNextWakeAmbiguouslyBeforeCanonical()
	FailNextWakeAfterCanonical()
	FailNextWakeCanonicalLookup()
	SetMainWakeSendTimeout(time.Duration)
	HangWakeUntilContextDone(string, string)
	AdvanceClockDuringWake(string, string, time.Duration)
	FailWakeObservation(string, string)
	FailWakeClaim(string)
	SeedPreparedReviewerWake(context.Context, string, string) error
	CorruptWakeIdentity(context.Context, string, string, string, string) error
	// SettleWakeTurn changes the canonical Turn observation and then enters
	// the production wake reconciliation seam.
	SettleWakeTurn(context.Context, string, string, string) error
	SetExecutionStatus(context.Context, string, string, string) error
	CorruptWakeTargetSession(context.Context, string, string, string) error
	CurrentTime() time.Time
	WakeDeliveryCallCount() int
	WakeDeliveries() []WakeDelivery
	WakeDeliveryClientSubmitIDs() []string
	WakeDeliveryCanonicalTurnCount() int
}
