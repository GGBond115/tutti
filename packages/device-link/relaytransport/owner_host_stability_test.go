package relaytransport

import (
	"context"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"
)

func TestOwnerHostResetsBackoffOnlyAfterStableReadySession(t *testing.T) {
	const seed = int64(73)
	relay := newTestOwnerRelay(t)
	defer relay.Close()
	lifecycle := newTestOwnerLifecycle(testOwnerSession(relay.OwnerEndpoint(), "owner-stability"))
	clock := newOwnerTestClock(time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC))
	ready := make(chan struct{}, 4)
	delays := make(chan time.Duration, 4)
	host := newTestOwnerHost(t, lifecycle, StreamHandlerFunc(func(context.Context, net.Conn) error { return nil }), func(cfg *OwnerHostConfig) {
		cfg.StableSessionFor = 30 * time.Second
		cfg.Now = clock.Now
		cfg.Backoff = BackoffConfig{
			Initial:     100 * time.Millisecond,
			Max:         time.Second,
			Multiplier:  2,
			RandFactory: func() *rand.Rand { return rand.New(rand.NewSource(seed)) },
		}
		cfg.Sleep = func(ctx context.Context, _ time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		}
		cfg.Observe = func(event OwnerEvent) {
			if event.Phase == OwnerPhaseServe && event.Outcome == OwnerOutcomeReady {
				ready <- struct{}{}
			}
			if event.Phase == OwnerPhaseRetry && event.Outcome == OwnerOutcomeScheduled {
				delays <- event.Retry.Delay
			}
		}
	})

	if err := host.Acquire(context.Background(), "owner"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Release("owner") }()

	first := relay.WaitSession(t)
	waitOwnerReady(t, ready)
	clock.Advance(29*time.Second + 999*time.Millisecond)
	first.Close()
	expectedRandom := rand.New(rand.NewSource(seed))
	if got, want := waitOwnerRetry(t, delays), time.Duration(expectedRandom.Int63n(int64(100*time.Millisecond)+1)); got != want {
		t.Fatalf("first retry = %s, want %s", got, want)
	}

	second := relay.WaitSession(t)
	waitOwnerReady(t, ready)
	clock.Advance(29*time.Second + 999*time.Millisecond)
	second.Close()
	if got, want := waitOwnerRetry(t, delays), time.Duration(expectedRandom.Int63n(int64(200*time.Millisecond)+1)); got != want {
		t.Fatalf("second retry = %s, want growing cap result %s", got, want)
	}

	third := relay.WaitSession(t)
	waitOwnerReady(t, ready)
	clock.Advance(30 * time.Second)
	third.Close()
	if got, want := waitOwnerRetry(t, delays), time.Duration(expectedRandom.Int63n(int64(100*time.Millisecond)+1)); got != want {
		t.Fatalf("retry after stable session = %s, want reset cap result %s", got, want)
	}
}

func waitOwnerReady(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("owner session did not become ready")
	}
}

func waitOwnerRetry(t *testing.T, delays <-chan time.Duration) time.Duration {
	t.Helper()
	select {
	case delay := <-delays:
		return delay
	case <-time.After(2 * time.Second):
		t.Fatal("owner session did not schedule a retry")
		return 0
	}
}

type ownerTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newOwnerTestClock(now time.Time) *ownerTestClock { return &ownerTestClock{now: now} }

func (c *ownerTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *ownerTestClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
}
