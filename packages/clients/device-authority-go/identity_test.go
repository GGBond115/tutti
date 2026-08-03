package deviceauthority

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"sync"
	"testing"
)

func TestMemoryIdentitySourceReusesIdentityPerRuntimeConcurrently(t *testing.T) {
	t.Parallel()

	source := NewMemoryIdentitySource()
	const callers = 32
	identities := make(chan SigningIdentity, callers)
	errors := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			identity, err := source.Identity(context.Background(), " runtime-1 ")
			if err != nil {
				errors <- err
				return
			}
			identities <- identity
		}()
	}
	group.Wait()
	close(identities)
	close(errors)
	for err := range errors {
		t.Errorf("Identity() error = %v", err)
	}

	var first SigningIdentity
	for identity := range identities {
		if first.Signer == nil {
			first = identity
			continue
		}
		if identity.KeyID != first.KeyID || !identity.Signer.Public().(ed25519.PublicKey).Equal(first.Signer.Public()) {
			t.Fatalf("identity changed across concurrent calls: first=%q current=%q", first.KeyID, identity.KeyID)
		}
	}
	if first.KeyID != GatewayIdentityKeyID(first.Signer.Public().(ed25519.PublicKey)) {
		t.Fatalf("key id = %q, does not match canonical derivation", first.KeyID)
	}

	other, err := source.Identity(context.Background(), "runtime-2")
	if err != nil {
		t.Fatalf("Identity(runtime-2) error = %v", err)
	}
	if other.KeyID == first.KeyID {
		t.Fatal("different runtimes received the same generated identity")
	}
}

func TestGatewayIdentityKeyIDMatchesTSHCompatibilityVector(t *testing.T) {
	t.Parallel()

	publicKey := make(ed25519.PublicKey, ed25519.PublicKeySize)
	for index := range publicKey {
		publicKey[index] = byte(index)
	}
	const want = "device-gateway-630dcd2966c43366"
	if got := GatewayIdentityKeyID(publicKey); got != want {
		t.Fatalf("GatewayIdentityKeyID() = %q, want %q", got, want)
	}
}

func TestValidateSigningIdentityRejectsInvalidSignerContract(t *testing.T) {
	t.Parallel()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	tests := []struct {
		name     string
		identity SigningIdentity
	}{
		{name: "missing key id", identity: SigningIdentity{Signer: privateKey}},
		{name: "missing signer", identity: SigningIdentity{KeyID: "key-1"}},
		{name: "wrong algorithm", identity: SigningIdentity{KeyID: "key-1", Signer: fakeSigner{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validateSigningIdentity(tt.identity); err == nil {
				t.Fatal("validateSigningIdentity() error = nil")
			}
		})
	}
}

type fakeSigner struct{}

func (fakeSigner) Public() crypto.PublicKey { return []byte("not-ed25519") }

func (fakeSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}
