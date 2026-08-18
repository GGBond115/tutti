package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	appServerRequestQueueCapacity    = 128
	appServerRequestCriticalReserve  = 16
	appServerRequestInFlightLimit    = 16
	appServerSchedulerResponseMethod = "__tutti/server-response"
)

var ErrAppServerBackpressure = errors.New("app-server request scheduler overloaded")

type appServerRequestPriority uint8

const (
	appServerRequestCritical appServerRequestPriority = iota
	appServerRequestInteractive
	appServerRequestBackground
)

func (p appServerRequestPriority) String() string {
	switch p {
	case appServerRequestCritical:
		return "critical"
	case appServerRequestInteractive:
		return "interactive"
	default:
		return "background"
	}
}

type AppServerBackpressureError struct {
	Priority   string
	QueueDepth int
}

func (e *AppServerBackpressureError) Error() string {
	return fmt.Sprintf("%v: priority=%s queue_depth=%d", ErrAppServerBackpressure, e.Priority, e.QueueDepth)
}

func (*AppServerBackpressureError) Unwrap() error { return ErrAppServerBackpressure }

type appServerScheduledResult struct {
	raw []byte
	err error
}

type appServerScheduledRequest struct {
	ctx         context.Context
	method      string
	priority    appServerRequestPriority
	lane        string
	coalesceKey string
	enqueuedAt  time.Time
	run         func(context.Context) ([]byte, error)
	done        chan appServerScheduledResult
}

type appServerCoalescedRequest struct {
	done   chan struct{}
	result appServerScheduledResult
}

type appServerRequestSchedulerTelemetry struct {
	CriticalQueueDepth    int
	InteractiveQueueDepth int
	BackgroundQueueDepth  int
	InFlight              int
	Rejected              uint64
	Coalesced             uint64
	Completed             uint64
	MaxWait               time.Duration
}

type appServerRequestScheduler struct {
	mu sync.Mutex

	queues    [3][]*appServerScheduledRequest
	inFlight  int
	laneBusy  map[string]bool
	coalesced map[string]*appServerCoalescedRequest
	closed    bool
	telemetry appServerRequestSchedulerTelemetry
	// admissionHook is a test-only channel barrier installed by scheduler
	// concurrency tests. Production construction leaves it nil.
	admissionHook func(method string, coalesced bool)
}

func newAppServerRequestScheduler() *appServerRequestScheduler {
	return &appServerRequestScheduler{
		laneBusy: make(map[string]bool), coalesced: make(map[string]*appServerCoalescedRequest),
	}
}

func (s *appServerRequestScheduler) do(
	ctx context.Context,
	method string,
	params any,
	run func(context.Context) ([]byte, error),
) ([]byte, error) {
	if s == nil {
		return run(ctx)
	}
	priority := appServerRequestPriorityForMethod(method)
	lane := appServerRequestMutationLane(method, params)
	coalesceKey := appServerSafeReadCoalesceKey(method, params)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSessionDisconnected
	}
	if coalesceKey != "" {
		if existing := s.coalesced[coalesceKey]; existing != nil {
			s.telemetry.Coalesced++
			hook := s.admissionHook
			s.mu.Unlock()
			if hook != nil {
				hook(method, true)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-existing.done:
				return append([]byte(nil), existing.result.raw...), existing.result.err
			}
		}
	}
	if overload := s.admissionErrorLocked(priority); overload != nil {
		s.telemetry.Rejected++
		s.mu.Unlock()
		return nil, overload
	}
	request := &appServerScheduledRequest{
		ctx: ctx, method: method, priority: priority, lane: lane, coalesceKey: coalesceKey,
		enqueuedAt: time.Now(), run: run, done: make(chan appServerScheduledResult, 1),
	}
	if coalesceKey != "" {
		s.coalesced[coalesceKey] = &appServerCoalescedRequest{done: make(chan struct{})}
	}
	s.queues[priority] = append(s.queues[priority], request)
	s.updateDepthTelemetryLocked()
	s.dispatchLocked()
	hook := s.admissionHook
	s.mu.Unlock()
	if hook != nil {
		hook(method, false)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-request.done:
		return result.raw, result.err
	}
}

func (s *appServerRequestScheduler) dispatchLocked() {
	for s.inFlight < appServerRequestInFlightLimit {
		request := s.takeNextLocked()
		if request == nil {
			return
		}
		s.inFlight++
		if request.lane != "" {
			s.laneBusy[request.lane] = true
		}
		wait := time.Since(request.enqueuedAt)
		if wait > s.telemetry.MaxWait {
			s.telemetry.MaxWait = wait
		}
		s.updateDepthTelemetryLocked()
		go s.execute(request)
	}
}

