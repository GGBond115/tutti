package application

import (
	"crypto/sha256"
	"encoding/hex"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"strings"
)

func authorizationViewForSession(release contracts.Release, session contracts.AuthorizationSession) *contracts.AuthorizationViewEnvelope {
	if session.State != contracts.AuthorizationStatePending || strings.TrimSpace(session.AuthorizationURL) == "" {
		return nil
	}

	view := contracts.AuthorizationView{Type: contracts.AuthorizationViewTypeExternalLink, URL: session.AuthorizationURL}
	managed := release.Manifest.Implementation.ManagedStdio
	if strings.TrimSpace(session.UserCode) != "" {
		view = contracts.AuthorizationView{
			Type:            contracts.AuthorizationViewTypeDeviceCode,
			VerificationURL: session.AuthorizationURL,
			UserCode:        session.UserCode,
		}
	} else if managed != nil && managed.CredentialBroker != nil &&
		managed.CredentialBroker.Presentation == contracts.CredentialBrokerPresentationQRCode {
		view = contracts.AuthorizationView{
			Type: contracts.AuthorizationViewTypeQRCode,
			Source: &contracts.AuthorizationQRCodeSource{
				Type:  contracts.AuthorizationQRCodeSourcePayload,
				Value: session.AuthorizationURL,
			},
		}
	}
	if !session.ExpiresAt.IsZero() {
		view.ExpiresAt = session.ExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	viewDigest := sha256.Sum256([]byte(session.SessionID + "\x00" + session.AuthorizationURL + "\x00" + session.UserCode))
	return &contracts.AuthorizationViewEnvelope{
		Protocol: contracts.AuthorizationViewProtocolV1,
		ViewID:   "authorization-" + hex.EncodeToString(viewDigest[:16]),
		View:     view,
	}
}
