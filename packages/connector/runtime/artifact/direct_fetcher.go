package artifact

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/connector/application"
	"github.com/tutti-os/tutti/packages/connector/contracts"
)

const (
	maxResolvedArtifactURLBytes = 8 << 10
	// tsh-server issues five-minute grants. Thirty seconds permits bounded
	// clock skew without accepting long-lived signed URLs.
	maxResolvedArtifactExpiry = 5*time.Minute + 30*time.Second
)

var errArtifactRedirectOrigin = errors.New("connector artifact redirect leaves the resolved origin")

type DirectFetcherConfig struct {
	Resolver   application.ArtifactDownloadResolver
	HTTPClient *http.Client
	Now        func() time.Time
}

// DirectFetcher resolves a short-lived signed URL immediately before every
// download. The deprecated catalog object key is never used for installation.
// DownloadCache independently enforces the declared digest and size before
// bytes can become an install candidate.
type DirectFetcher struct {
	resolver   application.ArtifactDownloadResolver
	httpClient *http.Client
	now        func() time.Time
}

var _ Fetcher = (*DirectFetcher)(nil)

func NewDirectFetcher(config DirectFetcherConfig) (*DirectFetcher, error) {
	if config.Resolver == nil {
		return nil, errors.New("connector artifact download resolver is required")
	}
	if config.HTTPClient == nil {
		return nil, errors.New("connector artifact HTTP client is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &DirectFetcher{resolver: config.Resolver, httpClient: config.HTTPClient, now: now}, nil
}

func (fetcher *DirectFetcher) Fetch(ctx context.Context, request FetchRequest) (FetchResponse, error) {
	resolved, err := fetcher.resolver.ResolveArtifactDownload(ctx, request.Release.ReleaseDigest)
	if err != nil {
		return FetchResponse{}, fmt.Errorf("resolve connector artifact: %w", err)
	}
	endpoint, err := validateResolvedArtifact(resolved, request.Release, fetcher.now())
	if err != nil {
		return FetchResponse{}, err
	}
	downloadRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return FetchResponse{}, errors.New("connector artifact resolved URL is invalid")
	}
	client := *fetcher.httpClient
	hostRedirectPolicy := fetcher.httpClient.CheckRedirect
	client.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !sameOrigin(redirect.URL, endpoint) {
			return errArtifactRedirectOrigin
		}
		if hostRedirectPolicy != nil {
			return hostRedirectPolicy(redirect, via)
		}
		return nil
	}
	downloadResponse, err := client.Do(downloadRequest)
	if err != nil {
		return FetchResponse{}, artifactDownloadTransportError{cause: err}
	}
	if downloadResponse.StatusCode != http.StatusOK {
		_ = downloadResponse.Body.Close()
		return FetchResponse{}, contracts.NewDomainError(
			contracts.ErrorCodeInstallFailed,
			"download connector artifact was rejected",
			artifactDownloadStatusRetryable(downloadResponse.StatusCode),
			nil,
		)
	}
	return FetchResponse{
		Body:          downloadResponse.Body,
		ContentLength: downloadResponse.ContentLength,
		MediaType:     downloadResponse.Header.Get("Content-Type"),
	}, nil
}

func artifactDownloadStatusRetryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooEarly ||
		status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

type artifactDownloadTransportError struct {
	cause error
}

func (artifactDownloadTransportError) Error() string {
	return "download connector artifact: transport failed"
}

func (err artifactDownloadTransportError) Unwrap() error {
	return err.cause
}

func validateResolvedArtifact(resolved contracts.ArtifactDownload, release contracts.Release, now time.Time) (*url.URL, error) {
	rawURL := resolved.URL
	if len(rawURL) == 0 || len(rawURL) > maxResolvedArtifactURLBytes || strings.TrimSpace(rawURL) != rawURL {
		return nil, errors.New("connector artifact resolved URL is invalid")
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, errors.New("connector artifact resolved URL is invalid")
	}
	if resolved.ExpiresAt.IsZero() || !resolved.ExpiresAt.After(now) {
		return nil, errors.New("connector artifact resolved URL is expired")
	}
	if resolved.ExpiresAt.After(now.Add(maxResolvedArtifactExpiry)) {
		return nil, errors.New("connector artifact resolved URL expiry exceeds limit")
	}
	if !isLowerSHA256(resolved.ReleaseDigest) || !isLowerSHA256(resolved.SHA256) {
		return nil, errors.New("connector artifact resolved identity is invalid")
	}
	if resolved.ReleaseDigest != release.ReleaseDigest {
		return nil, errors.New("connector artifact resolved release digest does not match catalog")
	}
	if resolved.SHA256 != release.Artifact.SHA256 {
		return nil, errors.New("connector artifact resolved SHA-256 does not match catalog")
	}
	if resolved.SizeBytes != release.Artifact.SizeBytes {
		return nil, errors.New("connector artifact resolved size does not match catalog")
	}
	if resolved.MediaType != release.Artifact.MediaType {
		return nil, errors.New("connector artifact resolved media type does not match catalog")
	}
	return endpoint, nil
}

func isLowerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func sameOrigin(left, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}
