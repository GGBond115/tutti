package connectormarket

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/httpx"
	market "github.com/tutti-os/tutti/packages/connector/market/daemon"
)

const connectorCatalogPath = "/v1/market/items"
const maxCatalogResponseBytes = 8 << 20

type RequestAuthorizer func(*http.Request) error

type CatalogSourceConfig struct {
	BaseURL            string
	ExpectedMarketType string
	HTTPClient         *http.Client
	AuthorizeRequest   RequestAuthorizer
}

type CatalogSource struct {
	baseURL            *url.URL
	expectedMarketType string
	httpClient         *http.Client
	authorizeRequest   RequestAuthorizer
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
	expectedMarketType := strings.ToLower(strings.TrimSpace(config.ExpectedMarketType))
	if expectedMarketType != "domestic" && expectedMarketType != "overseas" {
		return nil, errors.New("connector market type must be domestic or overseas")
	}
	client := config.HTTPClient
	if client == nil {
		client = httpx.NewClient(30 * time.Second)
	}
	return &CatalogSource{baseURL: baseURL, expectedMarketType: expectedMarketType,
		httpClient: client, authorizeRequest: config.AuthorizeRequest}, nil
}

func (source *CatalogSource) Refresh(ctx context.Context) (market.CatalogSnapshot, error) {
	endpoint := source.baseURL.ResolveReference(&url.URL{Path: connectorCatalogPath})
	query := endpoint.Query()
	query.Set("itemType", "connector")
	endpoint.RawQuery = query.Encode()
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
	var payload wireMarketResponse
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return market.CatalogSnapshot{}, fmt.Errorf("decode connector market catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return market.CatalogSnapshot{}, errors.New("decode connector market catalog: trailing JSON value")
	}
	if payload.MarketType != source.expectedMarketType {
		return market.CatalogSnapshot{}, errors.New("connector market type does not match configured market")
	}
	releases := make([]market.Release, 0, len(payload.Items))
	for _, item := range payload.Items {
		release, err := source.mapItem(item)
		if err != nil {
			return market.CatalogSnapshot{}, err
		}
		releases = append(releases, release)
	}
	digest := sha256.Sum256(payloadBytes)
	revision := hex.EncodeToString(digest[:])
	return market.CatalogSnapshot{SourceRevision: revision, Releases: releases}, nil
}

func (source *CatalogSource) mapItem(item wireMarketItem) (market.Release, error) {
	if item.ItemType != "connector" || item.ItemKey == "" || item.Version == "" || item.Artifact == nil || !safeArtifactKey(item.Artifact.Key) {
		return market.Release{}, errors.New("connector market item identity is incomplete")
	}
	manifestBytes, err := json.Marshal(item.Manifest)
	if err != nil {
		return market.Release{}, err
	}
	var connectorManifest wireConnectorMarketManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&connectorManifest); err != nil {
		return market.Release{}, fmt.Errorf("decode connector market manifest: %w", err)
	}
	if connectorManifest.SchemaVersion != "1" || connectorManifest.ItemType != "connector" ||
		connectorManifest.ItemKey != item.ItemKey || connectorManifest.Version != item.Version ||
		!containsString(connectorManifest.SupportedMarkets, source.expectedMarketType) {
		return market.Release{}, errors.New("connector manifest identity or market does not match item")
	}
	implementation, ok := connectorManifest.Payload.Implementations[source.expectedMarketType]
	if !ok {
		return market.Release{}, errors.New("connector manifest does not provide the configured market implementation")
	}
	manifestDigest := sha256.Sum256(manifestBytes)
	releaseDigest := sha256.Sum256([]byte(item.ItemKey + "\x00" + item.Version + "\x00" + item.Artifact.SHA256))
	manifest := market.Manifest{SchemaVersion: "1", DisplayName: connectorManifest.Display.Name,
		Category:    market.NormalizeConnectorCategory(market.ConnectorCategory(connectorManifest.Display.Category)),
		Description: connectorManifest.Display.Description, Permissions: connectorManifest.Payload.Permissions,
		Implementation: implementation, AuthorizationKind: connectorManifest.Payload.Authorization.Kind,
		Compatibility: connectorManifest.Payload.Compatibility}
	release := market.Release{SchemaVersion: "1", ReleaseID: item.ItemKey + "@" + item.Version,
		ConnectorKey: item.ItemKey, Version: item.Version,
		ReleaseDigest: hex.EncodeToString(releaseDigest[:]), ManifestDigest: hex.EncodeToString(manifestDigest[:]),
		Manifest: manifest, Artifact: market.Artifact{Key: item.Artifact.Key, SHA256: item.Artifact.SHA256,
			SizeBytes: int64(item.Artifact.SizeBytes), MediaType: artifactMediaType(item.Artifact.Key)},
		PublishedAt: time.UnixMilli(int64(item.PublishedAtMS)).UTC(), Status: market.ReleaseStatusAvailable}
	if err := market.ValidateReleaseShape(release); err != nil {
		return market.Release{}, err
	}
	return release, nil
}

type wireMarketResponse struct {
	MarketType string           `json:"marketType"`
	Items      []wireMarketItem `json:"items"`
}

type wireMarketItem struct {
	ItemType      string         `json:"itemType"`
	ItemKey       string         `json:"itemKey"`
	Version       string         `json:"version"`
	CommitSHA     string         `json:"commitSha"`
	Artifact      *wireArtifact  `json:"artifact"`
	Manifest      map[string]any `json:"manifest"`
	PublishedAtMS wireInt64      `json:"publishedAtMs"`
}

type wireArtifact struct {
	Key       string    `json:"key"`
	SHA256    string    `json:"sha256"`
	SizeBytes wireInt64 `json:"sizeBytes"`
}

// Kratos/protojson encodes int64 fields as JSON strings. Accepting numeric
// literals too keeps local tests and non-protobuf adapters straightforward.
type wireInt64 int64

func (value *wireInt64) UnmarshalJSON(payload []byte) error {
	text := strings.TrimSpace(string(payload))
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("decode market int64: %w", err)
	}
	*value = wireInt64(parsed)
	return nil
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
	Category    string `json:"category,omitempty"`
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

func artifactMediaType(key string) string {
	switch {
	case strings.HasSuffix(strings.ToLower(key), ".zip"):
		return "application/zip"
	case strings.HasSuffix(strings.ToLower(key), ".tar.gz"), strings.HasSuffix(strings.ToLower(key), ".tgz"):
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
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

func safeArtifactKey(key string) bool {
	cleaned := path.Clean(strings.TrimSpace(key))
	return cleaned != "." && cleaned != ".." && cleaned == key && !path.IsAbs(cleaned) && !strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, "\\")
}
