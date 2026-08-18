package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
	marketv1 "github.com/tutti-os/tutti/packages/clients/market-go/generated/sandbox/v1"
	market "github.com/tutti-os/tutti/packages/connector/host"
	"google.golang.org/protobuf/types/known/structpb"
)

type mutatingCatalogClient struct {
	t         *testing.T
	itemCalls int
}

func (client *mutatingCatalogClient) ListMarketCategories(context.Context, *marketv1.ListMarketCategoriesRequest, ...khttp.CallOption) (*marketv1.ListMarketCategoriesReply, error) {
	return &marketv1.ListMarketCategoriesReply{MarketType: "overseas", Categories: []*marketv1.MarketCategory{{
		CategoryId: "development", Kind: "category", ItemCount: 2, DisplayNameEn: "Development",
	}}}, nil
}

func (client *mutatingCatalogClient) ListMarketItems(_ context.Context, request *marketv1.ListMarketItemsRequest, _ ...khttp.CallOption) (*marketv1.ListMarketItemsReply, error) {
	client.itemCalls++
	version := "1.0.0"
	if client.itemCalls >= 3 {
		version = "2.0.0"
	}
	if request.GetPageToken() == "page-2" {
		item := validGeneratedCatalogItem(client.t, "slack", "1.0.0")
		return &marketv1.ListMarketItemsReply{MarketType: "overseas", Items: []*marketv1.PublicMarketItem{item}}, nil
	}
	item := validGeneratedCatalogItem(client.t, "github", version)
	return &marketv1.ListMarketItemsReply{MarketType: "overseas", Items: []*marketv1.PublicMarketItem{item}, NextPageToken: "page-2"}, nil
}

func (*mutatingCatalogClient) GetMarketItem(context.Context, *marketv1.GetMarketItemRequest, ...khttp.CallOption) (*marketv1.GetMarketItemReply, error) {
	return nil, errors.New("unexpected GetMarketItem")
}

func (*mutatingCatalogClient) ResolveMarketArtifactDownload(context.Context, *marketv1.ResolveMarketArtifactDownloadRequest, ...khttp.CallOption) (*marketv1.ResolveMarketArtifactDownloadReply, error) {
	return nil, errors.New("unexpected ResolveMarketArtifactDownload")
}

func TestCatalogSourceRejectsMutationBetweenPaginatedFullReads(t *testing.T) {
	client := &mutatingCatalogClient{t: t}
	source := &CatalogSource{expectedMarketType: "overseas", marketClient: client, executionTarget: "darwin-arm64"}
	_, err := source.Refresh(context.Background())
	var domainError *market.DomainError
	if !errors.As(err, &domainError) || domainError.Code != market.ErrorCodeUpstreamUnavailable || !domainError.Retryable {
		t.Fatalf("error = %#v", err)
	}
	if client.itemCalls != 4 {
		t.Fatalf("item calls = %d, want 4", client.itemCalls)
	}
}

func validGeneratedCatalogItem(t *testing.T, key, version string) *marketv1.PublicMarketItem {
	t.Helper()
	manifest := map[string]any{
		"schemaVersion": "2", "itemType": "connector", "itemKey": key, "version": version,
		"display": map[string]any{"name": key, "iconUrl": "data:image/png;base64,iVBORw0KGgo="},
		"payload": map[string]any{
			"permissions": []any{}, "packageManifestSha256": strings.Repeat("b", 64),
			"authorization": map[string]any{"kind": "none"}, "compatibility": map[string]any{},
			"implementation": map[string]any{"kind": "managed_stdio", "managedStdio": map[string]any{
				"runtime": map[string]any{"language": "node", "profile": "connector-node-static", "abi": "node20-darwin-arm64"},
				"mcp":     map[string]any{"entrypoint": "bin/connector.js"},
			}},
		},
	}
	item := generatedMarketItem(t, manifest, key, version)
	item.CategoryId = "development"
	item.Artifact.ReleaseDigest = strings.Repeat(map[bool]string{true: "e", false: "d"}[version == "2.0.0"], 64)
	return item
}

