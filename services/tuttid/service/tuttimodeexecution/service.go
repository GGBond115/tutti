package tuttimodeexecution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	workspaceissues "github.com/tutti-os/tutti/packages/workspace/issues"
	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
)

var ErrServiceUnavailable = errors.New("Tutti mode execution service is unavailable")
var ErrExecutionNotFound = executionbiz.ErrExecutionNotFound
var ErrExecutionConflict = executionbiz.ErrExecutionConflict
var ErrScheduleRejected = executionbiz.ErrScheduleRejected
var ErrScheduleMutationConflict = executionbiz.ErrScheduleMutationConflict

type Store interface {
	MaterializeTuttiModeIssue(
		context.Context,
		workspaceissues.Issue,
		[]workspaceissues.Task,
		executionbiz.Aggregate,
	) (workspaceissues.Issue, []workspaceissues.Task, executionbiz.Aggregate, error)
	GetTuttiModeExecutionByIssue(context.Context, string, string) (executionbiz.Aggregate, error)
	AdmitTuttiModeSchedule(context.Context, executionbiz.ScheduleAdmission) (executionbiz.ScheduleResult, error)
	ListPreparedTuttiModeRunLaunches(context.Context, string, string, []string, time.Time) ([]executionbiz.PreparedRunLaunch, error)
	GetTuttiModeRunLaunchClientSubmitID(context.Context, string, string, string) (string, bool, error)
	ClaimTuttiModeRunLaunchIntent(context.Context, string, string, string, string, time.Time, time.Time) (bool, error)
	RenewTuttiModeRunLaunchIntent(context.Context, string, string, string, string, time.Time, time.Time) error
	ReleaseTuttiModeRunLaunchIntent(context.Context, string, string, string, string, time.Time) error
	MarkTuttiModeRunLaunchIntentDispatched(context.Context, string, string, string, string, time.Time) error
	RequeueLeasedTuttiModeRunLaunchIntents(context.Context, string, time.Time) error
}

type Service struct {
	Store Store
	Clock func() time.Time
}

type MaterializeInput struct {
	Issue      workspaceissues.Issue
	Tasks      []workspaceissues.Task
	WorkflowID string
}

type ScheduleInput struct {
	WorkspaceID           string
	IssueID               string
	SourceSessionID       string
	CheckpointID          string
	ExpectedGraphRevision int64
	TaskIDs               []string
	RequestID             string
	Runs                  []workspaceissues.Run
}

func (service Service) Materialize(
	ctx context.Context,
	input MaterializeInput,
) (workspaceissues.Issue, []workspaceissues.Task, executionbiz.Aggregate, error) {
	if service.Store == nil {
		return workspaceissues.Issue{}, nil, executionbiz.Aggregate{}, ErrServiceUnavailable
	}
	if input.Issue.PlanningSource != workspaceissues.PlanningSourceTuttiModePlan ||
		strings.TrimSpace(input.Issue.SourceSessionID) == "" || len(input.Tasks) == 0 {
		return workspaceissues.Issue{}, nil, executionbiz.Aggregate{}, executionbiz.ErrInvalidExecution
	}
	aggregate, err := executionbiz.NewInitialAggregate(
		input.Issue.WorkspaceID,
		input.Issue.IssueID,
		input.WorkflowID,
		input.Issue.SourceSessionID,
		service.now(),
	)
	if err != nil {
		return workspaceissues.Issue{}, nil, executionbiz.Aggregate{}, err
	}
	return service.Store.MaterializeTuttiModeIssue(ctx, input.Issue, input.Tasks, aggregate)
}

