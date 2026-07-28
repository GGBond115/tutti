package tuttimodeexecution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
)

const defaultMainWakeLeaseDuration = time.Minute

var ErrMainWakeDeliveryPending = errors.New("Tutti mode main wake delivery remains pending")

type SourceSessionObservation struct {
	Exists bool
	Busy   bool
}

type MainWakeDelivery struct {
	CanonicalSessionID string
	CanonicalTurnID    string
}

// MainWakeTarget is the daemon adapter onto Agent Host. It observes canonical
// session liveness without resuming a provider, submits through Host's
// idempotent SendInput lifecycle, and recovers canonical Turn identity.
type MainWakeTarget interface {
	ObserveSourceSession(context.Context, string, string) (SourceSessionObservation, error)
	SendMainWake(context.Context, string, string, string, string) (MainWakeDelivery, error)
	FindMainWakeTurn(context.Context, string, string, string) (string, bool, error)
}

func (service Service) ListWakes(
	ctx context.Context,
	workspaceID string,
	issueID string,
) ([]executionbiz.Wake, error) {
	store := service.wakeStore()
	if store == nil {
		return nil, ErrServiceUnavailable
	}
	return store.ListTuttiModeExecutionWakes(
		ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID),
	)
}

func (service Service) ClaimMainWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
	leaseDuration time.Duration,
) (bool, error) {
	store := service.wakeStore()
	if store == nil {
		return false, ErrServiceUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	wakeID = strings.TrimSpace(wakeID)
	leaseOwner = strings.TrimSpace(leaseOwner)
	if workspaceID == "" || wakeID == "" || leaseOwner == "" || leaseDuration <= 0 {
		return false, executionbiz.ErrWakeRejected
	}
	now := service.now()
	return store.ClaimTuttiModeExecutionWake(
		ctx, workspaceID, wakeID, leaseOwner, now, now.Add(leaseDuration),
	)
}

func (service Service) RecoverMainWakes(
	ctx context.Context,
	workspaceID string,
	leaseOwner string,
) error {
	store := service.wakeStore()
	if store == nil || service.MainWakeTargets == nil {
		return ErrServiceUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	leaseOwner = strings.TrimSpace(leaseOwner)
	if workspaceID == "" || leaseOwner == "" {
		return executionbiz.ErrWakeRejected
	}
	wakes, err := store.ListDispatchableTuttiModeMainWakes(
		ctx, workspaceID, service.now(),
	)
	if err != nil {
		return err
	}
	for _, wake := range wakes {
		if wake.SourceSessionID != wake.TargetSessionID {
			message := "wake target does not match execution source session"
			if failErr := store.FailTuttiModeExecutionWakeIntegrity(
				ctx, workspaceID, wake.ID, message, service.now(),
			); failErr != nil {
				return errors.Join(executionbiz.ErrWakeIntegrity, failErr)
			}
			return fmt.Errorf("%w: %s", executionbiz.ErrWakeIntegrity, message)
		}
		observation, observeErr := service.MainWakeTargets.ObserveSourceSession(
			ctx, workspaceID, wake.SourceSessionID,
		)
		if observeErr != nil {
			return observeErr
		}
		if !observation.Exists || observation.Busy {
			continue
		}
		claimed, claimErr := service.ClaimMainWake(
			ctx, workspaceID, wake.ID, leaseOwner, defaultMainWakeLeaseDuration,
		)
		if claimErr != nil {
			return claimErr
		}
		if !claimed {
			continue
		}
		// Liveness is re-observed after the durable claim so a Turn that became
		// busy in the check-to-claim window cannot receive overlapping input.
		observation, observeErr = service.MainWakeTargets.ObserveSourceSession(
			ctx, workspaceID, wake.SourceSessionID,
		)
		if observeErr != nil || !observation.Exists || observation.Busy {
			message := "source session became unavailable"
			if observeErr != nil {
				message = observeErr.Error()
			}
			if releaseErr := store.ReleaseTuttiModeExecutionWake(
				ctx, workspaceID, wake.ID, leaseOwner, message, service.now(),
			); releaseErr != nil {
				return errors.Join(observeErr, releaseErr)
			}
			continue
		}
		if err := service.DispatchClaimedMainWake(
			ctx, workspaceID, wake.ID, leaseOwner,
		); err != nil {
			return err
		}
	}
	return nil
}

func (service Service) StartupRecoverMainWakes(
	ctx context.Context,
	workspaceID string,
	leaseOwner string,
) error {
	if err := service.PrepareStartupMainWakeRecovery(ctx, workspaceID); err != nil {
		return err
	}
	return service.RecoverMainWakes(ctx, workspaceID, leaseOwner)
}

// PrepareStartupMainWakeRecovery performs only durable local repair. Product
// wiring calls this while the daemon graph is still being built, then enqueues
// actual Agent delivery for the daemon-ready reconciliation seam.
func (service Service) PrepareStartupMainWakeRecovery(
	ctx context.Context,
	workspaceID string,
) error {
	store := service.wakeStore()
	if store == nil {
		return ErrServiceUnavailable
	}
	now := service.now()
	if err := store.CancelSuppressedTuttiModeExecutionWakes(
		ctx, strings.TrimSpace(workspaceID), now,
	); err != nil {
		return err
	}
	if err := store.RequeueExpiredTuttiModeExecutionWakes(
		ctx, strings.TrimSpace(workspaceID), now,
	); err != nil {
		return err
	}
	return nil
}

func (service Service) DispatchClaimedMainWake(
	ctx context.Context,
	workspaceID string,
	wakeID string,
	leaseOwner string,
) error {
	store := service.wakeStore()
	if store == nil || service.MainWakeTargets == nil {
		return ErrServiceUnavailable
	}
	wake, found, err := store.GetTuttiModeExecutionWake(
		ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(wakeID),
	)
	if err != nil {
		return err
	}
	if !found || wake.Status != executionbiz.WakeStatusLeased ||
		wake.LeaseOwner != strings.TrimSpace(leaseOwner) {
		return executionbiz.ErrWakeRejected
	}
	if wake.SourceSessionID != wake.TargetSessionID {
		message := "wake target does not match execution source session"
		if failErr := store.FailTuttiModeExecutionWakeIntegrity(
			ctx, wake.WorkspaceID, wake.ID, message, service.now(),
		); failErr != nil {
			return errors.Join(executionbiz.ErrWakeIntegrity, failErr)
		}
		return fmt.Errorf("%w: %s", executionbiz.ErrWakeIntegrity, message)
	}
	prompt := MainWakePrompt(wake)
	delivery, sendErr := service.MainWakeTargets.SendMainWake(
		ctx, wake.WorkspaceID, wake.TargetSessionID, wake.ClientSubmitID, prompt,
	)
	if sendErr != nil {
		turnID, found, findErr := service.MainWakeTargets.FindMainWakeTurn(
			ctx, wake.WorkspaceID, wake.TargetSessionID, wake.ClientSubmitID,
		)
		if findErr == nil && found && strings.TrimSpace(turnID) != "" {
			delivery = MainWakeDelivery{
				CanonicalSessionID: wake.TargetSessionID,
				CanonicalTurnID:    strings.TrimSpace(turnID),
			}
		} else {
			message := sendErr.Error()
			if findErr != nil {
				message += "; canonical lookup: " + findErr.Error()
			}
			if releaseErr := store.ReleaseTuttiModeExecutionWake(
				ctx, wake.WorkspaceID, wake.ID, wake.LeaseOwner, message, service.now(),
			); releaseErr != nil {
				return errors.Join(sendErr, findErr, releaseErr)
			}
			return errors.Join(ErrMainWakeDeliveryPending, sendErr, findErr)
		}
	}
	delivery.CanonicalSessionID = strings.TrimSpace(delivery.CanonicalSessionID)
	delivery.CanonicalTurnID = strings.TrimSpace(delivery.CanonicalTurnID)
	if delivery.CanonicalSessionID != wake.TargetSessionID ||
		delivery.CanonicalTurnID == "" {
		message := "wake delivery returned invalid canonical identity"
		if releaseErr := store.ReleaseTuttiModeExecutionWake(
			ctx, wake.WorkspaceID, wake.ID, wake.LeaseOwner, message, service.now(),
		); releaseErr != nil {
			return releaseErr
		}
		return ErrMainWakeDeliveryPending
	}
	return store.MarkTuttiModeExecutionWakeDispatched(
		ctx, wake.WorkspaceID, wake.ID, wake.LeaseOwner,
		delivery.CanonicalSessionID, delivery.CanonicalTurnID, service.now(),
	)
}

// ObserveMainWakeTurnSettled is the idempotent product observer seam used by
// canonical root-Turn settlement fan-out. Unrelated session/Turn pairs are
// successful no-ops and cannot resolve their checkpoint.
func (service Service) ObserveMainWakeTurnSettled(
	ctx context.Context,
	workspaceID string,
	sessionID string,
	turnID string,
) error {
	store := service.wakeStore()
	if store == nil {
		return ErrServiceUnavailable
	}
	_, err := store.MarkTuttiModeExecutionWakeTurnSettled(
		ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(sessionID),
		strings.TrimSpace(turnID), service.now(),
	)
	return err
}

func (service Service) wakeStore() WakeStore {
	if service.Wakes != nil {
		return service.Wakes
	}
	store, _ := service.Store.(WakeStore)
	return store
}
