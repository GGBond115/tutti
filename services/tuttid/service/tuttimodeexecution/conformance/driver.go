package conformance

import "context"

type MaterializeAcceptedPlanInput struct {
	WorkspaceID     string
	WorkflowID      string
	RevisionID      string
	SourceSessionID string
	TopicID         string
	Title           string
	Content         string
	TaskID          string
	TaskTitle       string
	AgentTargetID   string
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

// Driver is the narrow public contract exercised by Tutti execution product
// conformance. Implementations may compose real services and persistence, but
// scenarios do not reach through this seam to implementation details.
type Driver interface {
	MaterializeAcceptedPlan(context.Context, MaterializeAcceptedPlanInput) (string, error)
	GetExecutionByIssue(context.Context, string, string) (Execution, []Checkpoint, error)
	CountRuns(context.Context, string, string) (int, error)
	LauncherCallCount() int
}
