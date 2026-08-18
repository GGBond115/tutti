package application

import (
	"context"
	"sort"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

// RuntimeRetryHealth projects durable runtime debt without exposing repository
// rows, leases, or raw adapter errors to daemon health consumers.
func (application *service) RuntimeRetryHealth(
	ctx context.Context,
	scope contracts.OperationScope,
) ([]contracts.RuntimeRetryHealth, error) {
	convergences, err := application.RuntimeConvergenceSnapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	health := make([]contracts.RuntimeRetryHealth, 0, len(convergences))
	for _, convergence := range convergences {
		exhausted := convergence.Attempt >= contracts.RuntimeFailureBudget
		item := contracts.RuntimeRetryHealth{
			ConnectorKey:        strings.TrimSpace(convergence.Desired.ConnectorKey),
			LastSuccess:         convergence.Observed.ObservedAt,
			ConsecutiveFailures: convergence.Attempt,
			FailureBudget:       contracts.RuntimeFailureBudget,
			Exhausted:           exhausted,
		}
		if convergence.Attempt > 0 {
			item.FailureCode = stableRuntimeFailureCode(convergence.LastErrorCode)
			if !exhausted {
				item.BackoffUntil = convergence.NextAttemptAt
			}
		}
		health = append(health, item)
	}
	sort.Slice(health, func(left, right int) bool {
		return health[left].ConnectorKey < health[right].ConnectorKey
	})
	return health, nil
}

func stableRuntimeFailureCode(raw string) contracts.ErrorCode {
	code := contracts.ErrorCode(strings.TrimSpace(raw))
	switch code {
	case contracts.ErrorCodeInvalidRequest,
		contracts.ErrorCodeNotFound,
		contracts.ErrorCodeRevisionConflict,
		contracts.ErrorCodeOperationInProgress,
		contracts.ErrorCodeIncompatible,
		contracts.ErrorCodeInvalidManifest,
		contracts.ErrorCodeUnsupportedImplementation,
		contracts.ErrorCodeUpstreamUnavailable,
		contracts.ErrorCodeInstallFailed,
		contracts.ErrorCodeAuthorizationFailed,
		contracts.ErrorCodeUnavailable:
		return code
	default:
		return contracts.ErrorCodeUnavailable
	}
}
