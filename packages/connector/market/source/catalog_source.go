package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	marketclient "github.com/tutti-os/tutti/packages/clients/market-go"
	marketv1 "github.com/tutti-os/tutti/packages/clients/market-go/generated/sandbox/v1"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
)

const maxCatalogResponseBytes = marketclient.MaxResponseBodyBytes

type RequestAuthorizer func(*http.Request) error

type CatalogSourceConfig struct {
	BaseURL            string
	ExpectedMarketType string
	HTTPClient         *http.Client
	AuthorizeRequest   RequestAuthorizer
	// ExecutionTarget selects a Connector v3 target. Empty defaults to the
	// host process GOOS/GOARCH, which is the correct target for desktop hosts.
	ExecutionTarget string
}

type CatalogSource struct {
	expectedMarketType string
	marketClient       marketv1.MarketServiceHTTPClient
	executionTarget    string
}

var _ application.CatalogSource = (*CatalogSource)(nil)
var _ application.ArtifactDownloadResolver = (*CatalogSource)(nil)

func NewCatalogSource(config CatalogSourceConfig) (*CatalogSource, error) {
	expectedMarketType := strings.ToLower(strings.TrimSpace(config.ExpectedMarketType))
	if expectedMarketType != "domestic" && expectedMarketType != "overseas" {
		return nil, errors.New("connector market type must be domestic or overseas")
	}
	client, err := marketclient.New(marketclient.Config{
		BaseURL:        config.BaseURL,
		HTTPClient:     config.HTTPClient,
		PrepareRequest: marketclient.PrepareRequestFunc(config.AuthorizeRequest),
	})
	if err != nil {
		return nil, fmt.Errorf("configure connector market client: %w", err)
	}
	executionTarget := strings.TrimSpace(config.ExecutionTarget)
	var executionTargetErr error
	if executionTarget == "" {
		executionTarget, executionTargetErr = contracts.ExecutionTarget(runtime.GOOS, runtime.GOARCH)
	} else {
		executionTarget, executionTargetErr = contracts.NormalizeExecutionTarget(executionTarget)
	}
	if executionTargetErr != nil {
		return nil, executionTargetErr
	}
	return &CatalogSource{expectedMarketType: expectedMarketType, marketClient: client, executionTarget: executionTarget}, nil
}

func (source *CatalogSource) FetchSnapshot(ctx context.Context) (contracts.CatalogSnapshot, error) {
	// The current Market server protocol does not expose one authoritative
	// snapshot revision. For protocol compatibility, this adapter therefore
	// performs two complete structural reads and accepts only equal validated
	// results. This is a consistency fence, not a client-computed source or
	// release digest; server-owned identity remains authoritative.
	first, err := source.fetchSnapshot(ctx)
	if err != nil {
		return contracts.CatalogSnapshot{}, err
	}
	second, err := source.fetchSnapshot(ctx)
	if err != nil {
		return contracts.CatalogSnapshot{}, err
	}
	if !reflect.DeepEqual(first, second) {
		return contracts.CatalogSnapshot{}, contracts.NewDomainError(contracts.ErrorCodeUpstreamUnavailable,
			"connector market changed while a complete snapshot was being read", true, nil)
	}
	return second, nil
}

func (source *CatalogSource) fetchSnapshot(ctx context.Context) (contracts.CatalogSnapshot, error) {
	categories, err := source.fetchCategories(ctx)
	if err != nil {
		return contracts.CatalogSnapshot{}, err
	}
	primaryPlacements := make(map[string]struct{})
	primarySections := 0
	entries := make([]contracts.CatalogEntry, 0)
	for _, category := range categories {
		if category.Kind == "category" {
			primarySections++
		}
		sectionEntries, err := source.fetchSection(ctx, category.CategoryID)
		if err != nil {
			return contracts.CatalogSnapshot{}, err
		}
		if int64(len(sectionEntries)) != category.ItemCount {
			return contracts.CatalogSnapshot{}, errors.New("connector market category item count does not match the complete section")
		}
		for index := range sectionEntries {
			entry := &sectionEntries[index]
			entry.SectionID = category.CategoryID
			entry.Order = index
			if category.Kind == "category" {
				if _, exists := primaryPlacements[entry.Release.ConnectorKey]; exists {
					return contracts.CatalogSnapshot{}, errors.New("connector market catalog contains duplicate primary placements")
				}
				primaryPlacements[entry.Release.ConnectorKey] = struct{}{}
			}
		}
		entries = append(entries, sectionEntries...)
	}
	if primarySections == 0 {
		return contracts.CatalogSnapshot{}, errors.New("connector market catalog returned no primary categories")
	}
	return contracts.CatalogSnapshot{Categories: categories, Entries: entries}, nil
}

