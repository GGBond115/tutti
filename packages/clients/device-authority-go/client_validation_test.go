package deviceauthority

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClientValidatesEndpointAndDependencies(t *testing.T) {
	t.Parallel()

	identitySource := testIdentitySource(t)
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "missing base URL", cfg: Config{APIPrefix: "/v1", Identities: identitySource}},
		{name: "unsupported scheme", cfg: Config{BaseURL: "file:///tmp/socket", APIPrefix: "/v1", Identities: identitySource}},
		{name: "missing host", cfg: Config{BaseURL: "https:///v1", APIPrefix: "/v1", Identities: identitySource}},
		{name: "URL credentials", cfg: Config{BaseURL: "https://user:secret@example.test", APIPrefix: "/v1", Identities: identitySource}},
		{name: "URL query", cfg: Config{BaseURL: "https://example.test?lane=ppe", APIPrefix: "/v1", Identities: identitySource}},
		{name: "URL fragment", cfg: Config{BaseURL: "https://example.test#fragment", APIPrefix: "/v1", Identities: identitySource}},
		{name: "missing API prefix", cfg: Config{BaseURL: "https://example.test", Identities: identitySource}},
		{name: "root API prefix", cfg: Config{BaseURL: "https://example.test", APIPrefix: "/", Identities: identitySource}},
		{name: "API prefix query", cfg: Config{BaseURL: "https://example.test", APIPrefix: "/v1?lane=ppe", Identities: identitySource}},
		{name: "missing identity source", cfg: Config{BaseURL: "https://example.test", APIPrefix: "/v1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewClient(tt.cfg); err == nil {
				t.Fatal("NewClient() error = nil")
			}
		})
	}

	client, err := NewClient(Config{
		BaseURL:    " https://example.test/base/ ",
		APIPrefix:  " desktop/v1/ ",
		Identities: identitySource,
	})
	if err != nil {
		t.Fatalf("NewClient(valid config) error = %v", err)
	}
	if got := client.endpoint("/resource"); got != "https://example.test/base/desktop/v1/resource" {
		t.Fatalf("endpoint = %q", got)
	}
	defaultClient, ok := client.httpClient.(*http.Client)
	if !ok || defaultClient.Timeout != defaultHTTPTimeout {
		t.Fatalf("default HTTP client = %#v, want timeout %s", client.httpClient, defaultHTTPTimeout)
	}
}

func TestClientHTTPErrorIsBoundedAndPreservesRetryMetadata(t *testing.T) {
	t.Parallel()

	client := mustClientWithDoer(t, httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Retry-After": []string{" 17 "}},
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxHTTPResponseBodyBytes+100))),
		}, nil
	}))

	err := client.doJSON(context.Background(), http.MethodPost, "/test", map[string]string{"ok": "yes"}, nil, RequestMetadata{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T %v, want *HTTPError", err, err)
	}
	if httpErr.StatusCode != http.StatusServiceUnavailable || httpErr.HTTPStatusCode() != http.StatusServiceUnavailable ||
		httpErr.RetryAfter != "17" || httpErr.HTTPRetryAfter() != "17" || len(httpErr.Body) != maxHTTPErrorBodyBytes {
		t.Fatalf("HTTPError = %+v, body length = %d", httpErr, len(httpErr.Body))
	}
	if httpErr.Error() != "control-plane request failed (503)" {
		t.Fatalf("HTTPError.Error() = %q", httpErr.Error())
	}
	if strings.Contains(httpErr.Error(), httpErr.Body) {
		t.Fatal("HTTPError.Error() exposes the upstream response body")
	}
}

func TestClientRejectsOversizedOrMalformedSuccessfulResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "oversized", body: strings.Repeat("x", maxHTTPResponseBodyBytes+1)},
		{name: "malformed JSON", body: "{"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mustClientWithDoer(t, httpDoerFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(tt.body)),
				}, nil
			}))
			var response map[string]any
			if err := client.doJSON(context.Background(), http.MethodPost, "/test", map[string]string{}, &response, RequestMetadata{}); err == nil {
				t.Fatal("doJSON() error = nil")
			}
		})
	}
}

