package deviceauthority

import "strings"

type ensureDeviceAuthorityWireResponse struct {
	AuthorityID       string                `json:"authorityId"`
	State             string                `json:"state"`
	Relay             relayWire             `json:"relay"`
	Lease             leaseWire             `json:"lease"`
	GatewayEnrollment gatewayEnrollmentWire `json:"gatewayEnrollment"`
}

type deviceGatewayIdentityWire struct {
	AuthorityID string `json:"authorityId"`
	IdentityID  string `json:"identityId"`
	KeyID       string `json:"keyId"`
}

type enrollDeviceGatewayIdentityWireResponse struct {
	Identity deviceGatewayIdentityWire `json:"identity"`
}

type ownerTunnelTokenWire struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

type issueOwnerTunnelTokenWireResponse struct {
	AuthorityID       string               `json:"authorityId"`
	State             string               `json:"state"`
	Relay             relayWire            `json:"relay"`
	OwnerTunnelToken  ownerTunnelTokenWire `json:"ownerTunnelToken"`
	Lease             leaseWire            `json:"lease"`
	GatewayIdentityID string               `json:"gatewayIdentityId"`
}

type renewLeaseWireResponse struct {
	AuthorityID string `json:"authorityId"`
	State       string `json:"state"`
	RenewedAt   string `json:"renewedAt"`
	ExpiresAt   string `json:"expiresAt"`
}

type relayWire struct {
	HostEndpoint string `json:"hostEndpoint"`
	DialEndpoint string `json:"dialEndpoint"`
	RelayNodeID  string `json:"relayNodeId"`
}

func (r relayWire) descriptor() RelayDescriptor {
	return RelayDescriptor{
		HostEndpoint: strings.TrimSpace(r.HostEndpoint),
		DialEndpoint: strings.TrimSpace(r.DialEndpoint),
		RelayNodeID:  strings.TrimSpace(r.RelayNodeID),
	}
}

type leaseWire struct {
	TTLSeconds           int `json:"ttlSeconds"`
	RenewIntervalSeconds int `json:"renewIntervalSeconds"`
}

func (l leaseWire) policy() LeasePolicy {
	return LeasePolicy(l)
}

type gatewayEnrollmentWire struct {
	Proof     string `json:"proof"`
	ExpiresAt string `json:"expiresAt"`
}
