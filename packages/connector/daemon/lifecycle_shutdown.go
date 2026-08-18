package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

const defaultShutdownTimeout = 10 * time.Second

func (host *Host) applyCapabilityPublication(ctx context.Context, scope contracts.OperationScope, enabled bool) error {
	// The external call is part of the serialized publication transition. A
	// final disable must complete after any earlier enable, not merely be
	// invoked after it.
	host.publicationMu.Lock()
	defer host.publicationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if host.publication != nil {
		err = host.publication.ApplyCapabilityPublication(ctx, scope, enabled)
	} else if host.publicationGate != nil {
		host.publicationGate.SetCapabilityPublication(enabled)
	}
	if err == nil {
		host.publicationScopeMu.Lock()
		host.publicationScope = scope
		host.publicationScopeMu.Unlock()
	}
	return err
}

func (host *Host) currentPublicationScope() contracts.OperationScope {
	host.publicationScopeMu.Lock()
	defer host.publicationScopeMu.Unlock()
	return host.publicationScope
}

func (host *Host) shutdownDuration() time.Duration {
	if host != nil && host.shutdownTimeout > 0 {
		return host.shutdownTimeout
	}
	return defaultShutdownTimeout
}

func (host *Host) newShutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), host.shutdownDuration())
}

func (host *Host) shutdownDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(host.shutdownDuration())
}

