package connectormarket

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/httpx"
	market "github.com/tutti-os/tutti/packages/connector/market/daemon"
)

const connectorCatalogPath = "/v1/connector-market/releases"
const maxCatalogResponseBytes = 8 << 20

type RequestAuthorizer func(*http.Request) error

type CatalogTrustStateReader interface {
	CatalogTrustState(context.Context) (market.CatalogTrustState, error)
}

type CatalogSourceConfig struct {
	BaseURL            string
	ExpectedMarketType string
	HTTPClient         *http.Client
	AuthorizeRequest   RequestAuthorizer
	TrustVerifier      *market.TrustVerifier
	TrustStateReader   CatalogTrustStateReader
}

type CatalogSource struct {
	baseURL            *url.URL
	expectedMarketType string
	httpClient         *http.Client
	authorizeRequest   RequestAuthorizer
	trustVerifier      *market.TrustVerifier
	trustStateReader   CatalogTrustStateReader
}

var _ market.CatalogSource = (*CatalogSource)(nil)

func NewCatalogSource(config CatalogSourceConfig) (*CatalogSource, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("connector market base URL must be an absolute URL")
	}
	if baseURL.Scheme != "https" && (baseURL.Scheme != "http" || !isLoopbackHost(baseURL.Hostname())) {
		return nil, errors.New("connector market base URL must use https (http is allowed only for loopback tests)")
	}
	if config.TrustVerifier == nil || config.TrustStateReader == nil {
		return nil, errors.New("connector market verifier and durable trust state reader are required")
	}
	expectedMarketType := strings.ToLower(strings.TrimSpace(config.ExpectedMarketType))
	if expectedMarketType != "domestic" && expectedMarketType != "overseas" {
		return nil, errors.New("connector market type must be domestic or overseas")
	}
	client := config.HTTPClient
	if client == nil {
		client = httpx.NewClient(30 * time.Second)
	}
	return &CatalogSource{baseURL: baseURL, expectedMarketType: expectedMarketType,
		httpClient: client, authorizeRequest: config.AuthorizeRequest,
		trustVerifier: config.TrustVerifier, trustStateReader: config.TrustStateReader}, nil
}

func (source *CatalogSource) Refresh(ctx context.Context) (market.CatalogSnapshot, error) {
	endpoint := source.baseURL.ResolveReference(&url.URL{Path: connectorCatalogPath})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return market.CatalogSnapshot{}, err
	}
	request.Header.Set("Accept", "application/json")
	if source.authorizeRequest != nil {
		if err := source.authorizeRequest(request); err != nil {
			return market.CatalogSnapshot{}, err
		}
	}
	response, err := source.httpClient.Do(request)
	if err != nil {
		return market.CatalogSnapshot{}, fmt.Errorf("request connector market catalog: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return market.CatalogSnapshot{}, fmt.Errorf("request connector market catalog: status %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	payloadBytes, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogResponseBytes+1))
	if err != nil {
		return market.CatalogSnapshot{}, err
	}
	if len(payloadBytes) > maxCatalogResponseBytes {
		return market.CatalogSnapshot{}, errors.New("decode connector market catalog: response exceeds size limit")
	}
	var payload wireCatalogResponse
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return market.CatalogSnapshot{}, fmt.Errorf("decode connector market catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return market.CatalogSnapshot{}, errors.New("decode connector market catalog: trailing JSON value")
	}
	if source.expectedMarketType != "" && payload.MarketType != source.expectedMarketType {
		return market.CatalogSnapshot{}, errors.New("connector market type does not match configured market")
	}
	previous, err := source.trustStateReader.CatalogTrustState(ctx)
	if err != nil {
		return market.CatalogSnapshot{}, err
	}
	snapshotEnvelope, err := payload.Snapshot.SignedSnapshot.envelope()
	if err != nil {
		return market.CatalogSnapshot{}, err
	}
	signed, trust, err := source.trustVerifier.VerifyCatalog(snapshotEnvelope, previous)
	if err != nil {
		return market.CatalogSnapshot{}, err
	}
	statusByRelease := make(map[string]market.CatalogReleaseStatus, len(signed.Catalog.Releases))
	statuses := make([]market.ReleaseCatalogStatus, 0, len(signed.Catalog.Releases))
	requiredActive := make(map[string]struct{})
	for _, entry := range signed.Catalog.Releases {
		key := entry.ConnectorKey + "\x00" + entry.ReleaseDigest
		statusByRelease[key] = entry
		statuses = append(statuses, market.ReleaseCatalogStatus{ConnectorKey: entry.ConnectorKey, ReleaseDigest: entry.ReleaseDigest, Status: entry.Status})
		if entry.Status == market.ReleaseStatusAvailable && containsString(entry.PublishedMarkets, payload.MarketType) {
			requiredActive[key] = struct{}{}
		}
	}
	active := make([]market.Release, 0, len(payload.Releases))
	seen := make(map[string]struct{}, len(payload.Releases))
	for _, wireRelease := range payload.Releases {
		key := wireRelease.ConnectorKey + "\x00" + wireRelease.ReleaseDigest
		status, ok := statusByRelease[key]
		if !ok || status.Status != market.ReleaseStatusAvailable || !containsString(status.PublishedMarkets, payload.MarketType) {
			return market.CatalogSnapshot{}, errors.New("connector release is not active in the signed catalog")
		}
		if _, duplicate := seen[key]; duplicate {
			return market.CatalogSnapshot{}, errors.New("connector catalog contains duplicate release envelope")
		}
		seen[key] = struct{}{}
		delete(requiredActive, key)
		release, err := source.mapRelease(wireRelease, status)
		if err != nil {
			return market.CatalogSnapshot{}, err
		}
		active = append(active, release)
	}
	if len(requiredActive) != 0 {
		return market.CatalogSnapshot{}, errors.New("connector market withheld an active signed release")
	}
	return market.CatalogSnapshot{SourceRevision: trust.EnvelopeDigest, Trust: trust, Releases: active, Statuses: statuses}, nil
}