func (source *CatalogSource) fetchCategories(ctx context.Context) ([]contracts.CatalogCategory, error) {
	payload, err := source.marketClient.ListMarketCategories(ctx, &marketv1.ListMarketCategoriesRequest{ItemType: "connector"})
	if err != nil {
		return nil, fmt.Errorf("request connector market catalog: %w", err)
	}
	if payload.GetMarketType() != source.expectedMarketType {
		return nil, errors.New("connector market type does not match configured market")
	}
	categories := make([]contracts.CatalogCategory, 0, len(payload.GetCategories()))
	seen := make(map[string]struct{}, len(payload.GetCategories()))
	for _, category := range payload.GetCategories() {
		if category == nil || strings.TrimSpace(category.GetCategoryId()) == "" ||
			(category.GetKind() != "category" && category.GetKind() != "featured") || category.GetItemCount() < 0 ||
			!categoryHasDisplayName(category) {
			return nil, errors.New("connector market category is invalid")
		}
		if _, exists := seen[category.GetCategoryId()]; exists {
			return nil, errors.New("connector market category is duplicated")
		}
		seen[category.GetCategoryId()] = struct{}{}
		categories = append(categories, contracts.CatalogCategory{
			CategoryID: category.GetCategoryId(), Kind: category.GetKind(), SortOrder: category.GetSortOrder(), ItemCount: category.GetItemCount(),
			DisplayNameZH: category.GetDisplayNameZh(), DisplayNameEN: category.GetDisplayNameEn(),
		})
	}
	sort.Slice(categories, func(left, right int) bool {
		if categories[left].SortOrder == categories[right].SortOrder {
			return categories[left].CategoryID < categories[right].CategoryID
		}
		return categories[left].SortOrder < categories[right].SortOrder
	})
	return categories, nil
}

