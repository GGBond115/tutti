package tuttimodeexecution_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	workspaceissues "github.com/tutti-os/tutti/packages/workspace/issues"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
	workflowbiz "github.com/tutti-os/tutti/services/tuttid/biz/workspaceworkflow"
	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
	tuttimodeexecutionservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution"
	tuttimodeexecutionconformance "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution/conformance"
	tuttimodeplanservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeplan"
	workspaceservice "github.com/tutti-os/tutti/services/tuttid/service/workspace"
	"gopkg.in/yaml.v3"
)

type sqliteConformanceDriver struct {
	store      *workspacedata.SQLiteStore
	issues     workspaceservice.IssueManagerService
	executions *tuttimodeexecutionservice.Service
	plans      *tuttimodeplanservice.Service
	revisions  workspacedata.WorkflowRevisionFiles
	now        time.Time
	launcher   *recordingLauncher
}

type recordingLauncher struct {
	mu                      sync.Mutex
	calls                   int
	failNext                bool
	clientSubmitIDs         []string
	canonicalByClientSubmit map[string]string
	blockNext               bool
	started                 chan struct{}
	release                 chan struct{}
}

func (launcher *recordingLauncher) Launch(_ context.Context, launch workspaceservice.IssueRunLaunch) error {
	launcher.mu.Lock()
	launcher.calls++
	launcher.clientSubmitIDs = append(launcher.clientSubmitIDs, launch.ClientSubmitID)
	if launcher.canonicalByClientSubmit == nil {
		launcher.canonicalByClientSubmit = make(map[string]string)
	}
	if _, exists := launcher.canonicalByClientSubmit[launch.ClientSubmitID]; !exists {
		launcher.canonicalByClientSubmit[launch.ClientSubmitID] = fmt.Sprintf("turn-%d", len(launcher.canonicalByClientSubmit)+1)
	}
	fail := launcher.failNext
	if launcher.failNext {
		launcher.failNext = false
	}
	block := launcher.blockNext
	started := launcher.started
	release := launcher.release
	launcher.blockNext = false
	launcher.mu.Unlock()
	if block {
		close(started)
		<-release
	}
	if fail {
		return fmt.Errorf("injected launch failure after canonical Turn creation")
	}
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
	launcher := &recordingLauncher{}
	executions := &tuttimodeexecutionservice.Service{
		Store: store,
		Clock: func() time.Time { return now },
	}
	driver := &sqliteConformanceDriver{
		store: store,
		issues: workspaceservice.IssueManagerService{
			Store: store, RunLauncher: launcher, TuttiModeExecutions: executions,
			MutationLocks: workspaceservice.NewIssueMutationLocks(),
		},
		executions: executions,
		revisions:  workspacedata.WorkflowRevisionFiles{StateDir: t.TempDir()},
		now:        now,
		launcher:   launcher,
	}
	driver.plans = &tuttimodeplanservice.Service{
		Store:             store,
		Revisions:         driver.revisions,
		IssueMaterializer: tuttimodeplanservice.WorkspaceIssueMaterializer{Issues: &driver.issues},
		Now:               func() time.Time { return now },
	}
	return driver
}

