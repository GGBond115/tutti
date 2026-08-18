package daemon

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// LifecycleState is the externally observable daemon lifecycle. NewHost is
// deliberately side-effect free; workers exist only while the Host is running.
type LifecycleState string

const (
	LifecycleStateCreated  LifecycleState = "created"
	LifecycleStateStarting LifecycleState = "starting"
	LifecycleStateRunning  LifecycleState = "running"
	LifecycleStateFailed   LifecycleState = "failed"
	LifecycleStateStopping LifecycleState = "stopping"
	LifecycleStateStopped  LifecycleState = "stopped"
)

var errHostNotRunning = errors.New("connector market host is not running")

type workerGroup struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	registered map[string]struct{}
	sealed     bool
	wait       sync.WaitGroup
	done       chan struct{}
}

func newWorkerGroup(parent context.Context) *workerGroup {
	ctx, cancel := context.WithCancel(parent)
	return &workerGroup{
		ctx:        ctx,
		cancel:     cancel,
		registered: make(map[string]struct{}),
		done:       make(chan struct{}),
	}
}

func (group *workerGroup) Go(name string, run func(context.Context)) error {
	if group == nil || run == nil || name == "" {
		return errors.New("connector daemon worker registration is invalid")
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	if group.sealed {
		return errors.New("connector daemon worker group is sealed")
	}
	if _, exists := group.registered[name]; exists {
		return errors.New("connector daemon worker is already registered: " + name)
	}
	group.registered[name] = struct{}{}
	group.wait.Add(1)
	go func() {
		defer group.wait.Done()
		run(group.ctx)
	}()
	return nil
}

func (group *workerGroup) Seal() {
	if group == nil {
		return
	}
	group.mu.Lock()
	if group.sealed {
		group.mu.Unlock()
		return
	}
	group.sealed = true
	group.mu.Unlock()
	go func() {
		group.wait.Wait()
		close(group.done)
	}()
}

func (group *workerGroup) Stop() {
	if group != nil {
		group.cancel()
	}
}

func (group *workerGroup) Wait(ctx context.Context) error {
	if group == nil {
		return nil
	}
	select {
	case <-group.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (group *workerGroup) names() []string {
	if group == nil {
		return nil
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	names := make([]string, 0, len(group.registered))
	for name := range group.registered {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
