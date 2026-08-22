package devicepresence

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	authbridge "github.com/tutti-os/tutti/packages/auth/bridge-go"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultRetryMinimum      = time.Second
	defaultRetryMaximum      = 15 * time.Second
	closeTimeout             = 2 * time.Second
)

type AccountSessionSource interface {
	ReadSession() (*authbridge.Session, error)
}

type Service struct {
	Account   AccountSessionSource
	Control   ControlPlane
	Metadata  DeviceMetadata
	SessionID string
	RetryMin  time.Duration
	RetryMax  time.Duration
	// HeartbeatEvery is test-only tuning; zero follows the server interval.
	HeartbeatEvery time.Duration
	// AccountPollEvery is test-only tuning; zero uses one second.
	AccountPollEvery time.Duration
	CloseAfter       time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewService(account AccountSessionSource, control ControlPlane, metadata DeviceMetadata) *Service {
	return &Service{
		Account: account, Control: control, Metadata: metadata, SessionID: uuid.NewString(),
	}
}

// Start is idempotent. One daemon process owns one process-session identifier;
// reconnecting after logout or sleep reuses it while the server issues a new
// exact lease whenever the previous lease has expired.
func (s *Service) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	go func() {
		s.run(ctx)
		s.mu.Lock()
		if s.done == done {
			s.cancel = nil
			s.done = nil
		}
		s.mu.Unlock()
		close(done)
	}()
}

// Stop cancels renewal, waits for the worker to finish, and lets it make one
// bounded best-effort close using the last authenticated cookie and exact lease.
func (s *Service) Stop(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()
	if cancel == nil || done == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (s *Service) run(ctx context.Context) {
	retry := s.retryMinimum()
	for {
		cookie, lease, err := s.openAndActivate(ctx)
		if err != nil {
			if isDevicePresenceStatus(err, http.StatusUnauthorized) && cookie != "" {
				if !s.waitForAccountSessionChange(ctx, cookie) {
					return
				}
				retry = s.retryMinimum()
				continue
			}
			if !waitForDevicePresence(ctx, retry) {
				return
			}
			retry = nextDevicePresenceRetry(retry, s.retryMaximum())
			continue
		}
		retry = s.retryMinimum()
		result := s.renewLease(ctx, cookie, lease)
		if result.stopped {
			s.closeLease(cookie, lease.PresenceLeaseID)
			return
		}
		if result.retryableError != nil {
			slog.Warn("device presence lease will be reopened", "event", "tutti.device_presence.reopen", "error", result.retryableError)
			if isDevicePresenceStatus(result.retryableError, http.StatusUnauthorized) {
				if !s.waitForAccountSessionChange(ctx, cookie) {
					return
				}
				retry = s.retryMinimum()
				continue
			}
			if !isDevicePresenceStatus(result.retryableError, http.StatusNotFound) {
				if !waitForDevicePresence(ctx, retry) {
					return
				}
				retry = nextDevicePresenceRetry(retry, s.retryMaximum())
			}
		}
	}
}

func (s *Service) openAndActivate(ctx context.Context) (string, Lease, error) {
	if s.Account == nil || s.Control == nil || strings.TrimSpace(s.Metadata.DeviceID) == "" || uuid.Validate(s.SessionID) != nil {
		return "", Lease{}, errors.New("device presence service is not configured")
	}
	session, err := s.Account.ReadSession()
	if err != nil || session == nil || strings.TrimSpace(session.Cookie) == "" {
		return "", Lease{}, errors.New("device presence account session is unavailable")
	}
	cookie := strings.TrimSpace(session.Cookie)
	if _, err := s.Control.RegisterCurrentDevice(ctx, cookie, s.Metadata); err != nil {
		return cookie, Lease{}, err
	}
	lease, err := s.Control.OpenSession(ctx, cookie, s.Metadata.DeviceID, s.SessionID)
	if err != nil {
		return cookie, Lease{}, err
	}
	if _, err := s.Control.Heartbeat(ctx, cookie, lease.PresenceLeaseID); err != nil {
		return cookie, Lease{}, err
	}
	return cookie, lease, nil
}

type renewLeaseResult struct {
	stopped        bool
	retryableError error
}

func (s *Service) renewLease(ctx context.Context, cookie string, lease Lease) renewLeaseResult {
	interval := time.Duration(lease.HeartbeatIntervalSeconds) * time.Second
	if s.HeartbeatEvery > 0 {
		interval = s.HeartbeatEvery
	}
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	timer := time.NewTimer(jitterDevicePresenceInterval(interval))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return renewLeaseResult{stopped: true}
		case <-timer.C:
			if _, err := s.Control.Heartbeat(ctx, cookie, lease.PresenceLeaseID); err != nil {
				return renewLeaseResult{retryableError: err}
			}
			timer.Reset(jitterDevicePresenceInterval(interval))
		}
	}
}

func (s *Service) waitForAccountSessionChange(ctx context.Context, rejectedCookie string) bool {
	interval := s.AccountPollEvery
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			session, err := s.Account.ReadSession()
			if err == nil && session != nil {
				cookie := strings.TrimSpace(session.Cookie)
				if cookie != "" && cookie != rejectedCookie {
					return true
				}
			}
		}
	}
}

func (s *Service) closeLease(cookie, leaseID string) {
	if strings.TrimSpace(cookie) == "" || strings.TrimSpace(leaseID) == "" || s.Control == nil {
		return
	}
	timeout := s.CloseAfter
	if timeout <= 0 {
		timeout = closeTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.Control.CloseSession(ctx, cookie, leaseID); err != nil {
		var controlErr *ControlPlaneError
		if !errors.As(err, &controlErr) || !controlErr.IsStatus(http.StatusNotFound) {
			slog.Warn("device presence close failed; lease will expire", "event", "tutti.device_presence.close_failed", "error", err)
		}
	}
}

func (s *Service) retryMinimum() time.Duration {
	if s.RetryMin > 0 {
		return s.RetryMin
	}
	return defaultRetryMinimum
}

func (s *Service) retryMaximum() time.Duration {
	if s.RetryMax > 0 {
		return s.RetryMax
	}
	return defaultRetryMaximum
}

func nextDevicePresenceRetry(current, maximum time.Duration) time.Duration {
	next := current * 2
	if next > maximum {
		return maximum
	}
	return next
}

func jitterDevicePresenceInterval(interval time.Duration) time.Duration {
	delta := interval / 10
	if delta <= 0 {
		return interval
	}
	return interval - delta + time.Duration(rand.Int63n(int64(2*delta)+1))
}

func isDevicePresenceStatus(err error, statusCode int) bool {
	var controlErr *ControlPlaneError
	return errors.As(err, &controlErr) && controlErr.IsStatus(statusCode)
}

func waitForDevicePresence(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