func (driver *sqliteConformanceDriver) AcceptPlan(
	ctx context.Context,
	input tuttimodeexecutionconformance.AcceptPlanInput,
) (string, error) {
	tasks := make([]tuttimodeplanservice.PlanTask, 0, len(input.Tasks))
	for _, task := range input.Tasks {
		tasks = append(tasks, tuttimodeplanservice.PlanTask{
			ID:                 task.TaskID,
			Title:              task.Title,
			Content:            task.Content,
			Priority:           task.Priority,
			AgentTargetID:      task.AgentTargetID,
			Model:              task.Model,
			PermissionModeID:   task.PermissionModeID,
			ExecutionDirectory: task.ExecutionDirectory,
			DependsOn:          append([]string(nil), task.DependencyTaskIDs...),
			Parallelizable:     task.Parallelizable,
			AutoAccept:         task.AutoAccept,
		})
	}
	frontmatter, err := yaml.Marshal(tuttimodeplanservice.PlanDocument{
		Schema:  tuttimodeplanservice.SchemaV1,
		Phase:   tuttimodeplanservice.PhaseTaskGraph,
		Title:   input.Title,
		TopicID: input.TopicID,
		Execution: tuttimodeplanservice.PlanExecution{
			Mode:                   "sequential",
			ReasoningIntensity:     50,
			OrchestrationIntensity: 50,
		},
		Budget: tuttimodeplanservice.PlanBudget{
			Mode:       firstNonEmptyConformance(input.BudgetMode, "auto"),
			TokenLimit: input.TokenLimit,
		},
		Tasks: tasks,
	})
	if err != nil {
		return "", fmt.Errorf("encode plan revision: %w", err)
	}
	raw := []byte("---\n" + string(frontmatter) + "---\n" + input.Content + "\n")
	documentPath, digest, err := driver.revisions.Write(input.WorkflowID, raw)
	if err != nil {
		return "", fmt.Errorf("write plan revision: %w", err)
	}
	if err := driver.store.CreateWorkspaceWorkflowProposal(ctx, workflowbiz.ProposalAggregate{
		Workflow: workflowbiz.Workflow{
			ID:                input.WorkflowID,
			WorkspaceID:       input.WorkspaceID,
			Type:              workflowbiz.WorkflowTypeTuttiModePlan,
			Owner:             workflowbiz.WorkflowOwnerTutti,
			TriggerKind:       workflowbiz.TriggerKindAgentCLI,
			SourceSessionID:   input.SourceSessionID,
			Status:            workflowbiz.WorkflowStatusPendingReview,
			CurrentRevisionID: input.RevisionID,
			CreatedAt:         driver.now,
			UpdatedAt:         driver.now,
		},
		Plan: workflowbiz.TuttiModePlan{WorkflowID: input.WorkflowID},
		Revision: workflowbiz.PlanRevision{
			ID:            input.RevisionID,
			WorkflowID:    input.WorkflowID,
			Sequence:      1,
			SchemaVersion: tuttimodeplanservice.SchemaV1,
			DocumentPath:  documentPath,
			SHA256:        digest,
			CreatedAt:     driver.now,
		},
		Checkpoint: workflowbiz.WorkflowCheckpoint{
			ID:         input.CheckpointID,
			WorkflowID: input.WorkflowID,
			Kind:       workflowbiz.CheckpointKindTaskReview,
			RevisionID: input.RevisionID,
			Status:     workflowbiz.CheckpointStatusPending,
			CreatedAt:  driver.now,
			UpdatedAt:  driver.now,
		},
	}); err != nil {
		return "", fmt.Errorf("seed pending plan review: %w", err)
	}
	result, err := driver.plans.Decide(ctx, tuttimodeplanservice.DecideInput{
		WorkspaceID:  input.WorkspaceID,
		WorkflowID:   input.WorkflowID,
		CheckpointID: input.CheckpointID,
		Decision:     workflowbiz.CheckpointStatusAccepted,
		DecidedBy:    "conformance-user",
	})
	if err != nil {
		return "", err
	}
	if result.Operation == nil ||
		result.Operation.Status != workflowbiz.OperationStatusSucceeded ||
		result.Operation.IssueID == "" {
		return "", fmt.Errorf("accept plan operation = %#v, want succeeded create_issue", result.Operation)
	}
	return result.Operation.IssueID, nil
}

