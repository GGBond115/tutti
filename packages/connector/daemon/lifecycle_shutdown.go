package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (host *Host) applyCapabilityPublication(ctx context.Context, scope contracts.OperationScope, enabled bool) error {
	host.publicationScopeMu.Lock()
	host.publicationScope = scope
	host.publicationScopeMu.Unlock()
	if host.publication != nil {
		return host.publication.ApplyCapabilityPublication(ctx, scope, enabled)
	}
	if host.publicationGate != nil {
		host.publicationGate.SetCapabilityPublication(enabled)
	}
	return nil
}

func (host *Host) currentPublicationScope() contracts.OperationScope {
	host.publicationScopeMu.Lock()
	defer host.publicationScopeMu.Unlock()
	return host.publicationScope
}

func (host *Host) failCloseCapabilityPublication(scope contracts.OperationScope) <-chan error {
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result <- host.applyCapabilityPublication(ctx, scope, false)
	}()
	return result
}

func (host *Host) Close(ctx context.Context) error {
	if host == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("connector market host close context is required")
	}
	host.closeOnce.Do(func() {
		host.lifecycleMu.Lock()
		needsFence := host.lifecycleState == LifecycleStateRunning || host.lifecycleState == LifecycleStateStarting
		host.lifecycleState = LifecycleStateStopping
		workers := host.workers
		host.lifecycleMu.Unlock()
		drained := host.commandAdmission.close()
		var publicationResult <-chan error
		if needsFence {
			scope := host.currentPublicationScope()
			host.activationGate.setOpen(scope, false)
			publicationResult = host.failCloseCapabilityPublication(scope)
		} else {
			completed := make(chan error, 1)
			completed <- nil
			publicationResult = completed
		}
		go host.shutdownAfterCommandDrain(drained, workers, needsFence, publicationResult)
	})
	select {
	case <-host.closeDone:
		return host.closeResult
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (host *Host) shutdownAfterCommandDrain(
	drained <-chan struct{},
	workers *workerGroup,
	needsFence bool,
	publicationResult <-chan error,
) {
	<-drained
	_ = host.acquireScopeTransition(context.Background())
	defer host.releaseScopeTransition()
	if needsFence {
		fenceContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		host.shutdownFenceErr = host.activationGate.FailClosed(fenceContext, time.Now().Add(10*time.Second))
		cancel()
	}
	host.shutdownFenceErr = errors.Join(host.shutdownFenceErr, <-publicationResult)
	if workers != nil {
		workers.Stop()
	}
	host.finishClose(workers)
}

func (host *Host) finishClose(workers *workerGroup) {
	if workers != nil {
		_ = workers.Wait(context.Background())
	}
	host.scheduler.Wait()
	closeErr := host.shutdownFenceErr
	if closer, ok := host.implementationCommands.(interface{ Close() error }); ok {
		closeErr = errors.Join(closeErr, closer.Close())
	}
	host.lifecycleMu.Lock()
	host.closeResult = closeErr
	host.lifecycleState = LifecycleStateStopped
	host.lifecycleMu.Unlock()
	close(host.closeDone)
}
