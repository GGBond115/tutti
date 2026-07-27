package workspace

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	workspaceissues "github.com/tutti-os/tutti/packages/workspace/issues"
	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
	tuttimodeexecutionservice "github.com/tutti-os/tutti/services/tuttid/service/tuttimodeexecution"
)

func (s IssueManagerService) ScheduleTuttiModeIssue(
	ctx context.Context,
	workspaceID string,
	input ScheduleTuttiModeIssueInput,
) (ScheduleTuttiModeIssueResult, error) {
	if s.TuttiModeExecutions == nil || s.RunLauncher == nil {
		return ScheduleTuttiModeIssueResult{}, tuttimodeexecutionservice.ErrServiceUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	input.IssueID = strings.TrimSpace(input.IssueID)
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.CheckpointID = strings.TrimSpace(input.CheckpointID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	if workspaceID == "" || input.IssueID == "" || input.SourceSessionID == "" ||
		input.CheckpointID == "" || input.ExpectedGraphRevision < 1 ||
		input.RequestID == "" || len(input.TaskIDs) == 0 {
		return ScheduleTuttiModeIssueResult{}, executionbiz.ErrScheduleRejected
	}

	unlockIssue := s.MutationLocks.Lock(workspaceID, input.IssueID)
	detail, err := s.domainService().GetIssueDetail(ctx, workspaceID, input.IssueID)
	if err != nil {
		unlockIssue()
		return ScheduleTuttiModeIssueResult{}, err
	}
	if err := s.validateTuttiModeScheduleIsolation(detail.Issue, detail.Tasks, input.TaskIDs); err != nil {
		unlockIssue()
		return ScheduleTuttiModeIssueResult{}, err
	}
	runs, err := s.prepareTuttiModeScheduleRuns(detail.Issue, detail.Tasks, input.TaskIDs)
	if err != nil {
		unlockIssue()
		return ScheduleTuttiModeIssueResult{}, err
	}
	scheduled, err := s.TuttiModeExecutions.Schedule(ctx, tuttimodeexecutionservice.ScheduleInput{
		WorkspaceID:           workspaceID,
		IssueID:               input.IssueID,
		SourceSessionID:       input.SourceSessionID,
		CheckpointID:          input.CheckpointID,
		ExpectedGraphRevision: input.ExpectedGraphRevision,
		TaskIDs:               append([]string(nil), input.TaskIDs...),
		RequestID:             input.RequestID,
		Runs:                  runs,
	})
	if err != nil {
		unlockIssue()
		return ScheduleTuttiModeIssueResult{}, err
	}
	preparedRuns, err := s.TuttiModeExecutions.ListPreparedRunLaunches(
		ctx,
		workspaceID,
		input.IssueID,
		scheduled.RunIDs,
	)
	if err != nil {
		unlockIssue()
		return ScheduleTuttiModeIssueResult{}, err
	}
	launches := s.tuttiModeLaunchesForRuns(ctx, detail.Issue, detail.Tasks, preparedRuns)
	unlockIssue()

	s.launchScheduledTuttiModeRuns(ctx, launches)
	return ScheduleTuttiModeIssueResult{
		ExecutionID:   scheduled.ExecutionID,
		CheckpointID:  scheduled.CheckpointID,
		GraphRevision: scheduled.GraphRevision,
		RunIDs:        append([]string(nil), scheduled.RunIDs...),
		Replayed:      scheduled.Replayed,
	}, nil
}

func (s IssueManagerService) validateTuttiModeScheduleIsolation(
	issue workspaceissues.Issue,
	tasks []workspaceissues.Task,
	taskIDs []string,
) error {
	byID := make(map[string]workspaceissues.Task, len(tasks))
	activeOther := false
	for _, task := range tasks {
		byID[task.TaskID] = task
		if task.Status == workspaceissues.StatusRunning ||
			task.Status == workspaceissues.StatusPendingAcceptance {
			activeOther = true
			if !task.Parallelizable {
				for _, taskID := range taskIDs {
					if strings.TrimSpace(taskID) != task.TaskID {
						return executionbiz.ErrScheduleRejected
					}
				}
			}
		}
	}
	newTasks := make([]workspaceissues.Task, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		task, ok := byID[strings.TrimSpace(taskID)]
		if !ok {
			return executionbiz.ErrScheduleRejected
		}
		if task.Status == workspaceissues.StatusNotStarted {
			newTasks = append(newTasks, task)
		}
	}
	requiresConcurrency := len(newTasks) > 1 || (len(newTasks) > 0 && activeOther)
	if !requiresConcurrency {
		return nil
	}
	for _, task := range newTasks {
		if !task.Parallelizable {
			return executionbiz.ErrScheduleRejected
		}
		if issue.SequentialExecution {
			if _, concurrent := s.sequentialTaskIsolation(issue, tasks, task); !concurrent {
				return executionbiz.ErrScheduleRejected
			}
		}
	}
	return nil
}

func (s IssueManagerService) prepareTuttiModeScheduleRuns(
	issue workspaceissues.Issue,
	tasks []workspaceissues.Task,
	taskIDs []string,
) ([]workspaceissues.Run, error) {
	if issue.PlanningSource != workspaceissues.PlanningSourceTuttiModePlan {
		return nil, executionbiz.ErrScheduleRejected
	}
	byID := make(map[string]workspaceissues.Task, len(tasks))
	for _, task := range tasks {
		byID[task.TaskID] = task
	}
	seen := make(map[string]struct{}, len(taskIDs))
	now := time.Now().UTC().UnixMilli()
	runs := make([]workspaceissues.Run, 0, len(taskIDs))
	for _, rawTaskID := range taskIDs {
		taskID := strings.TrimSpace(rawTaskID)
		task, ok := byID[taskID]
		if taskID == "" || !ok {
			return nil, executionbiz.ErrScheduleRejected
		}
		if _, duplicate := seen[taskID]; duplicate {
			return nil, executionbiz.ErrScheduleRejected
		}
		seen[taskID] = struct{}{}
		runID := uuid.NewString()
		executionDirectory := s.resolveIssueTaskBaseDirectory(issue, task)
		runs = append(runs, workspaceissues.Run{
			RunID:              runID,
			TaskID:             task.TaskID,
			IssueID:            issue.IssueID,
			WorkspaceID:        issue.WorkspaceID,
			RequesterUserID:    issueManagerLocalActorUserID,
			AgentUserID:        issueManagerLocalActorUserID,
			AgentTargetID:      task.AgentTargetID,
			AgentSessionID:     uuid.NewString(),
			AgentProvider:      strings.TrimPrefix(task.AgentTargetID, "local:"),
			ModelPlanID:        task.ModelPlanID,
			Model:              task.Model,
			ReasoningIntensity: issue.ExecutionProfile.ReasoningIntensity,
			Status:             workspaceissues.StatusRunning,
			ExecutionDirectory: executionDirectory,
			CreatedAtUnixMS:    now,
			StartedAtUnixMS:    now,
			UpdatedAtUnixMS:    now,
		})
	}
	return runs, nil
}

func (s IssueManagerService) tuttiModeLaunchesForRuns(
	ctx context.Context,
	issue workspaceissues.Issue,
	tasks []workspaceissues.Task,
	runs []workspaceissues.Run,
) []IssueRunLaunch {
	byID := make(map[string]workspaceissues.Task, len(tasks))
	for _, task := range tasks {
		byID[task.TaskID] = task
	}
	launches := make([]IssueRunLaunch, 0, len(runs))
	for _, run := range runs {
		task, ok := byID[run.TaskID]
		if !ok {
			continue
		}
		isolation := issueTaskIsolation{}
		if task.Parallelizable {
			isolation, _ = s.sequentialTaskIsolation(issue, tasks, task)
		}
		executionDirectory := run.ExecutionDirectory
		worktreeBase := ""
		worktreeBranch := ""
		if isolation.worktreeBase != "" {
			worktreePath, branch := s.issueTaskRunWorktreePlan(issue.IssueID, task.TaskID, run.RunID)
			worktreeBase = isolation.worktreeBase
			worktreeBranch = branch
			executionDirectory = worktreePath
		}
		launches = append(launches, IssueRunLaunch{
			WorkspaceID:        run.WorkspaceID,
			AgentSessionID:     run.AgentSessionID,
			AgentTargetID:      run.AgentTargetID,
			RunID:              run.RunID,
			TaskID:             run.TaskID,
			IssueID:            run.IssueID,
			Title:              task.Title,
			Prompt:             issueTaskPrompt(issue, task, executionDirectory, worktreeBase, worktreeBranch, s.dependencyWorktreeOutputs(ctx, issue, task, byID)),
			ExecutionDirectory: executionDirectory,
			ModelPlanID:        run.ModelPlanID,
			Model:              run.Model,
			ReasoningIntensity: run.ReasoningIntensity,
			ReasoningEffort:    task.ReasoningEffort,
			PermissionModeID:   task.PermissionModeID,
			WorktreeBase:       worktreeBase,
			WorktreeBranch:     worktreeBranch,
		})
	}
	return launches
}

func (s IssueManagerService) launchScheduledTuttiModeRuns(
	ctx context.Context,
	launches []IssueRunLaunch,
) {
	for _, launch := range launches {
		gate := s.runLaunchGate()
		if !gate.begin(launch.WorkspaceID, launch.RunID) {
			continue
		}
		if s.issueRunLaunchDecision(ctx, launch) != issueRunLaunch {
			gate.finish(launch.WorkspaceID, launch.RunID)
			continue
		}
		var err error
		if launch.WorktreeBase != "" {
			_, _, err = s.createIssueTaskRunWorktree(
				ctx,
				launch.WorktreeBase,
				launch.IssueID,
				launch.TaskID,
				launch.RunID,
			)
		}
		if err == nil {
			err = s.RunLauncher.Launch(ctx, launch)
		}
		cancelRequested := gate.finish(launch.WorkspaceID, launch.RunID)
		if err != nil {
			// Task 4 turns this durable prepared intent into a failed
			// settlement checkpoint. Until then it remains recoverable.
			continue
		}
		_ = s.TuttiModeExecutions.MarkRunLaunchDispatched(
			ctx,
			launch.WorkspaceID,
			launch.IssueID,
			launch.RunID,
		)
		if cancelRequested {
			s.cancelIssueRunAfterLaunch(ctx, launch)
		}
	}
}

func (s IssueManagerService) RecoverTuttiModeRunLaunchIntents(
	ctx context.Context,
	workspaceID string,
) error {
	if s.TuttiModeExecutions == nil || s.RunLauncher == nil {
		return tuttimodeexecutionservice.ErrServiceUnavailable
	}
	runs, err := s.TuttiModeExecutions.ListPreparedRunLaunches(
		ctx,
		strings.TrimSpace(workspaceID),
		"",
		nil,
	)
	if err != nil {
		return err
	}
	byIssue := make(map[string][]workspaceissues.Run)
	issueOrder := make([]string, 0)
	for _, run := range runs {
		if _, exists := byIssue[run.IssueID]; !exists {
			issueOrder = append(issueOrder, run.IssueID)
		}
		byIssue[run.IssueID] = append(byIssue[run.IssueID], run)
	}
	for _, issueID := range issueOrder {
		unlockIssue := s.MutationLocks.Lock(workspaceID, issueID)
		detail, getErr := s.domainService().GetIssueDetail(ctx, workspaceID, issueID)
		if getErr != nil {
			unlockIssue()
			return getErr
		}
		launches := s.tuttiModeLaunchesForRuns(ctx, detail.Issue, detail.Tasks, byIssue[issueID])
		unlockIssue()
		s.launchScheduledTuttiModeRuns(ctx, launches)
	}
	return nil
}