func (driver *sqliteConformanceDriver) GetIssueByID(
	ctx context.Context,
	workspaceID string,
	issueID string,
) (tuttimodeexecutionconformance.Issue, []tuttimodeexecutionconformance.Task, error) {
	detail, err := driver.issues.GetIssueDetail(ctx, workspaceID, issueID)
	if err != nil {
		return tuttimodeexecutionconformance.Issue{}, nil, err
	}
	issue := tuttimodeexecutionconformance.Issue{
		WorkspaceID:     detail.Issue.WorkspaceID,
		IssueID:         detail.Issue.IssueID,
		TopicID:         detail.Issue.TopicID,
		Title:           detail.Issue.Title,
		Content:         detail.Issue.Content,
		PlanningSource:  string(detail.Issue.PlanningSource),
		SourceSessionID: detail.Issue.SourceSessionID,
	}
	tasks := make([]tuttimodeexecutionconformance.Task, 0, len(detail.Tasks))
	for _, task := range detail.Tasks {
		tasks = append(tasks, tuttimodeexecutionconformance.Task{
			TaskID:             task.TaskID,
			Title:              task.Title,
			Content:            task.Content,
			Status:             string(task.Status),
			Priority:           string(task.Priority),
			SortIndex:          task.SortIndex,
			AgentTargetID:      task.AgentTargetID,
			Model:              task.Model,
			PermissionModeID:   task.PermissionModeID,
			ExecutionDirectory: task.ExecutionDirectory,
			DependencyTaskIDs:  append([]string(nil), task.DependencyTaskIDs...),
			Parallelizable:     task.Parallelizable,
			AutoAccept:         task.AutoAccept,
		})
	}
	return issue, tasks, nil
}

func (driver *sqliteConformanceDriver) GetSnapshot(
	ctx context.Context,
	workspaceID string,
	issueID string,
) (tuttimodeexecutionconformance.Snapshot, error) {
	issue, tasks, err := driver.GetIssueByID(ctx, workspaceID, issueID)
	if err != nil {
		return tuttimodeexecutionconformance.Snapshot{}, err
	}
	execution, checkpoints, err := driver.GetExecutionByIssue(ctx, workspaceID, issueID)
	if err != nil {
		return tuttimodeexecutionconformance.Snapshot{}, err
	}
	runCount, err := driver.CountRuns(ctx, workspaceID, issueID)
	if err != nil {
		return tuttimodeexecutionconformance.Snapshot{}, err
	}
	runs, err := driver.issues.ListRuns(ctx, workspaceID, issueID, "")
	if err != nil {
		return tuttimodeexecutionconformance.Snapshot{}, err
	}
	snapshotRuns := make([]tuttimodeexecutionconformance.RunSnapshot, 0, len(runs))
	for _, run := range runs {
		snapshotRuns = append(snapshotRuns, tuttimodeexecutionconformance.RunSnapshot{
			RunID: run.RunID, TaskID: run.TaskID, Status: string(run.Status),
		})
	}
	return tuttimodeexecutionconformance.Snapshot{
		Issue:       issue,
		Tasks:       tasks,
		Execution:   execution,
		Checkpoints: checkpoints,
		RunCount:    runCount,
		Runs:        snapshotRuns,
	}, nil
}

func (driver *sqliteConformanceDriver) Schedule(
	ctx context.Context,
	input tuttimodeexecutionconformance.ScheduleInput,
) (tuttimodeexecutionconformance.ScheduleResult, error) {
	result, err := driver.issues.ScheduleTuttiModeIssue(ctx, input.WorkspaceID, workspaceservice.ScheduleTuttiModeIssueInput{
		IssueID:               input.IssueID,
		SourceSessionID:       input.SourceSessionID,
		CheckpointID:          input.CheckpointID,
		ExpectedGraphRevision: input.ExpectedGraphRevision,
		TaskIDs:               append([]string(nil), input.TaskIDs...),
		RequestID:             input.RequestID,
	})
	if err != nil {
		return tuttimodeexecutionconformance.ScheduleResult{}, err
	}
	return tuttimodeexecutionconformance.ScheduleResult{
		ExecutionID:   result.ExecutionID,
		CheckpointID:  result.CheckpointID,
		GraphRevision: result.GraphRevision,
		RunIDs:        append([]string(nil), result.RunIDs...),
		Replayed:      result.Replayed,
	}, nil
}

