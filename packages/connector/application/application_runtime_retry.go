package application

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (application *service) retryRuntimeConvergence(
	ctx context.Context,
	convergence contracts.RuntimeConvergence,
	cause error,
) error {
	now := application.config.Now().UTC()
	maximumBackoff := runtimeConvergenceBackoff(convergence.Attempt + 1)
	delay := application.config.RuntimeRetryJitter(maximumBackoff)
	if delay < 0 || delay > maximumBackoff {
		delay = maximumBackoff
	}
	nextAttemptAt := now.Add(delay)
	message := strings.TrimSpace(cause.Error())
	if len(message) > 512 {
		message = message[:512]
	}
	retryErr := application.config.Repository.RetryRuntimeConvergence(
		context.WithoutCancel(ctx), convergence.Desired.Scope, convergence.Desired.ConnectorKey,
		application.config.WorkerID, convergence.LeaseToken, convergence.Desired.Generation,
		nextAttemptAt, string(errorCodeOr(cause, contracts.ErrorCodeUnavailable)), message, now,
	)
	if retryErr != nil && !errors.Is(retryErr, contracts.ErrOperationLeaseLost) {
		return errors.Join(cause, fmt.Errorf("record runtime convergence retry: %w", retryErr))
	}
	return cause
}

func runtimeConvergenceBackoff(attempt uint32) time.Duration {
	if attempt == 0 {
		attempt = 1
	}
	exponent := attempt - 1
	if exponent > 5 {
		return time.Minute
	}
	return time.Second * time.Duration(uint64(1)<<exponent)
}

func runtimeFullJitter(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(maximum) + 1))
}

func (application *service) renewRuntimeConvergenceLease(
	ctx context.Context,
	cancel context.CancelFunc,
	convergence contracts.RuntimeConvergence,
	done chan<- error,
) {
	interval := application.config.LeaseDuration / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			now := application.config.Now().UTC()
			renewContext, renewCancel := context.WithTimeout(context.WithoutCancel(ctx), interval)
			err := application.config.Repository.RenewRuntimeConvergenceLease(
				renewContext, convergence.Desired.Scope, convergence.Desired.ConnectorKey,
				application.config.WorkerID, convergence.LeaseToken, now, now.Add(application.config.LeaseDuration),
			)
			renewCancel()
			if err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}
