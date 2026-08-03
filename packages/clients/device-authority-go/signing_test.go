package deviceauthority

import (
	"strings"
	"testing"
	"time"
)

func TestGatewayOwnerSessionSigningPayloadMatchesTSHServerCanonicalForm(t *testing.T) {
	t.Parallel()

	payload, err := gatewayOwnerSessionSigningPayload(
		" deva_123 ",
		" runtime-1 ",
		" key-1 ",
		" nonce-1 ",
		" 2026-08-02T09:10:11.000000123Z ",
		600,
		[]string{" Z-TARGET ", "DEVICE-GATEWAY", "device-gateway", "a-target"},
	)
	if err != nil {
		t.Fatalf("gatewayOwnerSessionSigningPayload() error = %v", err)
	}
	want := strings.Join([]string{
		"tsh.gateway.owner-session.v1",
		"authority_id=deva_123",
		"runtime_id=runtime-1",
		"key_id=key-1",
		"nonce=nonce-1",
		"timestamp=2026-08-02T09:10:11.000000123Z",
		"ttl_seconds=600",
		"supported_targets=a-target,device-gateway,z-target",
	}, "\n")
	if payload != want {
		t.Fatalf("payload =\n%s\nwant =\n%s", payload, want)
	}
}

func TestGatewayOwnerSessionSigningPayloadRejectsMissingFieldsAndEmptyTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		authority string
		targets   []string
	}{
		{name: "missing authority", targets: []string{"device-gateway"}},
		{name: "empty target", authority: "deva_123", targets: []string{"device-gateway", " "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gatewayOwnerSessionSigningPayload(tt.authority, "runtime-1", "key-1", "nonce-1", "timestamp", 600, tt.targets)
			if err == nil {
				t.Fatal("gatewayOwnerSessionSigningPayload() error = nil")
			}
		})
	}
}

func TestOwnerTunnelTTLSecondsPreservesExistingDefaultsAndProtocolBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ttl     time.Duration
		want    int
		wantErr bool
	}{
		{name: "zero defaults", want: 600},
		{name: "negative defaults", ttl: -time.Second, want: 600},
		{name: "sub-second defaults", ttl: 500 * time.Millisecond, want: 600},
		{name: "whole seconds", ttl: 42*time.Second + 900*time.Millisecond, want: 42},
		{name: "server maximum", ttl: 10 * time.Minute, want: 600},
		{name: "server maximum truncates fractional second", ttl: 10*time.Minute + 900*time.Millisecond, want: 600},
		{name: "above server maximum", ttl: 10*time.Minute + time.Second, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ownerTunnelTTLSeconds(tt.ttl)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("ownerTunnelTTLSeconds(%s) = (%d, %v), want (%d, error=%v)", tt.ttl, got, err, tt.want, tt.wantErr)
			}
		})
	}
}
