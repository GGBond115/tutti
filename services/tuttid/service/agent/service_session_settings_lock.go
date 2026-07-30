package agent

import (
	"context"
	"strings"
	"sync"
)

type serviceSessionSettingsLock struct {
	available chan struct{}
	refs      int
}

type serviceSessionSettingsState struct {
	mu    sync.Mutex
	locks map[string]*serviceSessionSettingsLock
}

// acquireSessionSettingsLock serializes runtime resume with durable settings
// read-modify-write for one session. It intentionally does not span provider
// turn execution or unrelated metadata mutations.
func (s *Service) acquireSessionSettingsLock(
	ctx context.Context,
	workspaceID string,
	agentSessionID string,
) (func(), error) {
	if s.sessionSettingsState != nil {
		return acquireServiceSessionSettingsLock(
			ctx,
			&s.sessionSettingsState.mu,
			&s.sessionSettingsState.locks,
			workspaceID,
			agentSessionID,
		)
	}
	return acquireServiceSessionSettingsLock(
		ctx,
		&s.sessionSettingsMu,
		&s.sessionSettingsLocks,
		workspaceID,
		agentSessionID,
	)
}

func (s *Service) sessionSettingsLockIdentity() any {
	if s != nil && s.sessionSettingsState != nil {
		return &s.sessionSettingsState.locks
	}
	return &s.sessionSettingsLocks
}

func acquireServiceSessionSettingsLock(
	ctx context.Context,
	mu *sync.Mutex,
	locks *map[string]*serviceSessionSettingsLock,
	workspaceID string,
	agentSessionID string,
) (func(), error) {
	key := strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(agentSessionID)
	mu.Lock()
	if *locks == nil {
		*locks = make(map[string]*serviceSessionSettingsLock)
	}
	lock := (*locks)[key]
	if lock == nil {
		lock = &serviceSessionSettingsLock{available: make(chan struct{}, 1)}
		lock.available <- struct{}{}
		(*locks)[key] = lock
	}
	lock.refs++
	mu.Unlock()

	select {
	case <-ctx.Done():
		releaseServiceSessionSettingsLockRef(mu, locks, key, lock)
		return nil, ctx.Err()
	case <-lock.available:
	}
	if err := ctx.Err(); err != nil {
		lock.available <- struct{}{}
		releaseServiceSessionSettingsLockRef(mu, locks, key, lock)
		return nil, err
	}
	return func() {
		lock.available <- struct{}{}
		releaseServiceSessionSettingsLockRef(mu, locks, key, lock)
	}, nil
}

func releaseServiceSessionSettingsLockRef(
	mu *sync.Mutex,
	locks *map[string]*serviceSessionSettingsLock,
	key string,
	lock *serviceSessionSettingsLock,
) {
	mu.Lock()
	lock.refs--
	if lock.refs <= 0 && (*locks)[key] == lock {
		delete(*locks, key)
	}
	mu.Unlock()
}
