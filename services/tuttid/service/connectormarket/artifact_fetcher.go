package connectormarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/httpx"
	marketartifact "github.com/tutti-os/tutti/packages/connector/market/artifact"
)

const artifactGrantPath = "/v1/connector-market/artifact-grants"

type ArtifactGrantFetcherConfig struct {
	BaseURL              string
	GrantClient          *http.Client
	DownloadClient       *http.Client
	AllowedDownloadHosts []string
	AuthorizeRequest     RequestAuthorizer
	Now                  func() time.Time
}

type ArtifactGrantFetcher struct {
	baseURL              *url.URL
	grantClient          *http.Client
	downloadClient       *http.Client
	allowedDownloadHosts map[string]struct{}
	authorizeRequest     RequestAuthorizer
	now                  func() time.Time
}

var _ marketartifact.Fetcher = (*ArtifactGrantFetcher)(nil)

func NewArtifactGrantFetcher(config ArtifactGrantFetcherConfig) (*ArtifactGrantFetcher, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" || (baseURL.Scheme != "https" && (baseURL.Scheme != "http" || !isLoopbackHost(baseURL.Hostname()))) {
		return nil, errors.New("connector artifact grant base URL is invalid")
	}
	allowed := make(map[string]struct{}, len(config.AllowedDownloadHosts))
	for _, host := range config.AllowedDownloadHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" || net.ParseIP(host) != nil {
			return nil, errors.New("connector artifact download allowlist contains an invalid hostname")
		}
		allowed[host] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, errors.New("connector artifact download host allowlist is required")
	}
	if config.GrantClient == nil {
		config.GrantClient = httpx.NewClient(30 * time.Second)
	}
	if config.DownloadClient == nil {
		config.DownloadClient = newPinnedDownloadClient(allowed)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &ArtifactGrantFetcher{baseURL: baseURL,
		grantClient: config.GrantClient, downloadClient: config.DownloadClient, allowedDownloadHosts: allowed,
		authorizeRequest: config.AuthorizeRequest, now: config.Now}, nil
}

func (fetcher *ArtifactGrantFetcher) Fetch(ctx context.Context, request marketartifact.FetchRequest) (marketartifact.FetchResponse, error) {
	if strings.TrimSpace(request.OperationID) == "" || strings.TrimSpace(request.WorkspaceID) == "" {
		return marketartifact.FetchResponse{}, errors.New("connector artifact grant requires operation and workspace identity")
	}
	release := request.Release
	body, err := json.Marshal(map[string]string{
		"workspaceId":  request.WorkspaceID,
		"connectorKey": release.ConnectorKey, "releaseDigest": release.ReleaseDigest,
		"artifactSha256": release.Artifact.SHA256, "objectVersion": release.Artifact.ObjectVersion,
	})
	if err != nil {
		return marketartifact.FetchResponse{}, err
	}
	endpoint := fetcher.baseURL.ResolveReference(&url.URL{Path: artifactGrantPath})
	grantRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return marketartifact.FetchResponse{}, err
	}
	grantRequest.Header.Set("Accept", "application/json")
	grantRequest.Header.Set("Content-Type", "application/json")
	grantRequest.Header.Set("Idempotency-Key", request.OperationID)
	if fetcher.authorizeRequest != nil {
		if err := fetcher.authorizeRequest(grantRequest); err != nil {
			return marketartifact.FetchResponse{}, err
		}
	}
	response, err := fetcher.grantClient.Do(grantRequest)
	if err != nil {
		return marketartifact.FetchResponse{}, fmt.Errorf("request connector artifact grant: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return marketartifact.FetchResponse{}, fmt.Errorf("request connector artifact grant: status %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	var grant struct {
		DownloadURL    string `json:"downloadUrl"`
		Method         string `json:"method"`
		ExpiresAtMS    int64  `json:"expiresAtMs"`
		ConnectorKey   string `json:"connectorKey"`
		ReleaseDigest  string `json:"releaseDigest"`
		ArtifactSHA256 string `json:"artifactSha256"`
		SizeBytes      int64  `json:"sizeBytes"`
		MediaType      string `json:"mediaType"`
		ObjectVersion  string `json:"objectVersion"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&grant); err != nil {
		return marketartifact.FetchResponse{}, fmt.Errorf("decode connector artifact grant: %w", err)
	}
	if grant.Method != http.MethodGet || grant.ConnectorKey != release.ConnectorKey || grant.ReleaseDigest != release.ReleaseDigest ||
		grant.ArtifactSHA256 != release.Artifact.SHA256 || grant.ObjectVersion != release.Artifact.ObjectVersion ||
		grant.SizeBytes != release.Artifact.SizeBytes || !sameGrantMediaType(grant.MediaType, release.Artifact.MediaType) {
		return marketartifact.FetchResponse{}, errors.New("connector artifact grant does not match signed release")
	}
	expiresAt := time.UnixMilli(grant.ExpiresAtMS)
	now := fetcher.now().UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(5*time.Minute)) {
		return marketartifact.FetchResponse{}, errors.New("connector artifact grant expiry is invalid")
	}
	downloadURL, err := url.Parse(grant.DownloadURL)
	if err != nil || !fetcher.allowedDownloadURL(downloadURL) {
		return marketartifact.FetchResponse{}, errors.New("connector artifact grant URL is not allowed")
	}
	downloadRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL.String(), nil)
	if err != nil {
		return marketartifact.FetchResponse{}, err
	}
	downloadResponse, err := fetcher.downloadClient.Do(downloadRequest)
	if err != nil {
		return marketartifact.FetchResponse{}, fmt.Errorf("download granted connector artifact: %w", err)
	}
	if downloadResponse.StatusCode != http.StatusOK {
		downloadResponse.Body.Close()
		return marketartifact.FetchResponse{}, fmt.Errorf("download granted connector artifact: status %d", downloadResponse.StatusCode)
	}
	return marketartifact.FetchResponse{Body: downloadResponse.Body, ContentLength: downloadResponse.ContentLength,
		MediaType: downloadResponse.Header.Get("Content-Type")}, nil
}

func (fetcher *ArtifactGrantFetcher) allowedDownloadURL(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Fragment != "" || net.ParseIP(value.Hostname()) != nil {
		return false
	}
	_, ok := fetcher.allowedDownloadHosts[strings.ToLower(value.Hostname())]
	return ok
}

func newPinnedDownloadClient(allowed map[string]struct{}) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: nil}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if _, ok := allowed[strings.ToLower(host)]; !ok {
			return nil, errors.New("connector artifact redirect host is not allowed")
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, candidate := range addresses {
			if unsafeArtifactIP(candidate.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		}
		return nil, errors.New("connector artifact hostname has no safe address")
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute} // proxy-funnel-exempt: DNS-pinned allowlist transport prevents SSRF and redirect rebinding.
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Fragment != "" {
			return errors.New("connector artifact redirect is not allowed")
		}
		_, ok := allowed[strings.ToLower(request.URL.Hostname())]
		if !ok {
			return errors.New("connector artifact redirect host is not allowed")
		}
		return nil
	}
	return client
}

func unsafeArtifactIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

func sameGrantMediaType(left, right string) bool {
	left = strings.TrimSpace(strings.Split(left, ";")[0])
	right = strings.TrimSpace(strings.Split(right, ";")[0])
	return strings.EqualFold(left, right)
}