func (source *CatalogSource) fetchSection(ctx context.Context, sectionID string) ([]contracts.CatalogEntry, error) {
	entries := make([]contracts.CatalogEntry, 0)
	seenConnectors := make(map[string]struct{})
	pageToken := ""
	seenPageTokens := make(map[string]struct{})
	for {
		payload, err := source.marketClient.ListMarketItems(ctx, &marketv1.ListMarketItemsRequest{
			ItemType: "connector", SectionId: sectionID, PageSize: 100, PageToken: pageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("request connector market catalog: %w", err)
		}
		if payload.GetMarketType() != source.expectedMarketType {
			return nil, errors.New("connector market type does not match configured market")
		}
		for _, item := range payload.GetItems() {
			release, err := source.mapItem(item)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(item.GetCategoryId()) == "" {
				return nil, errors.New("connector market item category is missing")
			}
			if _, exists := seenConnectors[release.ConnectorKey]; exists {
				return nil, errors.New("connector market section contains duplicate placements")
			}
			seenConnectors[release.ConnectorKey] = struct{}{}
			entries = append(entries, contracts.CatalogEntry{SectionID: sectionID, CategoryID: item.GetCategoryId(), Featured: item.GetFeatured(), Release: release})
		}
		nextPageToken := strings.TrimSpace(payload.GetNextPageToken())
		if nextPageToken == "" {
			return entries, nil
		}
		if _, exists := seenPageTokens[nextPageToken]; exists {
			return nil, errors.New("connector market catalog returned a cyclic page token")
		}
		seenPageTokens[nextPageToken] = struct{}{}
		pageToken = nextPageToken
	}
}

func (source *CatalogSource) ResolveArtifactDownload(ctx context.Context, releaseDigest string) (contracts.ArtifactDownload, error) {
	releaseDigest = strings.TrimSpace(releaseDigest)
	if !isSHA256Hex(releaseDigest) {
		return contracts.ArtifactDownload{}, errors.New("connector artifact release digest is invalid")
	}
	payload, err := source.marketClient.ResolveMarketArtifactDownload(ctx, &marketv1.ResolveMarketArtifactDownloadRequest{ReleaseDigest: releaseDigest})
	if err != nil {
		return contracts.ArtifactDownload{}, fmt.Errorf("resolve connector artifact download: %w", err)
	}
	if payload == nil {
		return contracts.ArtifactDownload{}, errors.New("resolve connector artifact download: response is missing")
	}
	return contracts.ArtifactDownload{
		URL:           payload.GetUrl(),
		ExpiresAt:     time.UnixMilli(payload.GetExpiresAtMs()).UTC(),
		ReleaseDigest: payload.GetReleaseDigest(),
		SHA256:        payload.GetSha256(),
		SizeBytes:     payload.GetSizeBytes(),
		MediaType:     payload.GetMediaType(),
	}, nil
}

func (source *CatalogSource) mapItem(item *marketv1.PublicMarketItem) (contracts.Release, error) {
	if item == nil || item.GetItemType() != "connector" || item.GetItemKey() == "" || item.GetVersion() == "" || item.GetArtifact() == nil ||
		!isSHA256Hex(item.GetArtifact().GetReleaseDigest()) || !supportedArtifactMediaType(item.GetArtifact().GetMediaType()) || item.GetManifest() == nil {
		return contracts.Release{}, errors.New("connector market item identity is incomplete")
	}
	manifestBytes, err := json.Marshal(item.GetManifest().AsMap())
	if err != nil {
		return contracts.Release{}, err
	}
	var connectorManifest wireConnectorMarketManifest
	// Connector market manifests are extensible. Unknown fields cannot alter
	// the semantics of known fields; breaking changes require a new major.
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	if err := decoder.Decode(&connectorManifest); err != nil {
		return contracts.Release{}, fmt.Errorf("decode connector market manifest: %w", err)
	}
	if connectorManifest.ItemType != "connector" || connectorManifest.ItemKey != item.GetItemKey() || connectorManifest.Version != item.GetVersion() {
		return contracts.Release{}, errors.New("connector manifest identity does not match item")
	}
	if !isSHA256Hex(connectorManifest.Payload.PackageManifestSHA256) {
		return contracts.Release{}, errors.New("connector manifest package digest is invalid")
	}
	implementation, err := source.resolveManifestImplementation(connectorManifest)
	if err != nil {
		return contracts.Release{}, err
	}
	authorizationInteraction, err := connectorManifest.Payload.Authorization.interaction()
	if err != nil {
		return contracts.Release{}, err
	}
	iconURL := connectorManifest.Display.IconURL
	if strings.TrimSpace(iconURL) == "" {
		iconURL = legacyConnectorIconURL
	}
	// The server's v2 envelope is the generic, market-neutral publication
	// contract. V3 selects one target first. Both project into the stable host
	// manifest contract; these schema versions describe different boundaries.
	manifest := contracts.Manifest{SchemaVersion: "1", DisplayName: connectorManifest.Display.Name, IconURL: iconURL,
		Description: connectorManifest.Display.Description, AgentRouting: connectorManifest.Payload.AgentRouting,
		Permissions:          connectorManifest.Payload.Permissions,
		RequiredCapabilities: connectorManifest.Payload.RequiredCapabilities,
		Implementation:       implementation, AuthorizationKind: connectorManifest.Payload.Authorization.Kind,
		AuthorizationInteraction: authorizationInteraction,
		Compatibility:            connectorManifest.Payload.Compatibility}
	release := contracts.Release{SchemaVersion: "1", ReleaseID: item.GetItemKey() + "@" + item.GetVersion(),
		ConnectorKey: item.GetItemKey(), Version: item.GetVersion(),
		ReleaseDigest: item.GetArtifact().GetReleaseDigest(), ManifestDigest: connectorManifest.Payload.PackageManifestSHA256,
		Manifest: manifest, Artifact: contracts.Artifact{SHA256: item.GetArtifact().GetSha256(),
			SizeBytes: item.GetArtifact().GetSizeBytes(), MediaType: item.GetArtifact().GetMediaType()},
		PublishedAt: time.UnixMilli(item.GetPublishedAtMs()).UTC(), Status: contracts.ReleaseStatusAvailable}
	if err := contracts.ValidateReleaseShape(release); err != nil {
		return contracts.Release{}, err
	}
	return release, nil
}

func (source *CatalogSource) resolveManifestImplementation(manifest wireConnectorMarketManifest) (contracts.Implementation, error) {
	payload := manifest.Payload
	switch manifest.SchemaVersion {
	case "2":
		if payload.Implementation == nil || len(payload.TargetImplementations) != 0 {
			return contracts.Implementation{}, errors.New("connector v2 manifest must provide one market-neutral implementation")
		}
		return *payload.Implementation, nil
	case "3":
		if payload.Implementation != nil || len(payload.TargetImplementations) == 0 {
			return contracts.Implementation{}, errors.New("connector v3 manifest must provide targetImplementations")
		}
		return contracts.ResolveTargetImplementation(source.executionTarget, payload.TargetImplementations)
	default:
		return contracts.Implementation{}, fmt.Errorf("connector manifest schemaVersion %q is unsupported", manifest.SchemaVersion)
	}
}

type wireConnectorMarketManifest struct {
	SchemaVersion string                       `json:"schemaVersion"`
	ItemType      string                       `json:"itemType"`
	ItemKey       string                       `json:"itemKey"`
	Version       string                       `json:"version"`
	Display       wireConnectorDisplay         `json:"display"`
	Payload       wireConnectorManifestPayload `json:"payload"`
}

type wireConnectorDisplay struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IconURL     string `json:"iconUrl"`
}

