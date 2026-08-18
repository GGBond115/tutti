package daemon

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	runtimeConvergenceScanInterval     = 500 * time.Millisecond
	runtimeConvergenceBatchSize        = 32
	runtimeConvergenceParallelism      = 4
	runtimeConvergenceTimeout          = 2 * time.Minute
	defaultPhysicalAntiEntropyInterval = 30 * time.Second
)

// runRuntimeConvergenceWorker continuously repairs durable Desired/Observed
// drift. The periodic scan is authoritative; command scheduling only reduces
// latency and is therefore not required for crash recovery.
func (host *Host) runRuntimeConvergenceWorker(ctx context.Context) {
	durableTicker := time.NewTicker(runtimeConvergenceScanInterval)
	defer durableTicker.Stop()
	physicalTimer := time.NewTimer(host.nextPhysicalAntiEntropyDelay())
	defer physicalTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-durableTicker.C:
			host.scanDueRuntimeConvergence(ctx)
		case <-host.runtimePhysicalWake:
			host.scanPhysicalRuntimeConvergence(ctx, false)
		case <-physicalTimer.C:
			host.scanPhysicalRuntimeConvergence(ctx, true)
			physicalTimer.Reset(host.nextPhysicalAntiEntropyDelay())
		}
	}
}

func (host *Host) scanDueRuntimeConvergence(ctx context.Context) {
	if err := host.convergeDueRuntimes(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("connector runtime convergence scan failed", "error", err)
	}
}

func (host *Host) scanPhysicalRuntimeConvergence(ctx context.Context, resetHealthyBudget bool) {
	if err := host.reconcilePhysicalRouteSnapshotWithPolicy(ctx, resetHealthyBudget); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("connector physical route snapshot failed", "error", err)
	}
	// Physical invalidation only persists new due work; command execution stays
	// on the normal lease/backoff path and outside the Watch reader goroutine.
	host.scanDueRuntimeConvergence(ctx)
}

func (host *Host) nextPhysicalAntiEntropyDelay() time.Duration {
	interval := host.physicalAntiEntropyInterval
	if interval <= 0 {
		interval = defaultPhysicalAntiEntropyInterval
	}
	jitter := host.physicalAntiEntropyJitter
	if jitter == nil {
		jitter = fullJitterDuration
	}
	delay := jitter(interval)
	if delay < 0 || delay > interval {
		return interval
	}
	return delay
}

func fullJitterDuration(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(maximum)))
}

func (host *Host) convergeDueRuntimes(ctx context.Context) error {
	host.bootstrapMu.Lock()
	bootstrapped, scope := host.bootstrapped, host.bootstrapScope
	host.bootstrapMu.Unlock()
	if !bootstrapped {
		return nil
	}
	due, err := host.Application.DueRuntimeConvergences(ctx, scope, runtimeConvergenceBatchSize)
	if err != nil || len(due) == 0 {
		return err
	}
	semaphore := make(chan struct{}, runtimeConvergenceParallelism)
	errorsFound := make(chan error, len(due))
	var wait sync.WaitGroup
	for _, convergence := range due {
		connectorKey := convergence.Desired.ConnectorKey
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				errorsFound <- ctx.Err()
				return
			}
			convergeContext, cancel := context.WithTimeout(ctx, runtimeConvergenceTimeout)
			err := host.Application.ConvergeRuntime(convergeContext, scope, connectorKey)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	var result error
	for err := range errorsFound {
		result = errors.Join(result, err)
	}
	return result
}
