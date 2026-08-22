package devicepresence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPControlPlaneDevicePresenceLifecycleContract(t *testing.T) {
	requests := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session_id=session-1" || r.Header.Get("x-zk-ppe-lane") != "ppe-a" {
			t.Fatalf("authorization headers = %#v", r.Header)
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "PUT /devices/current":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, exists := body["publicIdentity"]; exists {
				t.Fatalf("presence registration unexpectedly sent identity: %s", body["publicIdentity"])
			}
			_, _ = w.Write([]byte(`{"device":{"userDeviceId":"user-device-1","deviceId":"device-1"}}`))
		case "POST /device-presence/sessions":
			_, _ = w.Write([]byte(`{"presenceLeaseId":"lease-1","userDeviceId":"user-device-1","state":"DEVICE_PRESENCE_SESSION_STATE_PENDING","heartbeatIntervalSeconds":30,"authorityGeneration":"generation-1"}`))
		case "POST /device-presence/sessions/lease-1/heartbeat":
			_, _ = w.Write([]byte(`{"state":"DEVICE_PRESENCE_SESSION_STATE_ACTIVE","authorityGeneration":"generation-1"}`))
		case "DELETE /device-presence/sessions/lease-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	control := &HTTPControlPlane{
		BaseURL: server.URL, HTTPClient: server.Client(),
		Headers: http.Header{"x-zk-ppe-lane": []string{"ppe-a"}},
	}
	metadata := DeviceMetadata{
		DeviceID: "device-1", ReportedName: "Desktop", Platform: "windows", Arch: "amd64", ClientVersion: "1.2.3",
	}
	if _, err := control.RegisterCurrentDevice(context.Background(), "session_id=session-1", metadata); err != nil {
		t.Fatalf("register device: %v", err)
	}
	lease, err := control.OpenSession(context.Background(), "session_id=session-1", "device-1", "session-1")
	if err != nil {
		t.Fatalf("open presence: %v", err)
	}
	if _, err := control.Heartbeat(context.Background(), "session_id=session-1", lease.PresenceLeaseID); err != nil {
		t.Fatalf("heartbeat presence: %v", err)
	}
	if err := control.CloseSession(context.Background(), "session_id=session-1", lease.PresenceLeaseID); err != nil {
		t.Fatalf("close presence: %v", err)
	}
	want := strings.Join([]string{
		"PUT /devices/current",
		"POST /device-presence/sessions",
		"POST /device-presence/sessions/lease-1/heartbeat",
		"DELETE /device-presence/sessions/lease-1",
	}, "\n")
	if got := strings.Join(requests, "\n"); got != want {
		t.Fatalf("requests:\n%s\nwant:\n%s", got, want)
	}
}

func TestHTTPControlPlanePreservesUnknownAndUnavailableErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"DEVICE_PRESENCE_UNAVAILABLE","reason":"recovering"}}`))
	}))
	defer server.Close()
	control := &HTTPControlPlane{BaseURL: server.URL, HTTPClient: server.Client()}

	_, err := control.Heartbeat(context.Background(), "cookie", "lease-1")
	presenceErr, ok := err.(*ControlPlaneError)
	if !ok || !presenceErr.IsStatus(http.StatusServiceUnavailable) || presenceErr.Code != "DEVICE_PRESENCE_UNAVAILABLE" {
		t.Fatalf("control-plane error = %#v", err)
	}
}
