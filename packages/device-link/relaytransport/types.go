package relaytransport

import (
	"context"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"time"
)

// DialRequest describes one Relay byte-stream dial. Query and Header are
// cloned before use so a concurrent product refresh cannot mutate an in-flight
// handshake.
type DialRequest struct {
	Endpoint    string
	Query       url.Values
	Header      http.Header
	Subprotocol string
}

// StreamHandler consumes one stream opened over an owner tunnel.
type StreamHandler interface {
	HandleRelayStream(ctx context.Context, stream net.Conn) error
}

// StreamHandlerFunc adapts a function to StreamHandler.
type StreamHandlerFunc func(context.Context, net.Conn) error

func (f StreamHandlerFunc) HandleRelayStream(ctx context.Context, stream net.Conn) error {
	return f(ctx, stream)
}

// OwnerSession contains the transport material prepared by one product-owned
// lifecycle. Key is an opaque, non-secret correlation key such as an authority
// ID. It must not contain a token or application identifier.
type OwnerSession struct {
	Key         string
	Dial        DialRequest
	PingPayload []byte
}

// OwnerLifecycle owns product state for exactly one zero-to-one Host demand
// lifecycle. A Host never reuses a lifecycle after its final Release.
type OwnerLifecycle interface {
	// Prepare returns current owner-tunnel material. It may return a partial
	// session together with an error so Release can clean up product state that
	// was committed before preparation failed.
	Prepare(ctx context.Context) (OwnerSession, error)
	// Activate is a readiness barrier after the WebSocket connects and before
	// relay streams are accepted. The returned function stops maintenance and
	// joins any goroutines started by Activate.
	Activate(ctx context.Context, session OwnerSession) (deactivate func(), err error)
	// SessionEnded lets the product invalidate credentials or projections based
	// on a completed connection attempt. It must not block.
	SessionEnded(session OwnerSession, err error)
	// Release removes product state for this exact business lifecycle.
	Release(ctx context.Context, session OwnerSession) error
}

// OwnerLifecycleFactory creates isolated product state for each Host business
// lifecycle. Isolation prevents a delayed Release from detaching a newer run.
type OwnerLifecycleFactory interface {
	NewOwnerLifecycle() OwnerLifecycle
}

// OwnerLifecycleFactoryFunc adapts a function to OwnerLifecycleFactory.
type OwnerLifecycleFactoryFunc func() OwnerLifecycle

func (f OwnerLifecycleFactoryFunc) NewOwnerLifecycle() OwnerLifecycle { return f() }

// OwnerPhase identifies one stable phase in the owner-tunnel lifecycle. New
// phases may be added compatibly; observers must ignore phases they do not
// recognize.
type OwnerPhase string

const (
	OwnerPhasePrepare  OwnerPhase = "prepare"
	OwnerPhaseDial     OwnerPhase = "dial"
	OwnerPhaseServe    OwnerPhase = "serve"
	OwnerPhaseSession  OwnerPhase = "session"
	OwnerPhaseRetry    OwnerPhase = "retry"
	OwnerPhaseStream   OwnerPhase = "stream"
	OwnerPhaseLiveness OwnerPhase = "liveness"
	OwnerPhaseRelease  OwnerPhase = "release"
)

// OwnerOutcome identifies the result of one owner-tunnel phase. New outcomes
// may be added compatibly; observers must ignore outcomes they do not recognize.
type OwnerOutcome string

const (
	OwnerOutcomeSucceeded    OwnerOutcome = "succeeded"
	OwnerOutcomeFailed       OwnerOutcome = "failed"
	OwnerOutcomeConnected    OwnerOutcome = "connected"
	OwnerOutcomeReady        OwnerOutcome = "ready"
	OwnerOutcomeEnded        OwnerOutcome = "ended"
	OwnerOutcomeScheduled    OwnerOutcome = "scheduled"
	OwnerOutcomePingSent     OwnerOutcome = "ping_sent"
	OwnerOutcomePongReceived OwnerOutcome = "pong_received"
	OwnerOutcomeStopped      OwnerOutcome = "stopped"
)

// OwnerEvent is a sanitized transport observation. Product adapters decide how
// to map it to logs or metrics and must sanitize Error before persistence.
type OwnerEvent struct {
	Phase      OwnerPhase
	Outcome    OwnerOutcome
	SessionKey string
	Retry      *OwnerRetryObservation
	Liveness   *OwnerLivenessObservation
	Error      error
}

// OwnerRetryObservation describes one scheduled reconnect without product
// identifiers or credentials.
type OwnerRetryObservation struct {
	Delay        time.Duration
	BackoffCap   time.Duration
	BackoffDelay time.Duration
	RetryAfter   time.Duration
}

// OwnerLivenessObservation describes WebSocket ping/pong progress. LastPongAt
// is populated on the stopped event after at least one pong was received.
type OwnerLivenessObservation struct {
	PingCount  int64
	PongCount  int64
	At         time.Time
	LastPongAt time.Time
}

// OwnerObserver receives synchronous, non-payload transport observations. It
// must return quickly and must not call Host methods.
type OwnerObserver func(OwnerEvent)

// BackoffConfig configures full-jitter reconnect backoff.
type BackoffConfig struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	// RandFactory is called once per zero-to-one owner lifecycle. It may return
	// a seeded generator for deterministic tests; nil uses an isolated random
	// generator. The Host never shares one returned generator between runs.
	RandFactory func() *rand.Rand
}

// OwnerHostConfig configures one reference-counted Relay owner tunnel.
type OwnerHostConfig struct {
	LifecycleFactory OwnerLifecycleFactory
	Handler          StreamHandler
	Backoff          BackoffConfig
	StableSessionFor time.Duration
	PingInterval     time.Duration
	PongTimeout      time.Duration
	Sleep            func(context.Context, time.Duration) error
	Now              func() time.Time
	Observe          OwnerObserver
}
