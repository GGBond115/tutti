package tuttimodeexecution

import (
	"context"
	"errors"
	"strings"
	"time"

	workspaceissues "github.com/tutti-os/tutti/packages/workspace/issues"
	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
)

var ErrServiceUnavailable = errors.New("Tutti mode execution service is unavailable")
var ErrExecutionNotFound = executionbiz.ErrExecutionNotFound
var ErrExecutionConflict = executionbiz.ErrExecutionConflict

type Store interface {
	MaterializeTuttiModeIssue(
		context.Context,
		workspaceissues.Issue,
		[]workspaceissues.Task,
		executionbiz.Aggregate,
	) (workspaceissues.Issue, []workspaceissues.Task, executionbiz.Aggregate, error)
	GetTuttiModeExecutionByIssue(context.Context, string, string) (executionbiz.Aggregate, error)
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

func (service Service) now() time.Time {
	if service.Clock != nil {
		return service.Clock().UTC()
	}
	return time.Now().UTC()
}