type wireConnectorManifestPayload struct {
	Permissions           []string                            `json:"permissions"`
	RequiredCapabilities  []string                            `json:"requiredCapabilities"`
	AgentRouting          *contracts.AgentRouting             `json:"agentRouting,omitempty"`
	PackageManifestSHA256 string                              `json:"packageManifestSha256"`
	Authorization         wireConnectorAuthorization          `json:"authorization"`
	Compatibility         contracts.CompatibilityRequirements `json:"compatibility"`
	Implementation        *contracts.Implementation           `json:"implementation,omitempty"`
	TargetImplementations map[string]contracts.Implementation `json:"targetImplementations,omitempty"`
}

type wireConnectorAuthorization struct {
	Kind    string                             `json:"kind"`
	Methods []wireConnectorAuthorizationMethod `json:"methods,omitempty"`
}

type wireConnectorAuthorizationMethod struct {
	Interaction json.RawMessage `json:"interaction,omitempty"`
}

func (authorization wireConnectorAuthorization) interaction() (json.RawMessage, error) {
	var selected json.RawMessage
	for _, method := range authorization.Methods {
		if len(method.Interaction) == 0 || string(method.Interaction) == "null" {
			continue
		}
		if len(selected) != 0 {
			return nil, errors.New("connector authorization must declare at most one interaction")
		}
		selected = append(json.RawMessage(nil), method.Interaction...)
	}
	return selected, nil
}

const legacyConnectorIconURL = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCA2NCA2NCI+PHJlY3Qgd2lkdGg9IjY0IiBoZWlnaHQ9IjY0IiByeD0iMTQiIGZpbGw9IiM2YjcyODAiLz48cGF0aCBkPSJNMTggMjBoMjh2MjRIMTh6IiBmaWxsPSJub25lIiBzdHJva2U9IndoaXRlIiBzdHJva2Utd2lkdGg9IjQiLz48L3N2Zz4="

func supportedArtifactMediaType(mediaType string) bool {
	return mediaType == "application/zip" || mediaType == "application/gzip"
}

func categoryHasDisplayName(category *marketv1.MarketCategory) bool {
	if strings.TrimSpace(category.GetDisplayNameZh()) != "" || strings.TrimSpace(category.GetDisplayNameEn()) != "" {
		return true
	}
	// Compatibility window for the released category response that preceded
	// display names. New dynamic IDs must always carry a server-owned name.
	switch category.GetCategoryId() {
	case "featured", "productivity", "development", "other":
		return true
	default:
		return false
	}
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
