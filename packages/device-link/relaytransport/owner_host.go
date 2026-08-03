package relaytransport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// OwnerHost maintains one Relay owner tunnel while at least one product driver
// holds a reference. It is safe for concurrent use.
type OwnerHost struct {
	cfg OwnerHostConfig

	mu       sync.Mutex
	refs     map[string]int
	refCount int
	run      *ownerRun
}

type ownerRun struct {
	cancel    context.CancelFunc
	done      chan struct{}
	lifecycle OwnerLifecycle
	handlers  sync.WaitGroup

	mu      sync.Mutex
	session OwnerSession
}

// NewOwnerHost validates config and creates an idle owner host.
func NewOwnerHost(cfg OwnerHostConfig) (*OwnerHost, error) {
	if cfg.LifecycleFactory == nil {
		return nil, errors.New("relay owner lifecycle factory is required")
	}
	if cfg.Handler == nil {
		return nil, errors.New("relay owner stream handler is required")
	}
	if cfg.StableSessionFor <= 0 {
		cfg.StableSessionFor = 30 * time.Second
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = 20 * time.Second
	}
	if cfg.PongTimeout <= cfg.PingInterval {
		cfg.PongTimeout = cfg.PingInterval * 3
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepContext
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &OwnerHost{cfg: cfg, refs: make(map[string]int)}, nil
}

// Acquire starts the owner tunnel on the first reference. driver is a
// product-owned demand key and may be acquired more than once.
func (h *OwnerHost) Acquire(ctx context.Context, driver string) error {
	if h == nil {
		return errors.New("relay owner host is nil")
	}
	driver = strings.TrimSpace(driver)
	if driver == "" {
		return errors.New("relay owner acquire requires driver")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.refs[driver]++
	h.refCount++
	if h.refCount != 1 {
		return nil
	}
	lifecycle := h.cfg.LifecycleFactory.NewOwnerLifecycle()
	if lifecycle == nil {
		h.refs[driver]--
		if h.refs[driver] == 0 {
			delete(h.refs, driver)
		}
		h.refCount--
		return errors.New("relay owner lifecycle factory returned nil")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run := &ownerRun{cancel: cancel, done: make(chan struct{}), lifecycle: lifecycle}
	h.run = run
	go h.runLoop(runCtx, run)
	return nil
}

// Release drops one driver reference. The final release synchronously stops
// the tunnel, joins stream handlers, and releases the exact product lifecycle.
func (h *OwnerHost) Release(driver string) error {
	if h == nil {
		return errors.New("relay owner host is nil")
	}
	driver = strings.TrimSpace(driver)
	if driver == "" {
		return errors.New("relay owner release requires driver")
	}

	var run *ownerRun
	h.mu.Lock()
	count := h.refs[driver]
	if count == 0 {
		h.mu.Unlock()
		return fmt.Errorf("relay owner release without acquire for driver %q", driver)
	}
	if count == 1 {
		delete(h.refs, driver)
	} else {
		h.refs[driver] = count - 1
	}
	h.refCount--
	if h.refCount == 0 {
		run = h.run
		h.run = nil
	}
	h.mu.Unlock()

	if run == nil {
		return nil
	}
	run.cancel()
	<-run.done
	run.handlers.Wait()
	session := run.currentSession()
	err := run.lifecycle.Release(context.Background(), session)
	h.observe(OwnerEvent{Phase: OwnerPhaseRelease, Outcome: outcome(err), SessionKey: session.Key, Error: err})
	return err
}

// RefCount returns the number of current product references.
func (h *OwnerHost) RefCount() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.refCount
}

func (h *OwnerHost) runLoop(ctx context.Context, run *ownerRun) {
	defer close(run.done)
	backoff := newExponentialBackoff(h.cfg.Backoff)
	for ctx.Err() == nil {
		session, err := run.lifecycle.Prepare(ctx)
		if strings.TrimSpace(session.Key) != "" {
			run.setSession(session)
		}
		h.observe(OwnerEvent{Phase: OwnerPhasePrepare, Outcome: outcome(err), SessionKey: session.Key, Error: err})
		var readyFor time.Duration
		if err == nil {
			readyFor, err = h.runSession(ctx, run, session)
			if readyFor >= h.cfg.StableSessionFor {
				backoff.Reset()
			}
		}
		if ctx.Err() != nil {
			return
		}
		run.lifecycle.SessionEnded(session, err)
		h.observe(OwnerEvent{Phase: OwnerPhaseSession, Outcome: OwnerOutcomeEnded, SessionKey: session.Key, Error: err})
		backoffDelay := backoff.Next()
		retryAfter := retryDelay(err, h.cfg.Now())
		delay := combineRetryDelay(backoffDelay, retryAfter)
		h.observe(OwnerEvent{
			Phase: OwnerPhaseRetry, Outcome: OwnerOutcomeScheduled, SessionKey: session.Key,
			Retry: &OwnerRetryObservation{
				Delay: delay, BackoffCap: backoff.Cap(), BackoffDelay: backoffDelay, RetryAfter: retryAfter,
			},
			Error: err,
		})
		if sleepErr := h.cfg.Sleep(ctx, delay); sleepErr != nil {
			return
		}
	}
}

func (h *OwnerHost) runSession(ctx context.Context, run *ownerRun, session OwnerSession) (time.Duration, error) {
	ws, err := dialWebSocket(ctx, session.Dial)
	if err != nil {
		return 0, err
	}
	conn := newWebSocketByteConn(ws)
	defer func() { _ = conn.Close() }()
	stopLiveness, err := startLiveness(ctx, ws, livenessConfig{
		pingInterval: h.cfg.PingInterval,
		pongTimeout:  h.cfg.PongTimeout,
		pingPayload:  session.PingPayload,
		sessionKey:   session.Key,
		observe:      h.observe,
	})
	if err != nil {
		return 0, err
	}
	defer stopLiveness()
	h.observe(OwnerEvent{Phase: OwnerPhaseDial, Outcome: OwnerOutcomeConnected, SessionKey: session.Key})

	deactivate, err := run.lifecycle.Activate(ctx, session)
	if err != nil {
		return 0, err
	}
	if deactivate == nil {
		deactivate = func() {}
	}
	defer deactivate()

	yamuxConfig := yamux.DefaultConfig()
	yamuxConfig.EnableKeepAlive = false
	// The Host reports transport failures through OwnerEvent. Do not let the
	// underlying mux bypass product log redaction by writing to stderr.
	yamuxConfig.LogOutput = io.Discard
	mux, err := yamux.Server(conn, yamuxConfig)
	if err != nil {
		return 0, fmt.Errorf("start relay owner mux: %w", err)
	}
	defer func() { _ = mux.Close() }()
	readyAt := h.cfg.Now()
	h.observe(OwnerEvent{Phase: OwnerPhaseServe, Outcome: OwnerOutcomeReady, SessionKey: session.Key})

	cancelDone := make(chan struct{})
	defer close(cancelDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = mux.Close()
			_ = conn.Close()
		case <-cancelDone:
		}
	}()

	for {
		stream, acceptErr := mux.AcceptStream()
		if acceptErr != nil {
			readyFor := elapsed(readyAt, h.cfg.Now())
			if ctx.Err() != nil {
				return readyFor, ctx.Err()
			}
			return readyFor, fmt.Errorf("accept relay owner stream: %w", acceptErr)
		}
		run.handlers.Add(1)
		go h.handleStream(ctx, run, session.Key, stream)
	}
}

func (h *OwnerHost) handleStream(ctx context.Context, run *ownerRun, sessionKey string, stream net.Conn) {
	defer run.handlers.Done()
	defer func() { _ = stream.Close() }()
	err := h.cfg.Handler.HandleRelayStream(ctx, stream)
	h.observe(OwnerEvent{Phase: OwnerPhaseStream, Outcome: outcome(err), SessionKey: sessionKey, Error: err})
}

func (h *OwnerHost) observe(event OwnerEvent) {
	if h.cfg.Observe != nil {
		h.cfg.Observe(event)
	}
}

func (r *ownerRun) setSession(session OwnerSession) {
	r.mu.Lock()
	r.session = session
	r.mu.Unlock()
}

func (r *ownerRun) currentSession() OwnerSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.session
}

func outcome(err error) OwnerOutcome {
	if err != nil {
		return OwnerOutcomeFailed
	}
	return OwnerOutcomeSucceeded
}

func elapsed(start, end time.Time) time.Duration {
	if start.IsZero() || !end.After(start) {
		return 0
	}
	return end.Sub(start)
}
