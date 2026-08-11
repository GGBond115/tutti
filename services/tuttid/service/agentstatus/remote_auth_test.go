package agentstatus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	"github.com/tutti-os/tutti/packages/agent/daemon/providerstatus"
)

func TestResolveRemoteAuthEvidenceReadsClaudeOAuthCredential(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configDir, ".credentials.json"),
		[]byte(`{"claudeAiOauth":{"accessToken":"oauth-token","expiresAt":1}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	service := Service{
		HomeDir:    func() (string, error) { return home, nil },
		HTTPClient: server.Client(),
	}
	evidence, attempted := service.resolveRemoteAuthEvidence(context.Background(), ProviderSpec{
		Provider: "claude-code",
		RemoteAuthProbe: providerregistry.RemoteAuthProbeDescriptor{
			Kind:           providerregistry.RemoteAuthProbeKindHTTPBearer,
			CredentialKind: providerregistry.RemoteAuthCredentialKindClaudeOAuth,
			Endpoint:       server.URL, Method: http.MethodGet, TimeoutSeconds: 1,
		},
	})
	if !attempted || evidence.Kind != providerstatus.AuthEvidenceRemoteAuthFailure ||
		evidence.Reason != providerstatus.AuthReasonSessionExpired {
		t.Fatalf("evidence = %#v, attempted = %v", evidence, attempted)
	}
}

func TestReduceProviderAuthRemoteEvidenceOutranksLocalClaudeStatus(t *testing.T) {
	service := Service{RunOutcomes: NewRunOutcomeStore()}
	spec := ProviderSpec{Provider: "claude-code"}
	local := AuthInfo{Status: AuthAuthenticated, AuthMethod: "oauth"}

	authenticated := service.reduceProviderAuthWithRemote(
		spec,
		local,
		false,
		providerstatus.AuthEvidenceAuthorityLocal,
		providerstatus.AuthEvidence{Kind: providerstatus.AuthEvidenceRemoteSuccess},
		true,
	)
	if authenticated.Status != AuthAuthenticated {
		t.Fatalf("authenticated status = %q", authenticated.Status)
	}

	revoked := service.reduceProviderAuthWithRemote(
		spec,
		local,
		false,
		providerstatus.AuthEvidenceAuthorityLocal,
		providerstatus.AuthEvidence{
			Kind: providerstatus.AuthEvidenceRemoteAuthFailure, Reason: providerstatus.AuthReasonSessionExpired,
		},
		true,
	)
	if revoked.Status != AuthRequired {
		t.Fatalf("revoked status = %q", revoked.Status)
	}

	transient := service.reduceProviderAuthWithRemote(
		spec,
		local,
		false,
		providerstatus.AuthEvidenceAuthorityLocal,
		providerstatus.AuthEvidence{Kind: providerstatus.AuthEvidenceProbeFailure},
		true,
	)
	if transient.Status != AuthConfigured {
		t.Fatalf("transient status = %q", transient.Status)
	}
}
