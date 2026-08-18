package daemon

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"
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

	mu           sync.Mutex
	registered   map[string]struct{}
	unexpected   map[string]struct{}
	health       map[string]WorkerHealth
	onUnexpected func(string)
	sealed       bool
	stopping     bool
	wait         sync.WaitGroup
	done         chan struct{}
}

func newWorkerGroup(parent context.Context, onUnexpected func(string)) *workerGroup {
	ctx, cancel := context.WithCancel(parent)
	return &workerGroup{
		ctx:          ctx,
		cancel:       cancel,
		registered:   make(map[string]struct{}),
		unexpected:   make(map[string]struct{}),
		health:       make(map[string]WorkerHealth),
		onUnexpected: onUnexpected,
		done:         make(chan struct{}),
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
	group.health[name] = WorkerHealth{Name: name, Status: WorkerStatusRunning, StartedAt: time.Now().UTC()}
	group.wait.Add(1)
	go func() {
		reporter := workerHealthReporter{group: group, name: name}
		run(context.WithValue(group.ctx, workerHealthReporterContextKey{}, reporter))
		group.mu.Lock()
		unexpected := !group.stopping
		if unexpected {
			group.unexpected[name] = struct{}{}
			health := group.health[name]
			health.Status = WorkerStatusFailed
			health.ExitedAt = time.Now().UTC()
			health.LastFailureAt = health.ExitedAt
			if health.ConsecutiveFailures < math.MaxUint32 {
				health.ConsecutiveFailures++
			}
			health.BackoffUntil = time.Time{}
			health.FailureCode = "unexpected_exit"
			group.health[name] = health
		} else {
			health := group.health[name]
			health.Status = WorkerStatusStopped
			health.ExitedAt = time.Now().UTC()
			group.health[name] = health
		}
		group.mu.Unlock()
		group.wait.Done()
		if unexpected && group.onUnexpected != nil {
			group.onUnexpected(name)
		}
	}()
	return nil
}

func (group *workerGroup) unexpectedNames() []string {
	if group == nil {
		return nil
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	names := make([]string, 0, len(group.unexpected))
	for name := range group.unexpected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type WorkerHealthSnapshot struct {
	Lifecycle       LifecycleState
	UnexpectedExits []string
	Workers         []WorkerHealth
}

type WorkerStatus string

const (
	WorkerStatusRunning WorkerStatus = "running"
	WorkerStatusFailed  WorkerStatus = "failed"
	WorkerStatusStopped WorkerStatus = "stopped"
)

type WorkerHealth struct {
	Name        string
	Status      WorkerStatus
	StartedAt   time.Time
	ExitedAt    time.Time
	LastSuccess time.Time
	// LastFailureAt and FailureCode retain the most recent historical failure
	// after recovery; ConsecutiveFailures and BackoffUntil describe current debt.
	LastFailureAt       time.Time
	ConsecutiveFailures uint32
	BackoffUntil        time.Time
	FailureBudget       *uint32
	Exhausted           bool
	FailureCode         string
}

type workerHealthReporterContextKey struct{}

type workerHealthReporter struct {
	group *workerGroup
	name  string
}

func workerReporter(ctx context.Context) workerHealthReporter {
	if ctx == nil {
		return workerHealthReporter{}
	}
	reporter, _ := ctx.Value(workerHealthReporterContextKey{}).(workerHealthReporter)
	return reporter
}

func (reporter workerHealthReporter) Success(at time.Time) {
	if reporter.group == nil || reporter.name == "" {
		return
	}
	reporter.group.mu.Lock()
	defer reporter.group.mu.Unlock()
	health, ok := reporter.group.health[reporter.name]
	if !ok {
		return
	}
	health.LastSuccess = at.UTC()
	health.ConsecutiveFailures = 0
	health.BackoffUntil = time.Time{}
	health.FailureBudget = nil
	health.Exhausted = false
	reporter.group.health[reporter.name] = health
}

func (reporter workerHealthReporter) Failure(at time.Time, code string, backoffUntil time.Time, failureBudget *uint32) {
	if reporter.group == nil || reporter.name == "" {
		return
	}
	reporter.group.mu.Lock()
	defer reporter.group.mu.Unlock()
	health, ok := reporter.group.health[reporter.name]
	if !ok {
		return
	}
	health.LastFailureAt = at.UTC()
	if health.ConsecutiveFailures < math.MaxUint32 {
		health.ConsecutiveFailures++
	}
	health.BackoffUntil = backoffUntil.UTC()
	health.FailureCode = code
	health.FailureBudget = cloneFailureBudget(failureBudget)
	health.Exhausted = failureBudget != nil && health.ConsecutiveFailures >= *failureBudget
	reporter.group.health[reporter.name] = health
}

func (reporter workerHealthReporter) Reset() {
	if reporter.group == nil || reporter.name == "" {
		return
	}
	reporter.group.mu.Lock()
	defer reporter.group.mu.Unlock()
	health, ok := reporter.group.health[reporter.name]
	if !ok {
		return
	}
	health.ConsecutiveFailures = 0
	health.BackoffUntil = time.Time{}
	health.FailureBudget = nil
	health.Exhausted = false
	reporter.group.health[reporter.name] = health
}

func cloneFailureBudget(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (group *workerGroup) healthSnapshot() []WorkerHealth {
	if group == nil {
		return nil
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	result := make([]WorkerHealth, 0, len(group.health))
	for _, health := range group.health {
		health.FailureBudget = cloneFailureBudget(health.FailureBudget)
		result = append(result, health)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
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
		group.mu.Lock()
		group.stopping = true
		group.mu.Unlock()
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
