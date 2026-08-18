package application

import (
	"encoding/json"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationViewForSessionProjectsDeviceCodeWithV1Protocol(t *testing.T) {
	session := contracts.AuthorizationSession{
		SessionID:        "session-device-code",
		AuthorizationURL: "https://github.com/login/device",
		UserCode:         "ABCD-EFGH",
		State:            contracts.AuthorizationStatePending,
	}

	view := authorizationViewForSession(contracts.Release{}, session)
	if view == nil || view.Protocol != contracts.AuthorizationViewProtocolV1 || view.View.Type != contracts.AuthorizationViewTypeDeviceCode {
		t.Fatalf("authorization view = %#v, want V1 device-code view", view)
	}
	if view.View.VerificationURL != session.AuthorizationURL || view.View.UserCode != session.UserCode || view.View.URL != "" {
		t.Fatalf("device-code view = %#v", view.View)
	}
}

func TestAuthorizationSessionDoesNotPersistDeviceCode(t *testing.T) {
	payload, err := json.Marshal(contracts.AuthorizationSession{UserCode: "ABCD-EFGH"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "ABCD-EFGH") {
		t.Fatalf("authorization session persisted device code: %s", payload)
	}
}

func TestAuthorizationViewForSessionProjectsQRCodeWithV1Protocol(t *testing.T) {
	release := contracts.Release{Manifest: contracts.Manifest{Implementation: contracts.Implementation{
		Kind: contracts.ImplementationKindManagedStdio,
		ManagedStdio: &contracts.ManagedStdioImplementation{CredentialBroker: &contracts.ManagedCredentialBroker{
			Presentation: contracts.CredentialBrokerPresentationQRCode,
		}},
	}}}
	session := contracts.AuthorizationSession{
		SessionID:        "session-1",
		AuthorizationURL: "https://work.weixin.qq.com/ai/qc/c?s=payload",
		ExpiresAt:        time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
		State:            contracts.AuthorizationStatePending,
	}

	view := authorizationViewForSession(release, session)
	if view == nil || view.Protocol != contracts.AuthorizationViewProtocolV1 || view.View.Type != contracts.AuthorizationViewTypeQRCode {
		t.Fatalf("authorization view = %#v, want V1 QR view", view)
	}
	if view.View.URL != "" || view.View.Source == nil ||
		view.View.Source.Type != contracts.AuthorizationQRCodeSourcePayload ||
		view.View.Source.Value != session.AuthorizationURL {
		t.Fatalf("QR source = %#v and URL = %q", view.View.Source, view.View.URL)
	}
}

func TestAuthorizationViewForSessionDoesNotEmbedLegacyPagePresentation(t *testing.T) {
	release := contracts.Release{Manifest: contracts.Manifest{Implementation: contracts.Implementation{
		Kind: contracts.ImplementationKindManagedStdio,
		ManagedStdio: &contracts.ManagedStdioImplementation{CredentialBroker: &contracts.ManagedCredentialBroker{
			Presentation: contracts.CredentialBrokerPresentationEmbeddedPage,
		}},
	}}}
	session := contracts.AuthorizationSession{
		SessionID:        "session-legacy",
		AuthorizationURL: "https://work.weixin.qq.com/ai/qc/gen?scode=legacy",
		State:            contracts.AuthorizationStatePending,
	}

	view := authorizationViewForSession(release, session)
	if view == nil || view.View.Type != contracts.AuthorizationViewTypeExternalLink ||
		view.View.URL != session.AuthorizationURL || view.View.Source != nil {
		t.Fatalf("legacy authorization view = %#v, want external link", view)
	}
}