func (source *CatalogSource) mapRelease(wire wireReleaseEnvelope, status market.CatalogReleaseStatus) (market.Release, error) {
	envelope, err := wire.SignedEnvelope.envelope()
	if err != nil {
		return market.Release{}, err
	}
	verified, envelopeDigest, err := source.trustVerifier.VerifyRelease(envelope)
	if err != nil {
		return market.Release{}, err
	}
	manifestHash := sha256.Sum256([]byte(wire.Manifest.CanonicalBytes))
	manifestDigest := hex.EncodeToString(manifestHash[:])
	if wire.Manifest.SHA256 != manifestDigest || verified.ManifestSHA256 != manifestDigest || status.ManifestSHA256 != manifestDigest ||
		wire.ReleaseDigest != status.ReleaseDigest || envelopeDigest != status.ReleaseDigest ||
		status.SignatureKeyID != wire.SignedEnvelope.KeyID || status.Signature != wire.SignedEnvelope.Signature {
		return market.Release{}, errors.New("connector release manifest or envelope digest mismatch")
	}
	var signedManifest wireConnectorMarketManifest
	decoder := json.NewDecoder(strings.NewReader(wire.Manifest.CanonicalBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&signedManifest); err != nil {
		return market.Release{}, fmt.Errorf("decode signed connector manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return market.Release{}, errors.New("decode signed connector manifest: trailing JSON value")
	}
	if signedManifest.SchemaVersion != "1" || signedManifest.ItemType != "connector" ||
		signedManifest.ItemKey != wire.ConnectorKey || signedManifest.Version != wire.Version ||
		!containsString(signedManifest.SupportedMarkets, source.expectedMarketType) {
		return market.Release{}, errors.New("signed connector manifest identity or market does not match projection")
	}
	implementation, ok := signedManifest.Payload.Implementations[source.expectedMarketType]
	if !ok {
		return market.Release{}, errors.New("signed connector manifest does not provide the configured market implementation")
	}
	manifest := market.Manifest{SchemaVersion: "1", DisplayName: signedManifest.Display.Name,
		Description: signedManifest.Display.Description, Permissions: signedManifest.Payload.Permissions,
		Implementation: implementation, AuthorizationKind: signedManifest.Payload.Authorization.Kind,
		Compatibility: signedManifest.Payload.Compatibility}
	if err := market.ValidateManifestShape(manifest); err != nil {
		return market.Release{}, err
	}
	if !reflect.DeepEqual(manifest.Permissions, verified.Permissions) || verified.ItemKey != wire.ConnectorKey ||
		verified.Version != wire.Version || wire.Version != status.Version || wire.Artifact == nil ||
		wire.Artifact.ObjectVersion != verified.ArtifactObjectVersion || wire.Artifact.ObjectVersion != status.ArtifactObjectVersion ||
		wire.Artifact.SHA256 != verified.ArtifactSHA256 || wire.Artifact.SHA256 != status.ArtifactSHA256 ||
		wire.Artifact.SizeBytes != verified.ArtifactSizeBytes || !sameGrantMediaType(wire.Artifact.MediaType, verified.ArtifactMediaType) {
		return market.Release{}, errors.New("connector release projection does not match signed payload")
	}
	release := market.Release{
		SchemaVersion: "1", ReleaseID: wire.ReleaseID, ConnectorKey: wire.ConnectorKey, Version: wire.Version,
		ReleaseDigest: wire.ReleaseDigest, ManifestDigest: manifestDigest, Manifest: manifest,
		Artifact: market.Artifact{StorageRealm: verified.ArtifactStorageRealm, Key: verified.ArtifactKey, ObjectVersion: verified.ArtifactObjectVersion,
			SHA256: verified.ArtifactSHA256, SizeBytes: verified.ArtifactSizeBytes, MediaType: verified.ArtifactMediaType},
		PublishedAt: time.UnixMilli(wire.PublishedAtMS).UTC(), Status: status.Status,
		Publisher: market.PublisherIdentity{Subject: verified.PublisherSubject, SourceRepository: verified.SourceRepository,
			CommitSHA: verified.CommitSHA, Workflow: verified.WorkflowRef, TrustTier: verified.TrustTier},
		ProvenanceDigest: verified.ProvenanceDigest, EnvelopeDigest: envelopeDigest,
	}
	if err := market.ValidateReleaseShape(release); err != nil {
		return market.Release{}, err
	}
	return release, nil
}

type wireCatalogResponse struct {
	MarketType string `json:"marketType"`
	Snapshot   struct {
		SignedSnapshot wireSignedDocument `json:"signedSnapshot"`
	} `json:"snapshot"`
	Releases []wireReleaseEnvelope `json:"releases"`
}

type wireSignedDocument struct {
	CanonicalBytes string `json:"canonicalBytes"`
	SHA256         string `json:"sha256"`
	KeyID          string `json:"keyId"`
	Algorithm      string `json:"algorithm"`
	Signature      string `json:"signature"`
}

func (document wireSignedDocument) envelope() (market.SignedEnvelope, error) {
	digest := sha256.Sum256([]byte(document.CanonicalBytes))
	if hex.EncodeToString(digest[:]) != document.SHA256 {
		return market.SignedEnvelope{}, errors.New("signed document digest mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(document.Signature)
	if err != nil {
		signature, err = base64.RawStdEncoding.DecodeString(document.Signature)
	}
	if err != nil {
		return market.SignedEnvelope{}, errors.New("signed document signature encoding is invalid")
	}
	return market.SignedEnvelope{KeyID: document.KeyID, Algorithm: document.Algorithm, Payload: []byte(document.CanonicalBytes), Signature: signature}, nil
}

type wireCanonicalDocument struct {
	CanonicalBytes string `json:"canonicalBytes"`
	SHA256         string `json:"sha256"`
}
type wireArtifactProjection struct {
	ObjectVersion string `json:"objectVersion"`
	SHA256        string `json:"sha256"`
	SizeBytes     int64  `json:"sizeBytes"`
	MediaType     string `json:"mediaType"`
}
type wireReleaseEnvelope struct {
	ConnectorKey   string                  `json:"connectorKey"`
	ReleaseDigest  string                  `json:"releaseDigest"`
	SignedEnvelope wireSignedDocument      `json:"signedEnvelope"`
	Version        string                  `json:"version"`
	Manifest       wireCanonicalDocument   `json:"manifest"`
	Artifact       *wireArtifactProjection `json:"artifact"`
	PublishedAtMS  int64                   `json:"publishedAtMs"`
	ReleaseID      string                  `json:"releaseId"`
}

type wireConnectorMarketManifest struct {
	SchemaVersion    string                       `json:"schemaVersion"`
	ItemType         string                       `json:"itemType"`
	ItemKey          string                       `json:"itemKey"`
	Version          string                       `json:"version"`
	Display          wireConnectorDisplay         `json:"display"`
	SupportedMarkets []string                     `json:"supportedMarkets"`
	Payload          wireConnectorManifestPayload `json:"payload"`
}

type wireConnectorDisplay struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type wireConnectorManifestPayload struct {
	Permissions     []string                         `json:"permissions"`
	Authorization   wireConnectorAuthorization       `json:"authorization"`
	Compatibility   market.CompatibilityRequirements `json:"compatibility"`
	Implementations map[string]market.Implementation `json:"implementations"`
}

type wireConnectorAuthorization struct {
	Kind string `json:"kind"`
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