func TestCatalogSourceMapsServerDescriptorWithoutDeprecatedArtifactKey(t *testing.T) {
	itemCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer catalog-token" {
			t.Fatalf("request path=%q query=%q authorization=%q", request.URL.Path, request.URL.RawQuery, request.Header.Get("Authorization"))
		}
		if request.URL.Query().Get("itemType") != "connector" {
			t.Fatalf("request path=%q query=%q", request.URL.Path, request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/market/categories" {
			_, _ = writer.Write([]byte(`{
  "marketType": "overseas",
  "categories": [
    {"categoryId": "featured", "kind": "featured", "sortOrder": 10, "itemCount": "1", "displayNameZh": "精选", "displayNameEn": "Featured"},
    {"categoryId": "developer-tools", "kind": "category", "sortOrder": 40, "itemCount": "1", "displayNameZh": "开发者工具", "displayNameEn": "Developer Tools"}
  ]
}`))
			return
		}
		itemCalls++
		if request.URL.Path != "/v1/market/items" || request.URL.Query().Get("pageSize") != "100" {
			t.Fatalf("request path=%q query=%q", request.URL.Path, request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{
  "marketType": "overseas",
  "requestId": "request-1",
  "items": [{
    "itemType": "connector",
    "itemKey": "github",
    "version": "1.0.0",
    "commitSha": "0123456789abcdef",
    "publisher": {"name": "Tutti"},
    "artifact": {
      "sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
      "sizeBytes": "123",
      "releaseDigest": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
      "mediaType": "application/zip"
    },
    "manifest": {
      "schemaVersion": "2",
      "itemType": "connector",
      "itemKey": "github",
      "version": "1.0.0",
      "metadata": {"labels": ["source-control"]},
      "display": {"name": "GitHub", "description": "GitHub connector", "iconUrl": "data:image/png;base64,iVBORw0KGgo=", "badge": "new"},
      "payload": {
        "permissions": ["network:*"],
        "agentRouting": {"aliases": ["Git Hub", "代码托管"]},
        "packageManifestSha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "authorization": {"kind": "none"},
        "compatibility": {},
        "audit": {"reviewed": true},
        "implementation": {
          "kind": "managed_stdio",
          "extensionMetadata": {"revision": 2},
          "managedStdio": {
            "runtime": {"language": "node", "profile": "connector-node-static", "abi": "node20-darwin-arm64"},
            "mcp": {"entrypoint": "bin/github.js"},
            "observability": {"enabled": true}
          }
        }
      }
    },
    "publishedAtMs": "1785801600000",
    "categoryId": "developer-tools",
    "featured": true
  }],
  "nextPageToken": ""
}`))
	}))
	defer server.Close()

	source, err := NewCatalogSource(CatalogSourceConfig{
		BaseURL:            server.URL,
		ExpectedMarketType: "overseas",
		HTTPClient:         server.Client(),
		AuthorizeRequest: func(request *http.Request) error {
			request.Header.Set("Authorization", "Bearer catalog-token")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Categories) != 2 || len(result.Entries) != 2 || result.SourceRevision != "" {
		t.Fatalf("snapshot = %#v", result)
	}
	got := result.Entries[0].Release
	if got.ConnectorKey != "github" || got.ReleaseID != "github@1.0.0" || got.Manifest.SchemaVersion != "1" ||
		got.ReleaseDigest != strings.Repeat("d", 64) ||
		got.ManifestDigest != strings.Repeat("b", 64) || got.Artifact.SizeBytes != 123 || got.Artifact.MediaType != "application/zip" ||
		got.Manifest.Implementation.ManagedStdio == nil || len(got.Manifest.Permissions) != 1 || got.Manifest.Permissions[0] != "network:*" ||
		got.Manifest.AgentRouting == nil || len(got.Manifest.AgentRouting.Aliases) != 2 || got.Manifest.AgentRouting.Aliases[1] != "代码托管" ||
		got.Manifest.Implementation.ManagedStdio.MCP.Entrypoint != "bin/github.js" {
		t.Fatalf("release = %#v", got)
	}
	if result.Categories[1].CategoryID != "developer-tools" || result.Categories[1].DisplayNameZH != "开发者工具" ||
		result.Categories[1].DisplayNameEN != "Developer Tools" || result.Entries[1].Release.Artifact.SHA256 != strings.Repeat("c", 64) {
		t.Fatalf("snapshot = %#v", result)
	}
	if itemCalls != 4 {
		t.Fatalf("market item requests = %d, want 4", itemCalls)
	}
}

func TestCatalogSourceResolvesAuthenticatedArtifactDownloadByReleaseDigest(t *testing.T) {
	digest := strings.Repeat("d", 64)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/market/artifacts:resolve-download" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer catalog-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"url":"https://artifacts.example.test/signed.zip?token=secret","expiresAtMs":"1787018520000","releaseDigest":"` + digest + `","sha256":"` + strings.Repeat("c", 64) + `","sizeBytes":"123","mediaType":"application/zip"}`))
	}))
	defer server.Close()

	source, err := NewCatalogSource(CatalogSourceConfig{
		BaseURL: server.URL, ExpectedMarketType: "overseas", HTTPClient: server.Client(),
		AuthorizeRequest: func(request *http.Request) error {
			request.Header.Set("Authorization", "Bearer catalog-token")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := source.ResolveArtifactDownload(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ReleaseDigest != digest || resolved.SHA256 != strings.Repeat("c", 64) || resolved.SizeBytes != 123 || resolved.MediaType != "application/zip" ||
		resolved.ExpiresAt.UnixMilli() != 1787018520000 || !strings.Contains(resolved.URL, "token=secret") {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestCatalogSourcePreservesRemoteRequiredCapabilities(t *testing.T) {
	var manifest map[string]any
	if err := json.Unmarshal([]byte(`{
  "schemaVersion": "2",
  "itemType": "connector",
  "itemKey": "tencent-docs",
  "version": "0.2.0",
  "display": {
    "name": "Tencent Docs",
    "iconUrl": "data:image/png;base64,iVBORw0KGgo="
  },
  "payload": {
    "permissions": [],
    "requiredCapabilities": ["tools"],
    "packageManifestSha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "authorization": {
      "kind": "api_key",
      "methods": [{
        "interaction": {
          "protocol": "tutti.connector.authorization.declarative.v1",
          "initialView": {
            "type": "form",
            "fields": [{
              "type": "secret",
              "name": "personal_token",
              "label": "Personal token",
              "required": true
            }]
          },
          "submission": {"kind": "native_secret", "secretField": "personal_token"}
        }
      }]
    },
    "compatibility": {},
    "implementation": {
      "kind": "remote_streamable_http",
      "remoteStreamableHttp": {
        "protocolVersion": "2026-07-28",
        "bindingRef": "tencent-docs.primary",
        "contractVersion": 1,
        "bindingContractHash": "sha256:ca239a2e69a22a3e1df0d50f6ad944491e7cd813fd347591ce238ebfc884017a"
      }
    }
  }
}`), &manifest); err != nil {
		t.Fatal(err)
	}

	source := &CatalogSource{executionTarget: "darwin-arm64"}
	release, err := source.mapItem(generatedMarketItem(t, manifest, "tencent-docs", "0.2.0"))
	if err != nil {
		t.Fatal(err)
	}
	if len(release.Manifest.RequiredCapabilities) != 1 || release.Manifest.RequiredCapabilities[0] != "tools" {
		t.Fatalf("requiredCapabilities = %#v, want [tools]", release.Manifest.RequiredCapabilities)
	}
	if !strings.Contains(string(release.Manifest.AuthorizationInteraction), `"secretField":"personal_token"`) {
		t.Fatalf("authorizationInteraction = %s", release.Manifest.AuthorizationInteraction)
	}
}

func TestCatalogSourceRejectsLegacyConnectorManifestV1(t *testing.T) {
	var manifest map[string]any
	if err := json.Unmarshal([]byte(`{
  "schemaVersion": "1",
  "itemType": "connector",
  "itemKey": "github",
  "version": "1.0.0",
  "display": {"name": "GitHub", "iconUrl": "data:image/png;base64,iVBORw0KGgo="},
  "supportedMarkets": ["overseas"],
  "payload": {
    "permissions": [],
    "packageManifestSha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "authorization": {"kind": "none"},
    "compatibility": {},
    "implementations": {}
  }
}`), &manifest); err != nil {
		t.Fatal(err)
	}
	source := &CatalogSource{executionTarget: "darwin-arm64"}
	_, err := source.mapItem(generatedMarketItem(t, manifest, "github", "1.0.0"))
	if err == nil {
		t.Fatal("legacy connector manifest v1 was accepted")
	}
}

func TestCatalogSourceSelectsExactV3ExecutionTarget(t *testing.T) {
	source := &CatalogSource{expectedMarketType: "overseas", executionTarget: "linux-arm64"}
	manifest := wireConnectorMarketManifest{
		SchemaVersion: "3",
		Payload: wireConnectorManifestPayload{TargetImplementations: map[string]market.Implementation{
			"darwin-arm64": {Kind: market.ImplementationKindManagedStdio},
			"linux-arm64":  {Kind: market.ImplementationKindBuiltin},
		}},
	}
	implementation, err := source.resolveManifestImplementation(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if implementation.Kind != market.ImplementationKindBuiltin {
		t.Fatalf("implementation = %#v", implementation)
	}
	delete(manifest.Payload.TargetImplementations, "linux-arm64")
	if _, err := source.resolveManifestImplementation(manifest); err == nil || !strings.Contains(err.Error(), "linux-arm64") {
		t.Fatalf("resolveManifestImplementation() error = %v, want missing exact target", err)
	}
}

func TestCatalogSourceKeepsV2MarketNeutralImplementation(t *testing.T) {
	implementation := market.Implementation{Kind: market.ImplementationKindManagedStdio}
	source := &CatalogSource{expectedMarketType: "domestic", executionTarget: "darwin-arm64"}
	got, err := source.resolveManifestImplementation(wireConnectorMarketManifest{
		SchemaVersion: "2",
		Payload:       wireConnectorManifestPayload{Implementation: &implementation},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != implementation.Kind {
		t.Fatalf("implementation = %#v", got)
	}
}

func TestCatalogSourceRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewCatalogSource(CatalogSourceConfig{BaseURL: "/market", ExpectedMarketType: "overseas"}); err == nil {
		t.Fatal("expected invalid URL")
	}
	if _, err := NewCatalogSource(CatalogSourceConfig{BaseURL: "https://example.test", ExpectedMarketType: "invalid"}); err == nil {
		t.Fatal("expected invalid market type")
	}
	if _, err := NewCatalogSource(CatalogSourceConfig{BaseURL: "https://example.test", ExpectedMarketType: "overseas"}); err == nil || !strings.Contains(err.Error(), "HTTP client") {
		t.Fatalf("expected missing HTTP client error, got %v", err)
	}
	if _, err := NewCatalogSource(CatalogSourceConfig{BaseURL: "https://example.test", ExpectedMarketType: "overseas", HTTPClient: http.DefaultClient, ExecutionTarget: "linux-aarch64"}); err == nil || !strings.Contains(err.Error(), "execution target") {
		t.Fatalf("expected invalid execution target error, got %v", err)
	}
}

func TestCatalogSourcePreservesGatewayBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/desktop/v1/market/categories" {
			t.Fatalf("request path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"marketType":"overseas","categories":[]}`))
	}))
	defer server.Close()

	source, err := NewCatalogSource(CatalogSourceConfig{
		BaseURL:            server.URL + "/api/desktop",
		ExpectedMarketType: "overseas",
		HTTPClient:         server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.fetchCategories(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogSourceRequiresServerNamesForDynamicCategories(t *testing.T) {
	tests := []struct {
		name      string
		category  string
		wantError bool
	}{
		{name: "released legacy response", category: `{"categoryId":"development","kind":"category","sortOrder":20,"itemCount":"1"}`},
		{name: "unnamed dynamic category", category: `{"categoryId":"future-category","kind":"category","sortOrder":80,"itemCount":"1"}`, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"marketType":"overseas","categories":[` + test.category + `]}`))
			}))
			defer server.Close()
			source, err := NewCatalogSource(CatalogSourceConfig{
				BaseURL: server.URL, ExpectedMarketType: "overseas", HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.fetchCategories(context.Background())
			if test.wantError && err == nil {
				t.Fatal("expected unnamed dynamic category error")
			}
			if !test.wantError && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCatalogSourceRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat(" ", maxCatalogResponseBytes+1)))
	}))
	defer server.Close()
	source, err := NewCatalogSource(CatalogSourceConfig{
		BaseURL:            server.URL,
		ExpectedMarketType: "overseas",
		HTTPClient:         server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v", err)
	}
}

func generatedMarketItem(t *testing.T, manifest map[string]any, itemKey, version string) *marketv1.PublicMarketItem {
	t.Helper()
	manifestValue, err := structpb.NewStruct(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return &marketv1.PublicMarketItem{
		ItemType: "connector", ItemKey: itemKey, Version: version, Manifest: manifestValue,
		Artifact: &marketv1.MarketArtifactDescriptor{
			Sha256: strings.Repeat("c", 64), SizeBytes: 123, ReleaseDigest: strings.Repeat("d", 64), MediaType: "application/gzip",
		},
		PublishedAtMs: 1785801600000,
	}
}
