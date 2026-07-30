package agent

import (
	"context"
	"errors"
	"sync"
)

// TuttiModeSourceActivityObservers is a stable adapter port whose consumers
// may be registered while the larger daemon graph is assembled. Service keeps
// this complete port from construction onward rather than receiving a late
// field injection.
type TuttiModeSourceActivityObservers struct {
	mu        sync.RWMutex
	observers []TuttiModeSourceActivityObserver
}

func (o *TuttiModeSourceActivityObservers) Add(observer TuttiModeSourceActivityObserver) {
	if o == nil || observer == nil {
		return
	}
	o.mu.Lock()
	o.observers = append(o.observers, observer)
	o.mu.Unlock()
}

func (o *TuttiModeSourceActivityObservers) ObserveTuttiModeSourceActivity(
	ctx context.Context,
	activity TuttiModeSourceActivity,
) error {
	if o == nil {
		return nil
	}
	o.mu.RLock()
	observers := append([]TuttiModeSourceActivityObserver(nil), o.observers...)
	o.mu.RUnlock()
	var result error
	for _, observer := range observers {
		result = errors.Join(result, observer.ObserveTuttiModeSourceActivity(ctx, activity))
	}
	return result
}

// TurnCancelObservers provides the same stable construction-time port for
// post-cancel product observers.
type TurnCancelObservers struct {
	mu        sync.RWMutex
	observers []TurnCancelObserver
}

func (o *TurnCancelObservers) Add(observer TurnCancelObserver) {
	if o == nil || observer == nil {
		return
	}
	o.mu.Lock()
	o.observers = append(o.observers, observer)
	o.mu.Unlock()
}

func (o *TurnCancelObservers) ObserveUserTurnCanceled(
	ctx context.Context,
	workspaceID string,
	agentSessionID string,
) {
	if o == nil {
		return
	}
	o.mu.RLock()
	observers := append([]TurnCancelObserver(nil), o.observers...)
	o.mu.RUnlock()
	for _, observer := range observers {
		observer.ObserveUserTurnCanceled(ctx, workspaceID, agentSessionID)
	}
}
