package conformance

import "context"

type AcceptPlanInput struct {
	WorkspaceID     string
	WorkflowID      string
	RevisionID      string
	CheckpointID    string
	SourceSessionID string
	TopicID         string
	Title           string
	Content         string
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
	TaskID            string
	Title             string
	Content           string
	Status            string
	Priority          string
	SortIndex         int
	AgentTargetID     string
	Model             string
	PermissionModeID  string
	DependencyTaskIDs []string
	Parallelizable    bool
	AutoAccept        bool
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
}

// Driver is the narrow public contract exercised by Tutti execution product
// conformance. Implementations may compose real services and persistence, but
// scenarios do not reach through this seam to implementation details.
type Driver interface {
	AcceptPlan(context.Context, AcceptPlanInput) (string, error)
	GetSnapshot(context.Context, string, string) (Snapshot, error)
	LauncherCallCount() int
}
