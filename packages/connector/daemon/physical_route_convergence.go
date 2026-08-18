package daemon

import (
	"context"
	"errors"
	"fmt"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"log/slog"
	"math"
	"strings"
	"time"
)

const (
	physicalRouteSnapshotTimeout = 10 * time.Second
	physicalOrphanRemoveTimeout  = 10 * time.Second
	physicalRouteWatchRetryMax   = time.Minute
)

func (host *Host) notifyPhysicalRouteChanged() {
	if host == nil {
		return
	}
	select {
	case host.runtimePhysicalWake <- struct{}{}:
	default:
	}
}

// runPhysicalRouteWatchWorker only validates and coalesces edge-triggered
// hints. It never executes reconcile from the runtime reader goroutine.
func (host *Host) runPhysicalRouteWatchWorker(ctx context.Context) {
	retry := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		watchContext, cancelWatch := context.WithCancel(ctx)
		watch, err := host.physicalRoutes.Watch(watchContext)
		if err != nil || watch.Events == nil {
			cancelWatch()
			host.notifyPhysicalRouteChanged()
			if err != nil {
				slog.Warn("connector physical route watch failed", "error", err)
			} else {
				slog.Warn("connector physical route watch returned no event stream")
			}
			if !waitPhysicalRouteWatchRetry(ctx, retry) {
				return
			}
			retry = nextPhysicalRouteWatchRetry(retry)
			continue
		}
		retry = time.Second
		expectedRevision := watch.Revision
		watchFailed := false
		for {
			select {
			case <-ctx.Done():
				cancelWatch()
				return
			case event, ok := <-watch.Events:
				if !ok {
					watchFailed = true
				} else if expectedRevision == math.MaxUint64 || event.Revision != expectedRevision+1 ||
					(event.Kind != contracts.PhysicalRouteEventChanged && event.Kind != contracts.PhysicalRouteEventUnexpectedExit) {
					watchFailed = true
				} else {
					expectedRevision = event.Revision
					host.notifyPhysicalRouteChanged()
				}
			}
			if watchFailed {
				break
			}
		}
		cancelWatch()
		// Overflow, revision gaps, and source failure all invalidate cached
		// physical state. The convergence worker obtains the fresh Snapshot.
		host.notifyPhysicalRouteChanged()
		slog.Warn("connector physical route watch requires snapshot", "revision", expectedRevision)
		if !waitPhysicalRouteWatchRetry(ctx, retry) {
			return
		}
		retry = nextPhysicalRouteWatchRetry(retry)
	}
}

func nextPhysicalRouteWatchRetry(current time.Duration) time.Duration {
	next := current * 2
	if next > physicalRouteWatchRetryMax {
		return physicalRouteWatchRetryMax
	}
	return next
}

func waitPhysicalRouteWatchRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// reconcilePhysicalRouteSnapshot compares durable Desired, cached Observed,
// and current physical routes. Only an exact current-boot triple is considered
// converged; a missing route invalidates the receipt even when Desired and
// Observed otherwise match.
func (host *Host) reconcilePhysicalRouteSnapshot(ctx context.Context) error {
	return host.reconcilePhysicalRouteSnapshotWithPolicy(ctx, false)
}

