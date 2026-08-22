package devicepresence

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	authbridge "github.com/tutti-os/tutti/packages/auth/bridge-go"
)

type presenceAccountStub struct {
	mu      sync.RWMutex
	session *authbridge.Session
}

func (s *presenceAccountStub) ReadSession() (*authbridge.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.session == nil {
		return nil, nil
	}
	copy := *s.session
	return &copy, nil
}

type presenceControlStub struct {
	mu                 sync.Mutex
	openCount          int
	heartbeatCount     int
	registerCount      int
	closeLeaseIDs      []string
	failFirstRenewal   bool
	presenceSessionIDs []string
}

func (s *presenceControlStub) RegisterCurrentDevice(context.Context, string, DeviceMetadata) (RegisteredDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerCount++
	return RegisteredDevice{UserDeviceID: "user-device-1", DeviceID: "device-1"}, nil
}

func (s *presenceControlStub) OpenSession(_ context.Context, _, _, presenceSessionID string) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openCount++
	s.presenceSessionIDs = append(s.presenceSessionIDs, presenceSessionID)
	return Lease{
		PresenceLeaseID: "lease-" + string(rune('0'+s.openCount)), UserDeviceID: "user-device-1",
		HeartbeatIntervalSeconds: 30,
	}, nil
}

func (s *presenceControlStub) Heartbeat(context.Context, string, string) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatCount++
	if s.failFirstRenewal && s.heartbeatCount == 2 {
		return Lease{}, &ControlPlaneError{StatusCode: http.StatusNotFound, Code: "PRESENCE_LEASE_NOT_FOUND"}
	}
	return Lease{State: "DEVICE_PRESENCE_SESSION_STATE_ACTIVE"}, nil
}

func (s *presenceControlStub) CloseSession(_ context.Context, _, leaseID string) error {
	s.mu.Lock()
	s.closeLeaseIDs = append(s.closeLeaseIDs, leaseID)
	s.mu.Unlock()
	return nil
}

func TestServiceReopensExpiredLeaseAfterLateHeartbeat(t *testing.T) {
	account := &presenceAccountStub{session: &authbridge.Session{Cookie: "session_id=session-1", UserID: "user-1"}}
	control := &presenceControlStub{failFirstRenewal: true}
	service := NewService(account, control, DeviceMetadata{DeviceID: "device-1"})
	service.HeartbeatEvery = 5 * time.Millisecond
	service.RetryMin = time.Millisecond
	service.RetryMax = 5 * time.Millisecond
	service.Start()

	waitForPresenceTest(t, func() bool {
		control.mu.Lock()
		defer control.mu.Unlock()
		return control.openCount >= 2 && control.heartbeatCount >= 3
	})
	service.Stop(context.Background())

	control.mu.Lock()
	defer control.mu.Unlock()
	if control.registerCount < 2 || control.openCount != 2 {
		t.Fatalf("registers=%d opens=%d", control.registerCount, control.openCount)
	}
	if len(control.presenceSessionIDs) != 2 || control.presenceSessionIDs[0] != control.presenceSessionIDs[1] {
		t.Fatalf("process session IDs = %#v", control.presenceSessionIDs)
	}
	if len(control.closeLeaseIDs) != 1 || control.closeLeaseIDs[0] != "lease-2" {
		t.Fatalf("closed leases = %#v", control.closeLeaseIDs)
	}
}

func TestServiceStartIsIdempotentAndRestartable(t *testing.T) {
	account := &presenceAccountStub{session: &authbridge.Session{Cookie: "session_id=session-1", UserID: "user-1"}}
	control := &presenceControlStub{}
	service := NewService(account, control, DeviceMetadata{DeviceID: "device-1"})
	service.HeartbeatEvery = time.Hour

	service.Start()
	service.Start()
	waitForPresenceTest(t, func() bool {
		control.mu.Lock()
		defer control.mu.Unlock()
		return control.heartbeatCount >= 1
	})
	service.Stop(context.Background())
	service.Start()
	waitForPresenceTest(t, func() bool {
		control.mu.Lock()
		defer control.mu.Unlock()
		return control.openCount >= 2
	})
	service.Stop(context.Background())

	control.mu.Lock()
	defer control.mu.Unlock()
	if control.openCount != 2 || len(control.closeLeaseIDs) != 2 {
		t.Fatalf("opens=%d closes=%#v", control.openCount, control.closeLeaseIDs)
	}
}

func TestServiceWaitsForAChangedSessionAfterUnauthorized(t *testing.T) {
	account := &presenceAccountStub{session: &authbridge.Session{Cookie: "session_id=session-1", UserID: "user-1"}}
	control := &presenceControlStub{failFirstRenewal: true}
	service := NewService(account, &unauthorizedRenewalControl{presenceControlStub: control}, DeviceMetadata{DeviceID: "device-1"})
	service.HeartbeatEvery = 5 * time.Millisecond
	service.AccountPollEvery = time.Millisecond
	service.Start()
	waitForPresenceTest(t, func() bool {
		control.mu.Lock()
		defer control.mu.Unlock()
		return control.heartbeatCount >= 2
	})

	account.mu.Lock()
	account.session = &authbridge.Session{Cookie: "session_id=session-2", UserID: "user-1"}
	account.mu.Unlock()
	waitForPresenceTest(t, func() bool {
		control.mu.Lock()
		defer control.mu.Unlock()
		return control.openCount >= 2
	})
	service.Stop(context.Background())
}

type unauthorizedRenewalControl struct {
	*presenceControlStub
}

func (s *unauthorizedRenewalControl) Heartbeat(ctx context.Context, cookie, leaseID string) (Lease, error) {
	lease, err := s.presenceControlStub.Heartbeat(ctx, cookie, leaseID)
	if presenceErr, ok := err.(*ControlPlaneError); ok && presenceErr.StatusCode == http.StatusNotFound {
		presenceErr.StatusCode = http.StatusUnauthorized
	}
	return lease, err
}

func waitForPresenceTest(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for device presence worker")
}
