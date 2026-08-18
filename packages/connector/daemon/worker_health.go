package daemon

import (
	"context"
	"time"
)

const (
	workerFailureCatalogRefresh      = "catalog_refresh_failed"
	workerFailureRuntimeRecovery     = "runtime_recovery_failed"
	workerFailureRuntimeRouteWatch   = "runtime_route_watch_failed"
	workerFailureAuthorizationEvents = "authorization_events_failed"
)

func reportWorkerFailure(ctx context.Context, code string, retryDelay time.Duration) {
	now := time.Now().UTC()
	backoffUntil := time.Time{}
	if retryDelay > 0 {
		backoffUntil = now.Add(retryDelay)
	}
	workerReporter(ctx).Failure(now, code, backoffUntil, nil)
}

func reportWorkerSuccess(ctx context.Context) {
	workerReporter(ctx).Success(time.Now().UTC())
}

func nextBoundedRetry(current, maximum time.Duration) time.Duration {
	if current <= 0 {
		return time.Second
	}
	if current >= maximum || current > maximum/2 {
		return maximum
	}
	return current * 2
}
