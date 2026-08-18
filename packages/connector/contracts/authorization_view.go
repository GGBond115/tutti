package contracts

const (
	AuthorizationViewProtocolV1       = "tutti.connector.authorization.view.v1"
	AuthorizationViewTypeExternalLink = "external_link"
	AuthorizationViewTypeDeviceCode   = "device_code"
	AuthorizationViewTypeQRCode       = "qr_code"
	AuthorizationQRCodeSourcePayload  = "payload"
)

// AuthorizationViewEnvelope is the host-neutral authorization presentation
// contract returned after provider output has been validated by application.
type AuthorizationViewEnvelope struct {
	Protocol string            `json:"protocol"`
	ViewID   string            `json:"viewId"`
	View     AuthorizationView `json:"view"`
}

type AuthorizationView struct {
	Type            string                     `json:"type"`
	URL             string                     `json:"url,omitempty"`
	VerificationURL string                     `json:"verificationUrl,omitempty"`
	UserCode        string                     `json:"userCode,omitempty"`
	Source          *AuthorizationQRCodeSource `json:"source,omitempty"`
	ExpiresAt       string                     `json:"expiresAt,omitempty"`
}

type AuthorizationQRCodeSource struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
