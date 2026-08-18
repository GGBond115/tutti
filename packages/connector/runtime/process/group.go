package process

import (
	"context"
	"errors"
	"sync"
	"time"
)

type trackedProcess struct {
	connection Connection
	cancel     context.CancelFunc
	closing    bool
}

// Group fences process starts and owns every process launched for one
// Connector route. A late process start cannot escape route retirement.
type Group struct {
	mu            sync.Mutex
	processes     map[uint64]trackedProcess
	pendingStarts map[uint64]context.CancelFunc
	nextProcessID uint64
	fenced        bool
}

func NewGroup() *Group {
	return &Group{
		processes:     make(map[uint64]trackedProcess),
		pendingStarts: make(map[uint64]context.CancelFunc),
	}
}

func (group *Group) Begin(parent context.Context) (context.Context, uint64, bool) {
	group.mu.Lock()
	defer group.mu.Unlock()
	if group.fenced {
		return nil, 0, false
	}
	processContext, cancel := context.WithCancel(parent)
	group.nextProcessID++
	group.pendingStarts[group.nextProcessID] = cancel
	return processContext, group.nextProcessID, true
}

func (group *Group) FailStart(processID uint64) {
	group.mu.Lock()
	cancel := group.pendingStarts[processID]
	delete(group.pendingStarts, processID)
	group.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (group *Group) CommitStart(processID uint64, connection Connection) bool {
	group.mu.Lock()
	defer group.mu.Unlock()
	cancel := group.pendingStarts[processID]
	delete(group.pendingStarts, processID)
	if group.fenced || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return false
	}
	group.processes[processID] = trackedProcess{connection: connection, cancel: cancel}
	return true
}

func (group *Group) Release(processID uint64, connection Connection) {
	_ = group.ReleaseWithError(processID, connection)
}

func (group *Group) ReleaseWithError(processID uint64, connection Connection) error {
	group.mu.Lock()
	current, owned := group.processes[processID]
	if owned && current.connection == connection && !current.closing {
		delete(group.processes, processID)
	} else {
		owned = false
	}
	group.mu.Unlock()
	if !owned {
		return nil
	}
	current.cancel()
	return connection.Close()
}

func (group *Group) Fence() {
	group.mu.Lock()
	group.fenced = true
	for processID, cancel := range group.pendingStarts {
		cancel()
		delete(group.pendingStarts, processID)
	}
	group.mu.Unlock()
}

func (group *Group) IsFenced() bool {
	group.mu.Lock()
	defer group.mu.Unlock()
	return group.fenced
}

func (group *Group) ActiveCount() int {
	group.mu.Lock()
	defer group.mu.Unlock()
	return len(group.processes)
}

func (group *Group) Close(deadline time.Time) error {
	if group == nil {
		return nil
	}
	group.Fence()
	group.mu.Lock()
	processes := make(map[uint64]trackedProcess, len(group.processes))
	for processID, candidate := range group.processes {
		candidate.closing = true
		group.processes[processID] = candidate
		processes[processID] = candidate
	}
	group.mu.Unlock()

	type closeResult struct {
		processID uint64
		err       error
	}
	results := make(chan closeResult, len(processes))
	for processID, candidate := range processes {
		candidate.cancel()
		go func(processID uint64, connection Connection) {
			results <- closeResult{processID: processID, err: connection.Close()}
		}(processID, candidate.connection)
	}
	var closeErrors []error
	for range processes {
		var result closeResult
		if deadline.IsZero() {
			result = <-results
		} else {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return errors.Join(append(closeErrors, context.DeadlineExceeded)...)
			}
			timer := time.NewTimer(remaining)
			select {
			case result = <-results:
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
				return errors.Join(append(closeErrors, context.DeadlineExceeded)...)
			}
		}
		if result.err != nil {
			closeErrors = append(closeErrors, result.err)
			continue
		}
		group.mu.Lock()
		if current, exists := group.processes[result.processID]; exists &&
			current.connection == processes[result.processID].connection {
			delete(group.processes, result.processID)
		}
		group.mu.Unlock()
	}
	return errors.Join(closeErrors...)
}
