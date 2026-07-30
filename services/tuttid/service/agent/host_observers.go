package agent

import (
	"context"
	"sync"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	reporterservice "github.com/tutti-os/tutti/services/tuttid/service/reporter"
)

type serviceHostLifecycleObserver struct {
	reporter reporterservice.Reporter
}

func (o serviceHostLifecycleObserver) ObserveLifecycleStep(ctx context.Context, step agenthost.LifecycleStep) {
	input := agentServiceNodeResultInput{
		AgentSessionID: step.AgentSessionID,
		Flow:           step.Flow,
		Node:           step.Name,
		Provider:       step.Provider,
		StartedAt:      step.StartedAt,
	}
	if step.Err != nil {
		input.Error = step.Err
		input.Status = "failure"
	}
	reportAgentServiceNodeResult(ctx, o.reporter, input)
}

type serviceHostCommitObserver struct {
	observer agenthost.CommitObserver
}

func (o serviceHostCommitObserver) ObserveCommitted(ctx context.Context, delta agenthost.CommittedDelta) error {
	if o.observer == nil {
		return nil
	}
	return o.observer.ObserveCommitted(ctx, delta)
}

type serviceHostRuntimeOperationEventPublisher struct {
	publisher RuntimeOperationEventPublisher
}

func (p serviceHostRuntimeOperationEventPublisher) PublishRuntimeOperationEvent(ctx context.Context, event storesqlite.RuntimeOperationEvent) error {
	if p.publisher == nil {
		return nil
	}
	return p.publisher.PublishRuntimeOperationEvent(ctx, event)
}

type serviceHostWorktreeGC struct {
	mu                     *sync.RWMutex
	stateDir               string
	workspaceIDs           func(context.Context) ([]string, error)
	sessionReader          SessionReader
	runtime                RuntimeController
	agentTargetStore       AgentTargetStore
	workspaceAgentResolver WorkspaceAgentResolver
}

func (g serviceHostWorktreeGC) SweepWorktreeIsolation(ctx context.Context) error {
	resumeFacade := &Service{
		Runtime:                g.runtime,
		AgentTargetStore:       g.agentTargetStore,
		WorkspaceAgentResolver: g.workspaceAgentResolver,
	}
	return sweepConfiguredWorktreeIsolation(
		ctx,
		g.mu,
		g.stateDir,
		g.workspaceIDs,
		g.sessionReader,
		func(session PersistedSession) bool {
			return resumeFacade.persistedSessionCanResume(ctx, session)
		},
	)
}