func (host *Host) reconcilePhysicalRouteSnapshotWithPolicy(ctx context.Context, resetHealthyBudget bool) error {
	if host == nil || host.runtimeMaintenance == nil || host.physicalRoutes == nil {
		return errors.New("connector physical route convergence is unavailable")
	}
	host.bootstrapMu.Lock()
	bootstrapped, scope := host.bootstrapped, host.bootstrapScope
	host.bootstrapMu.Unlock()
	if !bootstrapped {
		return nil
	}
	snapshotContext, cancelSnapshot := context.WithTimeout(ctx, physicalRouteSnapshotTimeout)
	physical, err := host.physicalRoutes.Snapshot(snapshotContext)
	cancelSnapshot()
	if err != nil {
		return err
	}
	convergences, err := host.runtimeMaintenance.RuntimeConvergenceSnapshot(ctx, scope)
	if err != nil {
		return err
	}
	owners := make(map[string]struct{}, len(convergences))
	for _, convergence := range convergences {
		owners[physicalRouteOwnerKey(convergence.Desired.ConnectorKey, convergence.Desired.ConnectionID)] = struct{}{}
	}
	physicalByConnector := make(map[string][]contracts.PhysicalRoute)
	var result error
	for _, route := range physical.Routes {
		if _, owned := owners[physicalRouteOwnerKey(route.ConnectorKey, route.ConnectionID)]; owned {
			physicalByConnector[route.ConnectorKey] = append(physicalByConnector[route.ConnectorKey], route)
			continue
		}
		if orphanErr := host.removeOwnedPhysicalOrphan(ctx, scope, host.runtimeMaintenance.RuntimeBootEpoch(), route); orphanErr != nil {
			result = errors.Join(result, orphanErr)
		}
	}
	for _, convergence := range convergences {
		connectorKey := convergence.Desired.ConnectorKey
		if !runtimeObservationMatchesDesired(convergence, host.runtimeMaintenance.RuntimeBootEpoch()) {
			// Already level-triggered work; do not advance generations on every
			// anti-entropy scan while a prior repair is pending or backing off.
			continue
		}
		if physicalRuntimeMatchesDesired(convergence.Desired, host.runtimeMaintenance.RuntimeBootEpoch(), physicalByConnector[connectorKey]) {
			if resetHealthyBudget && convergence.Attempt > 0 {
				if resetErr := host.runtimeMaintenance.ResetRuntimeFailureBudget(
					ctx, scope, connectorKey, convergence.Desired.Generation,
				); resetErr != nil {
					result = errors.Join(result, fmt.Errorf("%s: %w", connectorKey, resetErr))
				}
			}
			continue
		}
		if invalidateErr := host.runtimeMaintenance.InvalidateRuntimeObservation(
			ctx, scope, connectorKey, convergence.Desired.Generation,
		); invalidateErr != nil {
			result = errors.Join(result, fmt.Errorf("%s: %w", connectorKey, invalidateErr))
		}
	}
	return result
}

func physicalRouteOwnerKey(connectorKey, connectionID string) string {
	return strings.TrimSpace(connectorKey) + "\x00" + strings.TrimSpace(connectionID)
}

func (host *Host) removeOwnedPhysicalOrphan(
	ctx context.Context,
	scope contracts.OperationScope,
	bootEpoch string,
	route contracts.PhysicalRoute,
) error {
	// A different boot may still be completing its own fence. This daemon has
	// no authority to guess that route's ownership.
	if strings.TrimSpace(route.Generation.BootEpoch) != strings.TrimSpace(bootEpoch) {
		return errors.New("connector physical orphan belongs to an unsupported boot epoch")
	}
	if strings.TrimSpace(route.ConnectorKey) == "" || strings.TrimSpace(route.ConnectionID) == "" ||
		strings.TrimSpace(route.ReleaseDigest) == "" || route.Generation.Generation == 0 ||
		(route.State != contracts.PhysicalRouteStateReady && route.State != contracts.PhysicalRouteStateDegraded) {
		return errors.New("connector physical orphan has an unsupported identity")
	}
	removeContext, cancelRemove := context.WithTimeout(ctx, physicalOrphanRemoveTimeout)
	defer cancelRemove()
	deadline := time.Now().Add(physicalOrphanRemoveTimeout)
	if err := host.implementationCommands.DeactivateRuntime(removeContext, contracts.RuntimeDeactivationRequest{
		Scope: scope, ConnectionID: route.ConnectionID, ConnectorKey: route.ConnectorKey,
		ReleaseDigest: route.ReleaseDigest, Generation: route.Generation, Deadline: deadline,
	}); err != nil {
		return fmt.Errorf("remove connector physical orphan %s: %w", route.ConnectorKey, err)
	}
	return nil
}

func runtimeObservationMatchesDesired(convergence contracts.RuntimeConvergence, bootEpoch string) bool {
	return convergence.Desired.Generation != 0 &&
		convergence.Observed.DesiredGeneration == convergence.Desired.Generation &&
		convergence.Observed.BootEpoch == bootEpoch &&
		convergence.Observed.Enabled == convergence.Desired.Enabled &&
		convergence.Observed.ConnectionID == convergence.Desired.ConnectionID &&
		convergence.Observed.ReleaseDigest == convergence.Desired.ReleaseDigest
}

func physicalRuntimeMatchesDesired(desired contracts.RuntimeDesired, bootEpoch string, routes []contracts.PhysicalRoute) bool {
	if !desired.Enabled {
		return len(routes) == 0
	}
	if len(routes) != 1 {
		return false
	}
	route := routes[0]
	return route.ConnectionID == desired.ConnectionID && route.ReleaseDigest == desired.ReleaseDigest &&
		route.Generation.BootEpoch == bootEpoch && route.Generation.Generation == desired.Generation &&
		route.State == contracts.PhysicalRouteStateReady
}
