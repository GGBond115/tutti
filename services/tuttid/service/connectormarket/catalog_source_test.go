package connectormarket

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	market "github.com/tutti-os/tutti/packages/connector/market/daemon"
)

func TestCatalogSourceVerifiesAndMapsSignedRelease(t *testing.T) {
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	response := signedCatalogResponse(t, privateKey, now)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != connectorCatalogPath || request.Header.Get("Authorization") != "Bearer catalog-token" {
			t.Fatalf("request path=%q authorization=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		_, _ = writer.Write(response)
	}))
	defer server.Close()
	verifier, err := market.NewTrustVerifier(market.TrustVerifierConfig{Keys: map[string]ed25519.PublicKey{"key-1": publicKey}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewCatalogSource(CatalogSourceConfig{BaseURL: server.URL, ExpectedMarketType: "overseas",
		TrustVerifier: verifier, TrustStateReader: catalogTrustReader{},
		AuthorizeRequest: func(request *http.Request) error {
			request.Header.Set("Authorization", "Bearer catalog-token")
			return nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Trust.Sequence != 1 || len(result.Releases) != 1 || len(result.Statuses) != 1 {
		t.Fatalf("snapshot = %#v", result)
	}
	got := result.Releases[0]
	if got.ConnectorKey != "github" || got.Artifact.SizeBytes != 123 || got.Manifest.Implementation.ManagedStdio == nil {
		t.Fatalf("release = %#v", got)
	}
}

func TestCatalogSourceRejectsNonAbsoluteBaseURLAndMissingTrust(t *testing.T) {
	if _, err := NewCatalogSource(CatalogSourceConfig{BaseURL: "/market"}); err == nil {
		t.Fatal("expected invalid URL")
	}
	if _, err := NewCatalogSource(CatalogSourceConfig{BaseURL: "https://example.test"}); err == nil {
		t.Fatal("expected missing verifier")
	}
}

func TestCatalogSourceRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat(" ", maxCatalogResponseBytes+1)))
	}))
	defer server.Close()
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	verifier, _ := market.NewTrustVerifier(market.TrustVerifierConfig{Keys: map[string]ed25519.PublicKey{"key-1": publicKey}})
	source, err := NewCatalogSource(CatalogSourceConfig{BaseURL: server.URL, ExpectedMarketType: "overseas", TrustVerifier: verifier, TrustStateReader: catalogTrustReader{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v", err)
	}
}

type catalogTrustReader struct{ state market.CatalogTrustState }

func (reader catalogTrustReader) CatalogTrustState(context.Context) (market.CatalogTrustState, error) {
	return reader.state, nil
}

func signedCatalogResponse(t *testing.T, privateKey ed25519.PrivateKey, now time.Time) []byte {
	t.Helper()
	release := catalogTestRelease()
	manifestBytes, err := json.Marshal(wireConnectorMarketManifest{
		SchemaVersion: "1", ItemType: "connector", ItemKey: release.ConnectorKey, Version: release.Version,
		Display:          wireConnectorDisplay{Name: release.Manifest.DisplayName, Description: release.Manifest.Description},
		SupportedMarkets: []string{"overseas"},
		Payload: wireConnectorManifestPayload{Permissions: release.Manifest.Permissions,
			Authorization:   wireConnectorAuthorization{Kind: release.Manifest.AuthorizationKind},
			Compatibility:   release.Manifest.Compatibility,
			Implementations: map[string]market.Implementation{"overseas": release.Manifest.Implementation}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	manifestDigest := hex.EncodeToString(manifestHash[:])
	releasePayload := market.ReleaseEnvelopePayload{SchemaVersion: "1", ItemType: "connector", ItemKey: release.ConnectorKey,
		Version: release.Version, PublisherSubject: release.Publisher.Subject, SourceRepository: release.Publisher.SourceRepository,
		CommitSHA: release.Publisher.CommitSHA, WorkflowRef: release.Publisher.Workflow, ProvenanceDigest: release.ProvenanceDigest,
		ArtifactKey: release.Artifact.Key, ArtifactStorageRealm: release.Artifact.StorageRealm,
		ArtifactObjectVersion: release.Artifact.ObjectVersion, ArtifactSHA256: release.Artifact.SHA256,
		ArtifactSizeBytes: release.Artifact.SizeBytes, ArtifactMediaType: release.Artifact.MediaType, ManifestSHA256: manifestDigest,
		TrustTier: release.Publisher.TrustTier, Permissions: release.Manifest.Permissions}
	releaseEnvelope := signCatalogTestEnvelope(t, privateKey, "tutti.connector.release.v1\x00", releasePayload)
	releaseHash := sha256.Sum256(releaseEnvelope.Payload)
	releaseDigest := hex.EncodeToString(releaseHash[:])
	snapshotPayload := market.CatalogSnapshotPayload{Sequence: 1, IssuedAt: now.Add(-time.Minute), NextUpdateAt: now.Add(5 * time.Minute), ExpiresAt: now.Add(10 * time.Minute),
		Catalog: market.CatalogIndex{SchemaVersion: "1", Releases: []market.CatalogReleaseStatus{{ConnectorKey: release.ConnectorKey,
			ReleaseDigest: releaseDigest, Version: release.Version, Status: market.ReleaseStatusAvailable, PublishedMarkets: []string{"overseas"},
			ManifestSHA256: manifestDigest, ArtifactSHA256: release.Artifact.SHA256, ArtifactObjectVersion: release.Artifact.ObjectVersion,
			EnvelopeSHA256: releaseDigest, SignatureKeyID: "key-1", Signature: base64.StdEncoding.EncodeToString(releaseEnvelope.Signature)}}}}
	snapshotEnvelope := signCatalogTestEnvelope(t, privateKey, "tutti.connector.catalog.v1\x00", snapshotPayload)
	wire := wireCatalogResponse{MarketType: "overseas", Releases: []wireReleaseEnvelope{{ConnectorKey: release.ConnectorKey,
		ReleaseDigest: releaseDigest, SignedEnvelope: wireDocument(releaseEnvelope), Version: release.Version,
		Manifest: wireCanonicalDocument{CanonicalBytes: string(manifestBytes), SHA256: manifestDigest},
		Artifact: &wireArtifactProjection{ObjectVersion: release.Artifact.ObjectVersion, SHA256: release.Artifact.SHA256,
			SizeBytes: release.Artifact.SizeBytes, MediaType: release.Artifact.MediaType}, PublishedAtMS: release.PublishedAt.UnixMilli(), ReleaseID: release.ReleaseID}}}
	wire.Snapshot.SignedSnapshot = wireDocument(snapshotEnvelope)
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func wireDocument(envelope market.SignedEnvelope) wireSignedDocument {
	digest := sha256.Sum256(envelope.Payload)
	return wireSignedDocument{CanonicalBytes: string(envelope.Payload), SHA256: hex.EncodeToString(digest[:]), KeyID: envelope.KeyID,
		Algorithm: envelope.Algorithm, Signature: base64.StdEncoding.EncodeToString(envelope.Signature)}
}

func signCatalogTestEnvelope(t *testing.T, privateKey ed25519.PrivateKey, domain string, payload any) market.SignedEnvelope {
	t.Helper()
	canonical, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return market.SignedEnvelope{KeyID: "key-1", Algorithm: "Ed25519", Payload: canonical,
		Signature: ed25519.Sign(privateKey, append([]byte(domain), canonical...))}
}

func catalogTestRelease() market.Release {
	return market.Release{SchemaVersion: "1", ReleaseID: "42", ConnectorKey: "github", Version: "1.0.0",
		ReleaseDigest:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Manifest: market.Manifest{SchemaVersion: "1", DisplayName: "GitHub", Permissions: []string{"repository.read"}, AuthorizationKind: "none",
			Implementation: market.Implementation{Kind: market.ImplementationKindManagedStdio, ManagedStdio: &market.ManagedStdioImplementation{
				Runtime: market.RuntimeRequirement{Language: "node", Profile: "connector-node-static", ABI: "node20-darwin-arm64"}, MCP: &market.ManagedMCPInterface{Entrypoint: "bin/github.js"}}}},
		Artifact: market.Artifact{StorageRealm: market.ConnectorArtifactStorageRealmV1, Key: "connectors/github/1.0.0.zip", ObjectVersion: "generation-1",
			SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", SizeBytes: 123, MediaType: "application/vnd.tutti.connector+zip"},
		PublishedAt: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC), Status: market.ReleaseStatusAvailable,
		Publisher:        market.PublisherIdentity{Subject: "ci", SourceRepository: "tutti/github", CommitSHA: "0123456789abcdef", Workflow: "release", TrustTier: "managed"},
		ProvenanceDigest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}
}
