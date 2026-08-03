package deviceauthority

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

const gatewayIdentityKeyIDPrefix = "device-gateway-"

// SigningIdentity binds a stable key identifier to its signer. Signer.Public
// must return the Ed25519 public key enrolled with the Device Authority.
type SigningIdentity struct {
	KeyID  string
	Signer crypto.Signer
}

// IdentitySource resolves the gateway identity for a runtime. Implementations
// must return the same identity across enrollment retries and token requests.
// Desktop products that need restart stability should back this interface with
// their durable credential store.
type IdentitySource interface {
	Identity(ctx context.Context, runtimeID string) (SigningIdentity, error)
}

// MemoryIdentitySource creates one Ed25519 identity per runtime and retains it
// for the life of the process. It preserves TSH's existing process-local
// identity semantics; durable hosts should provide their own IdentitySource.
type MemoryIdentitySource struct {
	mu        sync.Mutex
	byRuntime map[string]SigningIdentity
}

func NewMemoryIdentitySource() *MemoryIdentitySource {
	return &MemoryIdentitySource{byRuntime: make(map[string]SigningIdentity)}
}

func (s *MemoryIdentitySource) Identity(ctx context.Context, runtimeID string) (SigningIdentity, error) {
	if s == nil {
		return SigningIdentity{}, fmt.Errorf("device gateway identity source is nil")
	}
	if err := contextError(ctx); err != nil {
		return SigningIdentity{}, err
	}
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return SigningIdentity{}, fmt.Errorf("device gateway identity requires runtime id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if identity, ok := s.byRuntime[runtimeID]; ok {
		return identity, nil
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return SigningIdentity{}, fmt.Errorf("generate device gateway identity: %w", err)
	}
	identity := SigningIdentity{
		KeyID:  GatewayIdentityKeyID(publicKey),
		Signer: privateKey,
	}
	s.byRuntime[runtimeID] = identity
	return identity, nil
}

// GatewayIdentityKeyID derives the canonical key identifier used by the
// Device Authority gateway enrollment protocol.
func GatewayIdentityKeyID(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return gatewayIdentityKeyIDPrefix + hex.EncodeToString(sum[:8])
}

func validateSigningIdentity(identity SigningIdentity) (ed25519.PublicKey, error) {
	keyID := strings.TrimSpace(identity.KeyID)
	if keyID == "" || identity.Signer == nil {
		return nil, fmt.Errorf("device gateway identity requires key id and signer")
	}
	publicKey, ok := identity.Signer.Public().(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("device gateway identity signer must use Ed25519")
	}
	return append(ed25519.PublicKey(nil), publicKey...), nil
}