func TestClientStopsBeforeTransportForCanceledContextOrHeaderFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context func() context.Context
		prepare PrepareRequestFunc
	}{
		{
			name: "canceled context",
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{
			name:    "product header failure",
			context: context.Background,
			prepare: func(*http.Request, RequestMetadata) error { return errors.New("account unavailable") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			client, err := NewClient(Config{
				BaseURL:   "https://example.test",
				APIPrefix: "/v1",
				HTTPClient: httpDoerFunc(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return nil, errors.New("unexpected transport call")
				}),
				PrepareRequest: tt.prepare,
				Identities:     testIdentitySource(t),
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if err := client.doJSON(tt.context(), http.MethodPost, "/test", map[string]string{}, nil, RequestMetadata{}); err == nil {
				t.Fatal("doJSON() error = nil")
			}
			if calls.Load() != 0 {
				t.Fatalf("transport calls = %d, want 0", calls.Load())
			}
		})
	}
}

func TestClientRejectsEmptyHTTPResponse(t *testing.T) {
	t.Parallel()

	client := mustClientWithDoer(t, httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))
	if err := client.doJSON(context.Background(), http.MethodPost, "/test", map[string]string{}, nil, RequestMetadata{}); err == nil {
		t.Fatal("doJSON() error = nil")
	}
}

func TestClientRejectsCredentialBindingMismatches(t *testing.T) {
	t.Parallel()

	identitySource := testIdentitySource(t)
	keyID := identitySource.identity.KeyID
	tests := []struct {
		name     string
		response func(request map[string]any) string
		call     func(context.Context, *Client) error
	}{
		{
			name: "enrollment key id",
			response: func(map[string]any) string {
				return `{"identity":{"authorityId":"deva_123","keyId":"unexpected-key"}}`
			},
			call: enrollTestCall,
		},
		{
			name: "enrollment authority id",
			response: func(map[string]any) string {
				return fmt.Sprintf(`{"identity":{"authorityId":"deva_other","keyId":%q}}`, keyID)
			},
			call: enrollTestCall,
		},
		{
			name: "owner token key id",
			response: func(map[string]any) string {
				return `{"authorityId":"deva_123","ownerTunnelToken":{"token":"token"},"gatewayIdentityId":"unexpected-key"}`
			},
			call: issueTestCall,
		},
		{
			name: "owner token authority id",
			response: func(map[string]any) string {
				return fmt.Sprintf(`{"authorityId":"deva_other","ownerTunnelToken":{"token":"token"},"gatewayIdentityId":%q}`, keyID)
			},
			call: issueTestCall,
		},
		{
			name: "renewed authority id",
			response: func(map[string]any) string {
				return `{"authorityId":"deva_other","state":"online"}`
			},
			call: renewTestCall,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mustClientWithIdentityAndDoer(t, identitySource, httpDoerFunc(func(req *http.Request) (*http.Response, error) {
				var request map[string]any
				if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
					return nil, err
				}
				return jsonHTTPResponse(http.StatusOK, tt.response(request)), nil
			}))
			if err := tt.call(context.Background(), client); err == nil {
				t.Fatal("call error = nil, want binding mismatch")
			}
		})
	}
}