func (s *appServerRequestScheduler) takeNextLocked() *appServerScheduledRequest {
	for priority := appServerRequestCritical; priority <= appServerRequestBackground; priority++ {
		queue := s.queues[priority]
		for index, request := range queue {
			if request.lane != "" && s.laneBusy[request.lane] {
				continue
			}
			copy(queue[index:], queue[index+1:])
			queue[len(queue)-1] = nil
			s.queues[priority] = queue[:len(queue)-1]
			return request
		}
	}
	return nil
}

func (s *appServerRequestScheduler) execute(request *appServerScheduledRequest) {
	result := appServerScheduledResult{}
	select {
	case <-request.ctx.Done():
		result.err = request.ctx.Err()
	default:
		result.raw, result.err = request.run(request.ctx)
	}
	result.raw = append([]byte(nil), result.raw...)

	s.mu.Lock()
	s.inFlight--
	if request.lane != "" {
		delete(s.laneBusy, request.lane)
	}
	s.telemetry.Completed++
	if request.coalesceKey != "" {
		if shared := s.coalesced[request.coalesceKey]; shared != nil {
			shared.result = result
			delete(s.coalesced, request.coalesceKey)
			close(shared.done)
		}
	}
	s.dispatchLocked()
	s.mu.Unlock()
	request.done <- result
}

func (s *appServerRequestScheduler) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	var queued []*appServerScheduledRequest
	for index := range s.queues {
		queued = append(queued, s.queues[index]...)
		s.queues[index] = nil
	}
	s.updateDepthTelemetryLocked()
	for _, request := range queued {
		if request.coalesceKey != "" {
			if shared := s.coalesced[request.coalesceKey]; shared != nil {
				shared.result.err = ErrSessionDisconnected
				delete(s.coalesced, request.coalesceKey)
				close(shared.done)
			}
		}
	}
	s.mu.Unlock()
	for _, request := range queued {
		request.done <- appServerScheduledResult{err: ErrSessionDisconnected}
	}
}

func (s *appServerRequestScheduler) snapshot() appServerRequestSchedulerTelemetry {
	if s == nil {
		return appServerRequestSchedulerTelemetry{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.telemetry
	value.InFlight = s.inFlight
	value.CriticalQueueDepth = len(s.queues[appServerRequestCritical])
	value.InteractiveQueueDepth = len(s.queues[appServerRequestInteractive])
	value.BackgroundQueueDepth = len(s.queues[appServerRequestBackground])
	return value
}

func (s *appServerRequestScheduler) queueDepthLocked() int {
	return len(s.queues[appServerRequestCritical]) + len(s.queues[appServerRequestInteractive]) + len(s.queues[appServerRequestBackground])
}

func (s *appServerRequestScheduler) admissionErrorLocked(priority appServerRequestPriority) error {
	queued := s.queueDepthLocked()
	normalLimit := appServerRequestQueueCapacity - appServerRequestCriticalReserve
	if (priority != appServerRequestCritical && queued >= normalLimit) || queued >= appServerRequestQueueCapacity {
		return &AppServerBackpressureError{Priority: priority.String(), QueueDepth: queued}
	}
	return nil
}

func (s *appServerRequestScheduler) updateDepthTelemetryLocked() {
	s.telemetry.CriticalQueueDepth = len(s.queues[appServerRequestCritical])
	s.telemetry.InteractiveQueueDepth = len(s.queues[appServerRequestInteractive])
	s.telemetry.BackgroundQueueDepth = len(s.queues[appServerRequestBackground])
	s.telemetry.InFlight = s.inFlight
}

func appServerRequestPriorityForMethod(method string) appServerRequestPriority {
	switch strings.TrimSpace(method) {
	case appServerMethodTurnInterrupt, appServerSchedulerResponseMethod:
		return appServerRequestCritical
	case appServerMethodThreadStart, appServerMethodThreadResume, appServerMethodThreadUnsubscribe,
		appServerMethodTurnStart, appServerMethodTurnSteer, appServerMethodThreadFork,
		appServerMethodThreadRollback, appServerMethodThreadCompact, appServerMethodReviewStart,
		appServerMethodThreadGoalSet, appServerMethodThreadGoalClear:
		return appServerRequestInteractive
	default:
		return appServerRequestBackground
	}
}

func appServerRequestMutationLane(method string, params any) string {
	if appServerSafeReadCoalesceKey(method, params) != "" {
		return ""
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	var values map[string]any
	if json.Unmarshal(encoded, &values) != nil {
		return ""
	}
	threadID := strings.TrimSpace(asString(values["threadId"]))
	if threadID == "" {
		return ""
	}
	return "thread:" + threadID
}

func appServerSafeReadCoalesceKey(method string, params any) string {
	if strings.TrimSpace(method) != appServerMethodModelList {
		return ""
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return ""
	}
	return appServerMethodModelList + ":" + string(encoded)
}