func (service Service) GetByIssue(
	ctx context.Context,
	workspaceID string,
	issueID string,
) (executionbiz.Aggregate, error) {
	if service.Store == nil {
		return executionbiz.Aggregate{}, ErrServiceUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if workspaceID == "" || issueID == "" {
		return executionbiz.Aggregate{}, executionbiz.ErrInvalidExecution
	}
	return service.Store.GetTuttiModeExecutionByIssue(ctx, workspaceID, issueID)
}

func (service Service) Schedule(
	ctx context.Context,
	input ScheduleInput,
) (executionbiz.ScheduleResult, error) {
	if service.Store == nil {
		return executionbiz.ScheduleResult{}, ErrServiceUnavailable
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.IssueID = strings.TrimSpace(input.IssueID)
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.CheckpointID = strings.TrimSpace(input.CheckpointID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.WorkspaceID == "" || input.IssueID == "" ||
		input.SourceSessionID == "" || input.CheckpointID == "" ||
		input.RequestID == "" || input.ExpectedGraphRevision < 1 ||
		len(input.TaskIDs) == 0 || len(input.TaskIDs) != len(input.Runs) {
		return executionbiz.ScheduleResult{}, executionbiz.ErrScheduleRejected
	}
	taskIDs := make([]string, len(input.TaskIDs))
	seen := make(map[string]struct{}, len(input.TaskIDs))
	for index, taskID := range input.TaskIDs {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			return executionbiz.ScheduleResult{}, executionbiz.ErrScheduleRejected
		}
		if _, duplicate := seen[taskID]; duplicate {
			return executionbiz.ScheduleResult{}, executionbiz.ErrScheduleRejected
		}
		seen[taskID] = struct{}{}
		taskIDs[index] = taskID
		run := input.Runs[index]
		if run.WorkspaceID != input.WorkspaceID || run.IssueID != input.IssueID ||
			run.TaskID != taskID || strings.TrimSpace(run.RunID) == "" ||
			run.Status != workspaceissues.StatusRunning {
			return executionbiz.ScheduleResult{}, executionbiz.ErrScheduleRejected
		}
	}
	digest, err := scheduleInputDigest(input, taskIDs)
	if err != nil {
		return executionbiz.ScheduleResult{}, err
	}
	return service.Store.AdmitTuttiModeSchedule(ctx, executionbiz.ScheduleAdmission{
		WorkspaceID:           input.WorkspaceID,
		IssueID:               input.IssueID,
		SourceSessionID:       input.SourceSessionID,
		CheckpointID:          input.CheckpointID,
		ExpectedGraphRevision: input.ExpectedGraphRevision,
		RequestID:             input.RequestID,
		InputSHA256:           digest,
		Runs:                  append([]workspaceissues.Run(nil), input.Runs...),
		Now:                   service.now(),
	})
}

func (service Service) ListPreparedRunLaunches(
	ctx context.Context,
	workspaceID string,
	issueID string,
	runIDs []string,
) ([]executionbiz.PreparedRunLaunch, error) {
	if service.Store == nil {
		return nil, ErrServiceUnavailable
	}
	return service.Store.ListPreparedTuttiModeRunLaunches(ctx, workspaceID, issueID, runIDs, service.now())
}

func (service Service) GetRunLaunchClientSubmitID(
	ctx context.Context,
	workspaceID string,
	issueID string,
	runID string,
) (string, bool, error) {
	if service.Store == nil {
		return "", false, ErrServiceUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	runID = strings.TrimSpace(runID)
	if workspaceID == "" || issueID == "" || runID == "" {
		return "", false, executionbiz.ErrScheduleRejected
	}
	return service.Store.GetTuttiModeRunLaunchClientSubmitID(
		ctx, workspaceID, issueID, runID,
	)
}

func (service Service) ClaimRunLaunch(
	ctx context.Context,
	workspaceID string,
	issueID string,
	runID string,
	leaseOwner string,
	leaseDuration time.Duration,
) (bool, error) {
	if service.Store == nil {
		return false, ErrServiceUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	runID = strings.TrimSpace(runID)
	leaseOwner = strings.TrimSpace(leaseOwner)
	if workspaceID == "" || issueID == "" || runID == "" || leaseOwner == "" ||
		leaseDuration <= 0 {
		return false, executionbiz.ErrScheduleRejected
	}
	now := service.now()
	return service.Store.ClaimTuttiModeRunLaunchIntent(
		ctx, workspaceID, issueID, runID, leaseOwner, now, now.Add(leaseDuration),
	)
}

func (service Service) ReleaseRunLaunch(
	ctx context.Context,
	workspaceID string,
	issueID string,
	runID string,
	leaseOwner string,
) error {
	if service.Store == nil {
		return ErrServiceUnavailable
	}
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(issueID) == "" ||
		strings.TrimSpace(runID) == "" || strings.TrimSpace(leaseOwner) == "" {
		return executionbiz.ErrScheduleRejected
	}
	return service.Store.ReleaseTuttiModeRunLaunchIntent(
		ctx, workspaceID, issueID, runID, leaseOwner, service.now(),
	)
}

func (service Service) RenewRunLaunch(
	ctx context.Context,
	workspaceID string,
	issueID string,
	runID string,
	leaseOwner string,
	leaseDuration time.Duration,
) error {
	if service.Store == nil {
		return ErrServiceUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	runID = strings.TrimSpace(runID)
	leaseOwner = strings.TrimSpace(leaseOwner)
	if workspaceID == "" || issueID == "" || runID == "" || leaseOwner == "" ||
		leaseDuration <= 0 {
		return executionbiz.ErrScheduleRejected
	}
	now := service.now()
	return service.Store.RenewTuttiModeRunLaunchIntent(
		ctx, workspaceID, issueID, runID, leaseOwner, now, now.Add(leaseDuration),
	)
}

func (service Service) MarkRunLaunchDispatched(
	ctx context.Context,
	workspaceID string,
	issueID string,
	runID string,
	leaseOwner string,
) error {
	if service.Store == nil {
		return ErrServiceUnavailable
	}
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(issueID) == "" ||
		strings.TrimSpace(runID) == "" || strings.TrimSpace(leaseOwner) == "" {
		return executionbiz.ErrScheduleRejected
	}
	return service.Store.MarkTuttiModeRunLaunchIntentDispatched(
		ctx,
		strings.TrimSpace(workspaceID),
		strings.TrimSpace(issueID),
		strings.TrimSpace(runID),
		strings.TrimSpace(leaseOwner),
		service.now(),
	)
}

func (service Service) RequeueLeasedRunLaunches(
	ctx context.Context,
	workspaceID string,
) error {
	if service.Store == nil {
		return ErrServiceUnavailable
	}
	return service.Store.RequeueLeasedTuttiModeRunLaunchIntents(
		ctx, strings.TrimSpace(workspaceID), service.now(),
	)
}

func scheduleInputDigest(input ScheduleInput, taskIDs []string) (string, error) {
	payload, err := json.Marshal(struct {
		CheckpointID          string   `json:"checkpointId"`
		ExpectedGraphRevision int64    `json:"expectedGraphRevision"`
		TaskIDs               []string `json:"taskIds"`
	}{
		CheckpointID:          input.CheckpointID,
		ExpectedGraphRevision: input.ExpectedGraphRevision,
		TaskIDs:               taskIDs,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (service Service) now() time.Time {
	if service.Clock != nil {
		return service.Clock().UTC()
	}
	return time.Now().UTC()
}