func TestClientPropagatesIdentityAndSigningFailuresWithoutHTTP(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	doer := httpDoerFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected transport call")
	})
	client := mustClientWithIdentityAndDoer(t, staticIdentitySource{err: errors.New("keychain locked")}, doer)
	if err := enrollTestCall(context.Background(), client); err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Fatalf("enrollment error = %v", err)
	}

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	client = mustClientWithIdentityAndDoer(t, staticIdentitySource{identity: SigningIdentity{
		KeyID:  GatewayIdentityKeyID(publicKey),
		Signer: failingEd25519Signer{publicKey: publicKey},
	}}, doer)
	if err := issueTestCall(context.Background(), client); err == nil || !strings.Contains(err.Error(), "signing unavailable") {
		t.Fatalf("owner token error = %v", err)
	}
	client = mustClientWithIdentityAndDoer(t, staticIdentitySource{identity: SigningIdentity{
		KeyID:  GatewayIdentityKeyID(publicKey),
		Signer: shortSignatureSigner{publicKey: publicKey},
	}}, doer)
	if err := issueTestCall(context.Background(), client); err == nil || !strings.Contains(err.Error(), "signature length") {
		t.Fatalf("short signature error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("transport calls = %d, want 0", calls.Load())
	}
}

func TestClientRejectsZeroClockBeforeSigning(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, err := NewClient(Config{
		BaseURL:   "https://example.test",
		APIPrefix: "/v1",
		HTTPClient: httpDoerFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("unexpected transport call")
		}),
		Identities: testIdentitySource(t),
		Now:        func() time.Time { return time.Time{} },
		Nonce:      func() (string, error) { return "nonce-1", nil },
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := issueTestCall(context.Background(), client); err == nil || !strings.Contains(err.Error(), "non-zero clock") {
		t.Fatalf("owner token error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("transport calls = %d, want 0", calls.Load())
	}
}

func mustClientWithDoer(t *testing.T, doer HTTPDoer) *Client {
	t.Helper()
	return mustClientWithIdentityAndDoer(t, testIdentitySource(t), doer)
}

func mustClientWithIdentityAndDoer(t *testing.T, identities IdentitySource, doer HTTPDoer) *Client {
	t.Helper()
	client, err := NewClient(Config{
		BaseURL:    "https://example.test",
		APIPrefix:  "/v1",
		HTTPClient: doer,
		Identities: identities,
		Now:        func() time.Time { return time.Date(2026, 8, 2, 9, 10, 11, 0, time.UTC) },
		Nonce:      func() (string, error) { return "nonce-1", nil },
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func testIdentitySource(t *testing.T) staticIdentitySource {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return staticIdentitySource{identity: SigningIdentity{
		KeyID:  GatewayIdentityKeyID(publicKey),
		Signer: privateKey,
	}}
}

func enrollTestCall(ctx context.Context, client *Client) error {
	_, err := client.RegisterDeviceGatewayIdentity(ctx, RegisterDeviceGatewayIdentityRequest{
		AuthorityID:     "deva_123",
		RuntimeID:       "runtime-1",
		EnrollmentProof: "proof-1",
	})
	return err
}

func issueTestCall(ctx context.Context, client *Client) error {
	_, err := client.IssueDeviceGatewayOwnerTunnelToken(ctx, IssueDeviceGatewayOwnerTunnelTokenRequest{
		AuthorityID:      "deva_123",
		RuntimeID:        "runtime-1",
		SupportedTargets: []string{"device-gateway"},
	})
	return err
}

func renewTestCall(ctx context.Context, client *Client) error {
	_, err := client.RenewDeviceAuthorityLease(ctx, RenewDeviceAuthorityLeaseRequest{
		AuthorityID: "deva_123",
		OwnerUserID: "owner-1",
		RuntimeID:   "runtime-1",
	})
	return err
}

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jsonHTTPResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type failingEd25519Signer struct {
	publicKey ed25519.PublicKey
}

func (s failingEd25519Signer) Public() crypto.PublicKey { return s.publicKey }

func (failingEd25519Signer) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("signing unavailable")
}

type shortSignatureSigner struct {
	publicKey ed25519.PublicKey
}

func (s shortSignatureSigner) Public() crypto.PublicKey { return s.publicKey }

func (shortSignatureSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return []byte("short"), nil
}
