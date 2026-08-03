package deviceauthority

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClientDeviceAuthorityOwnerLifecycle(t *testing.T) {
	t.Parallel()

	fixedTime := time.Date(2026, time.August, 2, 9, 10, 11, 123, time.UTC)
	privateKey := ed25519.NewKeyFromSeed(bytesOf(7, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyID := GatewayIdentityKeyID(publicKey)
	identitySource := staticIdentitySource{identity: SigningIdentity{KeyID: keyID, Signer: privateKey}}
	seen := make(map[string]bool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("X-Product-Auth"); got != "current-account" {
			t.Errorf("X-Product-Auth = %q, want current-account", got)
		}
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/control/v1/device-authorities/ensure":
			seen["ensure"] = true
			if got := r.Header.Get("X-Owner-User-ID"); got != "owner-1" {
				t.Errorf("ensure X-Owner-User-ID = %q, want owner-1", got)
			}
			var body struct {
				OwnerUserID string `json:"ownerUserId"`
				RuntimeID   string `json:"runtimeId"`
			}
			decodeJSONRequest(t, r, &body)
			if body.OwnerUserID != "owner-1" || body.RuntimeID != "runtime-1" {
				t.Errorf("ensure body = %+v", body)
			}
			writeJSONResponse(t, w, map[string]any{
				"authorityId": "deva_123",
				"state":       "online",
				"relay": map[string]any{
					"hostEndpoint": "wss://relay.example/host",
					"dialEndpoint": "wss://relay.example/dial",
					"relayNodeId":  "relay-1",
				},
				"lease": map[string]any{"ttlSeconds": 120, "renewIntervalSeconds": 30},
				"gatewayEnrollment": map[string]any{
					"proof":     "proof-1",
					"expiresAt": "2026-08-02T09:11:11Z",
				},
			})
		case "/control/v1/gateway/device-authorities/deva_123/identity/enroll":
			seen["enroll"] = true
			if got := r.Header.Get("X-Owner-User-ID"); got != "" {
				t.Errorf("enroll X-Owner-User-ID = %q, want empty", got)
			}
			var body struct {
				AuthorityID     string `json:"authorityId"`
				RuntimeID       string `json:"runtimeId"`
				EnrollmentProof string `json:"enrollmentProof"`
				KeyID           string `json:"keyId"`
				Algorithm       string `json:"algorithm"`
				PublicKey       string `json:"publicKey"`
			}
			decodeJSONRequest(t, r, &body)
			decodedPublicKey, err := base64.RawURLEncoding.DecodeString(body.PublicKey)
			if err != nil {
				t.Errorf("decode enrollment public key: %v", err)
			}
			if body.AuthorityID != "deva_123" || body.RuntimeID != "runtime-1" ||
				body.EnrollmentProof != "proof-1" || body.KeyID != keyID ||
				body.Algorithm != "ed25519" || !reflect.DeepEqual(decodedPublicKey, []byte(publicKey)) {
				t.Errorf("enrollment body = %+v", body)
			}
			writeJSONResponse(t, w, map[string]any{
				"identity": map[string]any{
					"authorityId": "deva_123",
					"identityId":  "identity-1",
					"keyId":       keyID,
				},
			})
		case "/control/v1/gateway/device-authorities/deva_123/owner-tunnel-token":
			seen["issue"] = true
			var body struct {
				AuthorityID      string   `json:"authorityId"`
				RuntimeID        string   `json:"runtimeId"`
				KeyID            string   `json:"keyId"`
				Nonce            string   `json:"nonce"`
				Timestamp        string   `json:"timestamp"`
				Signature        string   `json:"signature"`
				SupportedTargets []string `json:"supportedTargets"`
				TTLSeconds       int      `json:"ttlSeconds"`
			}
			decodeJSONRequest(t, r, &body)
			// Build the verifier-side fixture independently from the client
			// helper so a simultaneous bug in signing and test setup cannot pass.
			payload := strings.Join([]string{
				"tsh.gateway.owner-session.v1",
				"authority_id=" + body.AuthorityID,
				"runtime_id=" + body.RuntimeID,
				"key_id=" + body.KeyID,
				"nonce=" + body.Nonce,
				"timestamp=" + body.Timestamp,
				"ttl_seconds=600",
				"supported_targets=device-gateway",
			}, "\n")
			signature, err := base64.RawURLEncoding.DecodeString(body.Signature)
			if err != nil || !ed25519.Verify(publicKey, []byte(payload), signature) {
				t.Errorf("owner token signature is invalid: decode error = %v", err)
			}
			if body.Nonce != "fixed-nonce" || body.Timestamp != fixedTime.Format(time.RFC3339Nano) ||
				body.TTLSeconds != 600 || !reflect.DeepEqual(body.SupportedTargets, []string{" DEVICE-GATEWAY ", "device-gateway"}) {
				t.Errorf("owner token body = %+v", body)
			}
			writeJSONResponse(t, w, map[string]any{
				"authorityId": "deva_123",
				"state":       "online",
				"ownerTunnelToken": map[string]any{
					"token":     "owner-token",
					"expiresAt": "2026-08-02T09:20:11Z",
				},
				"relay": map[string]any{
					"hostEndpoint": "wss://relay.example/host",
					"dialEndpoint": "wss://relay.example/dial",
					"relayNodeId":  "relay-1",
				},
				"lease":             map[string]any{"ttlSeconds": 120, "renewIntervalSeconds": 30},
				"gatewayIdentityId": keyID,
			})
		case "/control/v1/device-authorities/deva_123/lease/renew":
			seen["renew"] = true
			if got := r.Header.Get("X-Owner-User-ID"); got != "owner-1" {
				t.Errorf("renew X-Owner-User-ID = %q, want owner-1", got)
			}
			var body struct {
				AuthorityID       string `json:"authorityId"`
				OwnerUserID       string `json:"ownerUserId"`
				RuntimeID         string `json:"runtimeId"`
				TTLSeconds        int    `json:"ttlSeconds"`
				VMStatus          string `json:"vmStatus"`
				OwnerTunnelStatus string `json:"ownerTunnelStatus"`
			}
			decodeJSONRequest(t, r, &body)
			if body.AuthorityID != "deva_123" || body.OwnerUserID != "owner-1" ||
				body.RuntimeID != "runtime-1" || body.TTLSeconds != 120 ||
				body.VMStatus != "ready" || body.OwnerTunnelStatus != "connected" {
				t.Errorf("renew body = %+v", body)
			}
			writeJSONResponse(t, w, map[string]any{
				"authorityId": "deva_123",
				"state":       "online",
				"renewedAt":   "2026-08-02T09:12:11Z",
				"expiresAt":   "2026-08-02T09:14:11Z",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:    server.URL + "/control/",
		APIPrefix:  "v1/",
		HTTPClient: server.Client(),
		PrepareRequest: func(req *http.Request, metadata RequestMetadata) error {
			req.Header.Set("X-Product-Auth", "current-account")
			if metadata.OwnerUserID != "" {
				req.Header.Set("X-Owner-User-ID", metadata.OwnerUserID)
			}
			return nil
		},
		Identities: identitySource,
		Now:        func() time.Time { return fixedTime },
		Nonce:      func() (string, error) { return "fixed-nonce", nil },
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	authority, err := client.EnsureDeviceAuthority(context.Background(), EnsureDeviceAuthorityRequest{
		OwnerUserID: " owner-1 ",
		RuntimeID:   " runtime-1 ",
	})
	if err != nil {
		t.Fatalf("EnsureDeviceAuthority() error = %v", err)
	}
	if authority.AuthorityID != "deva_123" || authority.Relay.RelayNodeID != "relay-1" ||
		authority.Lease.RenewIntervalSeconds != 30 || authority.GatewayEnrollment.Proof != "proof-1" {
		t.Fatalf("authority = %+v", authority)
	}

	identity, err := client.RegisterDeviceGatewayIdentity(context.Background(), RegisterDeviceGatewayIdentityRequest{
		AuthorityID:     authority.AuthorityID,
		RuntimeID:       authority.RuntimeID,
		EnrollmentProof: authority.GatewayEnrollment.Proof,
	})
	if err != nil {
		t.Fatalf("RegisterDeviceGatewayIdentity() error = %v", err)
	}
	if identity.KeyID != keyID || identity.IdentityID != "identity-1" {
		t.Fatalf("identity = %+v", identity)
	}

	token, err := client.IssueDeviceGatewayOwnerTunnelToken(context.Background(), IssueDeviceGatewayOwnerTunnelTokenRequest{
		AuthorityID:      authority.AuthorityID,
		RuntimeID:        authority.RuntimeID,
		SupportedTargets: []string{" DEVICE-GATEWAY ", "device-gateway"},
	})
	if err != nil {
		t.Fatalf("IssueDeviceGatewayOwnerTunnelToken() error = %v", err)
	}
	if token.Token.Value != "owner-token" || token.IdentityID != identity.KeyID || token.Relay.HostEndpoint == "" {
		t.Fatalf("token = %+v", token)
	}

	renewed, err := client.RenewDeviceAuthorityLease(context.Background(), RenewDeviceAuthorityLeaseRequest{
		AuthorityID:       authority.AuthorityID,
		OwnerUserID:       authority.OwnerUserID,
		RuntimeID:         authority.RuntimeID,
		TTLSeconds:        authority.Lease.TTLSeconds,
		OwnerTunnelStatus: " connected ",
		VMStatus:          " ready ",
	})
	if err != nil {
		t.Fatalf("RenewDeviceAuthorityLease() error = %v", err)
	}
	if renewed.AuthorityID != authority.AuthorityID || renewed.State != "online" {
		t.Fatalf("renewed = %+v", renewed)
	}

	for _, operation := range []string{"ensure", "enroll", "issue", "renew"} {
		if !seen[operation] {
			t.Errorf("did not observe %s request", operation)
		}
	}
}

func TestClientEnrollmentRetryReusesIdentityAfterLostResponse(t *testing.T) {
	t.Parallel()

	var keyIDs []string
	var publicKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AuthorityID     string `json:"authorityId"`
			RuntimeID       string `json:"runtimeId"`
			EnrollmentProof string `json:"enrollmentProof"`
			KeyID           string `json:"keyId"`
			Algorithm       string `json:"algorithm"`
			PublicKey       string `json:"publicKey"`
		}
		decodeJSONRequest(t, r, &body)
		if body.AuthorityID != "deva_123" || body.RuntimeID != "runtime-1" || body.Algorithm != "ed25519" {
			t.Errorf("enrollment retry body = %+v", body)
		}
		keyIDs = append(keyIDs, body.KeyID)
		publicKeys = append(publicKeys, body.PublicKey)
		if len(keyIDs) == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("test server does not support connection hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack first enrollment response: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		writeJSONResponse(t, w, map[string]any{
			"identity": map[string]any{
				"authorityId": "deva_123",
				"keyId":       body.KeyID,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:    server.URL,
		APIPrefix:  "/v1",
		HTTPClient: server.Client(),
		Identities: NewMemoryIdentitySource(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	firstRequest := RegisterDeviceGatewayIdentityRequest{
		AuthorityID:     "deva_123",
		RuntimeID:       "runtime-1",
		EnrollmentProof: "proof-consumed-with-lost-response",
	}
	if _, err := client.RegisterDeviceGatewayIdentity(context.Background(), firstRequest); err == nil {
		t.Fatal("first RegisterDeviceGatewayIdentity() error = nil, want ambiguous response loss")
	}
	secondRequest := firstRequest
	secondRequest.EnrollmentProof = "fresh-proof"
	if _, err := client.RegisterDeviceGatewayIdentity(context.Background(), secondRequest); err != nil {
		t.Fatalf("second RegisterDeviceGatewayIdentity() error = %v", err)
	}
	if len(keyIDs) != 2 || keyIDs[0] == "" || keyIDs[0] != keyIDs[1] {
		t.Fatalf("enrollment key ids = %v, want same non-empty key", keyIDs)
	}
	if len(publicKeys) != 2 || publicKeys[0] == "" || publicKeys[0] != publicKeys[1] {
		t.Fatalf("enrollment public keys differ across retry")
	}
}

func decodeJSONRequest(t *testing.T, r *http.Request, target any) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", r.Method)
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Errorf("decode request: %v", err)
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func bytesOf(value byte, count int) []byte {
	return bytes.Repeat([]byte{value}, count)
}

type staticIdentitySource struct {
	identity SigningIdentity
	err      error
}

func (s staticIdentitySource) Identity(context.Context, string) (SigningIdentity, error) {
	return s.identity, s.err
}
