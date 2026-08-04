package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestTrustVerifierRejectsRollbackEquivocationAndExpiredSnapshot(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	verifier, err := NewTrustVerifier(TrustVerifierConfig{
		Keys: map[string]ed25519.PublicKey{"key-1": publicKey}, Now: func() time.Time { return now },
		MaxSnapshotTTL: time.Hour, MaxClockSkew: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	releaseEnvelope := signedTestRelease(t, privateKey)
	releaseHash := sha256.Sum256(releaseEnvelope.Payload)
	releaseDigest := hex.EncodeToString(releaseHash[:])
	payload := CatalogSnapshotPayload{
		Sequence: 5,
		IssuedAt: now.Add(-time.Minute), NextUpdateAt: now.Add(10 * time.Minute), ExpiresAt: now.Add(20 * time.Minute),
		Catalog: CatalogIndex{SchemaVersion: "1", Releases: []CatalogReleaseStatus{{ConnectorKey: "github", ReleaseDigest: releaseDigest,
			Version: "1.0.0", Status: ReleaseStatusAvailable, PublishedMarkets: []string{"overseas"},
			ManifestSHA256:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ArtifactSHA256:        "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			ArtifactObjectVersion: "generation-1", EnvelopeSHA256: releaseDigest, SignatureKeyID: "key-1", Signature: "signed"}}},
	}
	envelope := signTestEnvelope(t, privateKey, catalogSignatureContext, payload)
	_, state, err := verifier.VerifyCatalog(envelope, CatalogTrustState{})
	if err != nil {
		t.Fatal(err)
	}
	if state.Sequence != 5 || !state.Fresh(now, time.Second) {
		t.Fatalf("state = %#v", state)
	}
	rollback := payload
	rollback.Sequence = 4
	if _, _, err := verifier.VerifyCatalog(signTestEnvelope(t, privateKey, catalogSignatureContext, rollback), state); err == nil {
		t.Fatal("rollback snapshot was accepted")
	}
	equivocation := payload
	equivocation.NextUpdateAt = equivocation.NextUpdateAt.Add(time.Minute)
	if _, _, err := verifier.VerifyCatalog(signTestEnvelope(t, privateKey, catalogSignatureContext, equivocation), state); err == nil {
		t.Fatal("same-sequence equivocation was accepted")
	}
	expired := payload
	expired.Sequence = 6
	expired.IssuedAt = now.Add(-time.Hour)
	expired.NextUpdateAt = now.Add(-40 * time.Minute)
	expired.ExpiresAt = now.Add(-30 * time.Minute)
	if _, _, err := verifier.VerifyCatalog(signTestEnvelope(t, privateKey, catalogSignatureContext, expired), state); err == nil {
		t.Fatal("expired snapshot was accepted")
	}
}

func TestTrustVerifierAcceptsKMSDigestSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewTrustVerifier(TrustVerifierConfig{Keys: map[string]ed25519.PublicKey{"key-1": publicKey}})
	if err != nil {
		t.Fatal(err)
	}
	envelope := signedTestRelease(t, privateKey)
	message := append([]byte(releaseSignatureContext), envelope.Payload...)
	digest := sha256.Sum256(message)
	envelope.Algorithm = signatureAlgorithmEd25519SHA256
	envelope.Signature = ed25519.Sign(privateKey, digest[:])
	if _, _, err := verifier.VerifyRelease(envelope); err != nil {
		t.Fatalf("VerifyRelease() error = %v", err)
	}
}

func TestTrustKeyringOverlapThenNewOnlyRotationPreservesEquivocationDefense(t *testing.T) {
	oldPublic, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	newVerifier := func(version uint64, keys map[string]ed25519.PublicKey) *TrustVerifier {
		t.Helper()
		verifier, verifierErr := NewTrustVerifier(TrustVerifierConfig{Keyring: &TrustKeyring{Version: version, Keys: keys},
			Now: func() time.Time { return now }, MaxSnapshotTTL: time.Hour, MaxClockSkew: time.Second})
		if verifierErr != nil {
			t.Fatal(verifierErr)
		}
		return verifier
	}
	overlap := newVerifier(2, map[string]ed25519.PublicKey{"old": oldPublic, "new": newPublic})
	oldRelease := signedTestRelease(t, oldPrivate)
	oldRelease.KeyID = "old"
	newRelease := signedTestRelease(t, newPrivate)
	newRelease.KeyID = "new"
	if _, _, err := overlap.VerifyRelease(oldRelease); err != nil {
		t.Fatalf("overlap rejected old key: %v", err)
	}
	if _, _, err := overlap.VerifyRelease(newRelease); err != nil {
		t.Fatalf("overlap rejected new key: %v", err)
	}

	newOnly := newVerifier(3, map[string]ed25519.PublicKey{"new": newPublic})
	if _, _, err := newOnly.VerifyRelease(oldRelease); err == nil {
		t.Fatal("new-only keyring accepted retired old key")
	}
	if _, _, err := newOnly.VerifyRelease(newRelease); err != nil {
		t.Fatalf("new-only keyring rejected new key: %v", err)
	}

	payload := CatalogSnapshotPayload{Sequence: 9, IssuedAt: now.Add(-time.Minute), NextUpdateAt: now.Add(10 * time.Minute),
		ExpiresAt: now.Add(20 * time.Minute), Catalog: CatalogIndex{SchemaVersion: "1", Releases: []CatalogReleaseStatus{}}}
	first := signTestEnvelopeWithKeyID(t, oldPrivate, "old", catalogSignatureContext, payload)
	_, state, err := overlap.VerifyCatalog(first, CatalogTrustState{})
	if err != nil {
		t.Fatal(err)
	}
	equivocated := payload
	equivocated.NextUpdateAt = equivocated.NextUpdateAt.Add(time.Minute)
	second := signTestEnvelopeWithKeyID(t, newPrivate, "new", catalogSignatureContext, equivocated)
	if _, _, err := overlap.VerifyCatalog(second, state); err == nil {
		t.Fatal("overlap key transition accepted same-sequence equivocation")
	}
}

func signedTestRelease(t *testing.T, privateKey ed25519.PrivateKey) SignedEnvelope {
	t.Helper()
	payload := ReleaseEnvelopePayload{SchemaVersion: "1", ItemType: "connector", ItemKey: "github", Version: "1.0.0",
		PublisherSubject: "ci", SourceRepository: "tutti/github", CommitSHA: "0123456789abcdef", WorkflowRef: "release",
		ProvenanceDigest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ArtifactKey:      "connectors/github.zip", ArtifactStorageRealm: ConnectorArtifactStorageRealmV1, ArtifactObjectVersion: "generation-1",
		ArtifactSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", ArtifactSizeBytes: 42,
		ArtifactMediaType: "application/zip", ManifestSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		TrustTier: "managed", Permissions: []string{"repository.read"}}
	return signTestEnvelope(t, privateKey, releaseSignatureContext, payload)
}

func signTestEnvelope(t *testing.T, privateKey ed25519.PrivateKey, context string, payload any) SignedEnvelope {
	return signTestEnvelopeWithKeyID(t, privateKey, "key-1", context, payload)
}

func signTestEnvelopeWithKeyID(t *testing.T, privateKey ed25519.PrivateKey, keyID, context string, payload any) SignedEnvelope {
	t.Helper()
	canonical, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	message := append([]byte(context), canonical...)
	return SignedEnvelope{KeyID: keyID, Algorithm: signatureAlgorithmEd25519, Payload: canonical, Signature: ed25519.Sign(privateKey, message)}
}

func testTrustedRelease() Release {
	return Release{
		SchemaVersion: "1", ReleaseID: "github@1.0.0", ConnectorKey: "github", Version: "1.0.0",
		ReleaseDigest:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Manifest: Manifest{SchemaVersion: "1", DisplayName: "GitHub", AuthorizationKind: "none",
			Implementation: Implementation{Kind: ImplementationKindManagedStdio, ManagedStdio: &ManagedStdioImplementation{
				Runtime: RuntimeRequirement{Language: "node", Profile: "connector-node-static", ABI: "node20-darwin-arm64"},
				MCP:     &ManagedMCPInterface{Entrypoint: "bin/github.js"},
			}}},
		Artifact: Artifact{StorageRealm: ConnectorArtifactStorageRealmV1, Key: "connectors/github.zip", ObjectVersion: "generation-1",
			SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", SizeBytes: 42, MediaType: "application/zip"},
		PublishedAt: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC), Status: ReleaseStatusAvailable,
		Publisher:        PublisherIdentity{Subject: "ci", SourceRepository: "tutti/github", CommitSHA: "0123456789abcdef", Workflow: "release", TrustTier: "managed"},
		ProvenanceDigest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
}