// runBounded prevents a context-ignoring dependency from holding the host's
// shutdown coordinator forever. The dependency goroutine is intentionally not
// canceled a second time: it retains the bounded call context and, for a
// pending FailClosed call, remains ordered behind any in-flight mutation so it
// can perform the durable fence when that mutation eventually returns.
func (*Host) runBounded(ctx context.Context, run func(context.Context) error) error {
	result := make(chan error, 1)
	go func() { result <- run(ctx) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (host *Host) publicationEnableEpoch() (uint64, uint64, error) {
	host.lifecycleMu.Lock()
	defer host.lifecycleMu.Unlock()
	if host.lifecycleState != LifecycleStateStarting && host.lifecycleState != LifecycleStateRunning {
		return 0, 0, errHostNotRunning
	}
	return host.lifecycleEpoch, host.transitionEpoch, nil
}

func (host *Host) validatePublicationEnableEpoch(lifecycleEpoch, transitionEpoch uint64) error {
	host.lifecycleMu.Lock()
	defer host.lifecycleMu.Unlock()
	if host.lifecycleEpoch != lifecycleEpoch || host.transitionEpoch != transitionEpoch ||
		(host.lifecycleState != LifecycleStateStarting && host.lifecycleState != LifecycleStateRunning) {
		return errHostNotRunning
	}
	return nil
}

func (host *Host) enableCapabilityPublication(ctx context.Context, scope contracts.OperationScope) error {
	lifecycleEpoch, transitionEpoch, err := host.publicationEnableEpoch()
	if err != nil {
		return err
	}
	if err := host.applyCapabilityPublication(ctx, scope, true); err != nil {
		return err
	}
	if err := host.validatePublicationEnableEpoch(lifecycleEpoch, transitionEpoch); err != nil {
		cleanupCtx, cancel := host.newShutdownContext()
		defer cancel()
		return errors.Join(err, host.runBounded(cleanupCtx, func(callCtx context.Context) error {
			return host.applyCapabilityPublication(callCtx, scope, false)
		}))
	}
	return nil
}

func (host *Host) beginBestEffortPublicationDisable(scope contracts.OperationScope) <-chan error {
	result := make(chan error, 1)
	go func() {
		publicationCtx, cancel := host.newShutdownContext()
		defer cancel()
		result <- host.runBounded(publicationCtx, func(callCtx context.Context) error {
			return host.applyCapabilityPublication(callCtx, scope, false)
		})
	}()
	return result
}

func (host *Host) cleanupFailedStart(workers *workerGroup) error {
	if workers != nil {
		workers.Stop()
		workers.Seal()
	}
	rollbackScope := host.currentPublicationScope()
	host.activationGate.setOpen(rollbackScope, false)
	immediatePublication := host.beginBestEffortPublicationDisable(rollbackScope)
	var cleanupErr error
	if workers != nil {
		workerCtx, cancelWorkers := host.newShutdownContext()
		cleanupErr = errors.Join(cleanupErr, workers.Wait(workerCtx))
		cancelWorkers()
	}
	schedulerCtx, cancelScheduler := host.newShutdownContext()
	cleanupErr = errors.Join(cleanupErr, host.scheduler.Wait(schedulerCtx))
	cancelScheduler()
	cleanupErr = errors.Join(cleanupErr, <-immediatePublication)

	transitionCtx, cancelTransition := host.newShutdownContext()
	if err := host.acquireScopeTransition(transitionCtx); err != nil {
		cancelTransition()
		return errors.Join(cleanupErr, fmt.Errorf("acquire startup rollback transition: %w", err))
	}
	cancelTransition()
	defer host.releaseScopeTransition()
	publicationCtx, cancelPublication := host.newShutdownContext()
	cleanupErr = errors.Join(cleanupErr, host.runBounded(publicationCtx, func(callCtx context.Context) error {
		return host.applyCapabilityPublication(callCtx, rollbackScope, false)
	}))
	cancelPublication()
	fenceCtx, cancelFence := host.newShutdownContext()
	cleanupErr = errors.Join(cleanupErr, host.runBounded(fenceCtx, func(callCtx context.Context) error {
		return host.activationGate.FailClosed(callCtx, host.shutdownDeadline(callCtx))
	}))
	cancelFence()
	return cleanupErr
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
		host.lifecycleEpoch++
		lifecycleCancel := host.lifecycleCancel
		workers := host.workers
		host.lifecycleMu.Unlock()
		drained := host.commandAdmission.close()
		var immediatePublication <-chan error
		var publicationScope contracts.OperationScope
		if needsFence {
			publicationScope = host.currentPublicationScope()
			host.activationGate.setOpen(publicationScope, false)
		}
		// Mark the group as intentionally stopping before canceling its parent;
		// otherwise a fast worker exit can be misclassified as an unexpected
		// lifecycle failure.
		if workers != nil {
			workers.Stop()
		}
		if lifecycleCancel != nil {
			lifecycleCancel()
		}
		if needsFence {
			immediatePublication = host.beginBestEffortPublicationDisable(publicationScope)
		} else {
			completed := make(chan error, 1)
			completed <- nil
			immediatePublication = completed
		}
		go host.shutdownAfterCommandDrain(drained, workers, needsFence, immediatePublication)
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
	immediatePublication <-chan error,
) {
	var closeErr error
	drainCtx, cancelDrain := host.newShutdownContext()
	select {
	case <-drained:
	case <-drainCtx.Done():
		closeErr = errors.Join(closeErr, fmt.Errorf("drain connector commands: %w", drainCtx.Err()))
	}
	cancelDrain()
	closeErr = errors.Join(closeErr, <-immediatePublication)

	transitionCtx, cancelTransition := host.newShutdownContext()
	if err := host.acquireScopeTransition(transitionCtx); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("acquire connector shutdown transition: %w", err))
	} else {
		scope := host.currentPublicationScope()
		host.activationGate.setOpen(scope, false)
		if needsFence {
			publicationCtx, cancelPublication := host.newShutdownContext()
			closeErr = errors.Join(closeErr, host.runBounded(publicationCtx, func(callCtx context.Context) error {
				return host.applyCapabilityPublication(callCtx, scope, false)
			}))
			cancelPublication()

			fenceCtx, cancelFence := host.newShutdownContext()
			closeErr = errors.Join(closeErr, host.runBounded(fenceCtx, func(callCtx context.Context) error {
				return host.activationGate.FailClosed(callCtx, host.shutdownDeadline(callCtx))
			}))
			cancelFence()
		}
		host.releaseScopeTransition()
	}
	cancelTransition()
	host.finishClose(workers, closeErr)
}

func (host *Host) finishClose(workers *workerGroup, closeErr error) {
	if workers != nil {
		workerCtx, cancelWorkers := host.newShutdownContext()
		closeErr = errors.Join(closeErr, workers.Wait(workerCtx))
		cancelWorkers()
	}
	schedulerCtx, cancelScheduler := host.newShutdownContext()
	closeErr = errors.Join(closeErr, host.scheduler.Wait(schedulerCtx))
	cancelScheduler()
	if host.implementationCommands != nil {
		runtimeCtx, cancelRuntime := host.newShutdownContext()
		closeErr = errors.Join(closeErr, host.runBounded(runtimeCtx, host.implementationCommands.Close))
		cancelRuntime()
	}
	host.lifecycleMu.Lock()
	host.closeResult = closeErr
	host.lifecycleState = LifecycleStateStopped
	host.lifecycleMu.Unlock()
	close(host.closeDone)
}