func (driver *sqliteConformanceDriver) ScheduleReplica(
	ctx context.Context,
	input tuttimodeexecutionconformance.ScheduleInput,
) (tuttimodeexecutionconformance.ScheduleResult, error) {
	replica := driver.issues
	replica.MutationLocks = workspaceservice.NewIssueMutationLocks()
	replica.RunLaunchGate = workspaceservice.NewIssueRunLaunchGate()
	result, err := replica.ScheduleTuttiModeIssue(ctx, input.WorkspaceID, workspaceservice.ScheduleTuttiModeIssueInput{
		IssueID:               input.IssueID,
		SourceSessionID:       input.SourceSessionID,
		CheckpointID:          input.CheckpointID,
		ExpectedGraphRevision: input.ExpectedGraphRevision,
		TaskIDs:               append([]string(nil), input.TaskIDs...),
		RequestID:             input.RequestID,
	})
	if err != nil {
		return tuttimodeexecutionconformance.ScheduleResult{}, err
	}
	return tuttimodeexecutionconformance.ScheduleResult{
		ExecutionID: result.ExecutionID, CheckpointID: result.CheckpointID,
		GraphRevision: result.GraphRevision, RunIDs: append([]string(nil), result.RunIDs...),
		Replayed: result.Replayed,
	}, nil
}

func (driver *sqliteConformanceDriver) SeedActiveRun(
	ctx context.Context,
	workspaceID string,
	issueID string,
	taskID string,
) error {
	task, err := driver.store.GetTask(ctx, workspaceID, issueID, taskID)
	if err != nil {
		return err
	}
	now := driver.now.UnixMilli()
	run, err := driver.store.CreateRun(ctx, workspaceissues.Run{
		RunID: "seed-run-" + taskID, TaskID: taskID, IssueID: issueID,
		WorkspaceID: workspaceID, RequesterUserID: "conformance",
		AgentUserID: "conformance", AgentTargetID: task.AgentTargetID,
		AgentSessionID: "seed-session-" + taskID, Status: workspaceissues.StatusRunning,
		CreatedAtUnixMS: now, StartedAtUnixMS: now, UpdatedAtUnixMS: now,
	})
	if err != nil {
		return err
	}
	task.Status = workspaceissues.StatusRunning
	task.LatestRunID = run.RunID
	task.UpdatedAtUnixMS = now
	if _, err := driver.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	_, err = driver.store.RecalculateIssueProjection(ctx, workspaceID, issueID)
	return err
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
	driver.launcher.mu.Lock()
	defer driver.launcher.mu.Unlock()
	return driver.launcher.calls
}

func (driver *sqliteConformanceDriver) FailNextLaunch() {
	driver.launcher.mu.Lock()
	defer driver.launcher.mu.Unlock()
	driver.launcher.failNext = true
}

func (driver *sqliteConformanceDriver) HoldNextLaunch() (<-chan struct{}, func()) {
	driver.launcher.mu.Lock()
	defer driver.launcher.mu.Unlock()
	driver.launcher.blockNext = true
	driver.launcher.started = make(chan struct{})
	driver.launcher.release = make(chan struct{})
	var once sync.Once
	return driver.launcher.started, func() {
		once.Do(func() { close(driver.launcher.release) })
	}
}

func (driver *sqliteConformanceDriver) RecoverLaunches(ctx context.Context, workspaceID string) error {
	return driver.issues.RecoverTuttiModeRunLaunchIntents(ctx, workspaceID)
}

func (driver *sqliteConformanceDriver) LauncherClientSubmitIDs() []string {
	driver.launcher.mu.Lock()
	defer driver.launcher.mu.Unlock()
	return append([]string(nil), driver.launcher.clientSubmitIDs...)
}

func (driver *sqliteConformanceDriver) LauncherCanonicalTurnCount() int {
	driver.launcher.mu.Lock()
	defer driver.launcher.mu.Unlock()
	return len(driver.launcher.canonicalByClientSubmit)
}

func TestMaterializationSQLiteServiceConformance(t *testing.T) {
	for _, scenario := range tuttimodeexecutionconformance.MaterializationCatalog() {
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

func TestScheduleSQLiteServiceConformance(t *testing.T) {
	for _, scenario := range tuttimodeexecutionconformance.ScheduleCatalog() {
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

func firstNonEmptyConformance(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
