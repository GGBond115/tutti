package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

var directFetcherNow = time.Date(2026, 8, 18, 2, 0, 0, 0, time.UTC)

type artifactDownloadResolverFunc func(context.Context, string) (contracts.ArtifactDownload, error)

func (resolve artifactDownloadResolverFunc) ResolveArtifactDownload(ctx context.Context, digest string) (contracts.ArtifactDownload, error) {
	return resolve(ctx, digest)
}

func TestDirectFetcherResolvesReleaseDigestImmediatelyBeforeDownload(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/signed/artifact.zip" || request.URL.Query().Get("signature") != "secret" {
			t.Fatalf("request = %s %s", request.Method, request.URL.RequestURI())
		}
		writer.Header().Set("Content-Type", "application/zip")
		writer.Header().Set("Content-Length", "3")
		_, _ = writer.Write([]byte("zip"))
	}))
	defer server.Close()

	release := directFetcherTestRelease()
	resolveCalls := 0
	resolver := artifactDownloadResolverFunc(func(_ context.Context, digest string) (contracts.ArtifactDownload, error) {
		resolveCalls++
		if digest != release.ReleaseDigest {
			t.Fatalf("resolved digest = %q", digest)
		}
		return resolvedArtifact(server.URL+"/signed/artifact.zip?signature=secret", release), nil
	})
	fetcher, err := NewDirectFetcher(DirectFetcherConfig{Resolver: resolver, HTTPClient: server.Client(), Now: func() time.Time { return directFetcherNow }})
	if err != nil {
		t.Fatal(err)
	}
	response, err := fetcher.Fetch(context.Background(), FetchRequest{Release: release})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, _ := io.ReadAll(response.Body)
	if string(content) != "zip" || response.ContentLength != 3 || response.MediaType != "application/zip" || resolveCalls != 1 {
		t.Fatalf("response = %#v content=%q resolveCalls=%d", response, content, resolveCalls)
	}
}

func TestDirectFetcherRejectsMissingOrExpiredResolution(t *testing.T) {
	release := directFetcherTestRelease()
	tests := []struct {
		name     string
		resolved contracts.ArtifactDownload
		resolve  error
		want     string
	}{
		{name: "resolve missing", resolve: errors.New("not found"), want: "resolve connector artifact"},
		{name: "URL missing", resolved: resolvedArtifact("", release), want: "URL is invalid"},
		{name: "expired", resolved: resolvedArtifact("https://artifacts.example.test/signed.zip", release), want: "expired"},
		{name: "expiry beyond bound", resolved: resolvedArtifact("https://artifacts.example.test/signed.zip", release), want: "expiry exceeds limit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved := test.resolved
			switch test.name {
			case "expired":
				resolved.ExpiresAt = directFetcherNow
			case "expiry beyond bound":
				resolved.ExpiresAt = directFetcherNow.Add(maxResolvedArtifactExpiry + time.Second)
			}
			fetcher, err := NewDirectFetcher(DirectFetcherConfig{
				Resolver:   artifactDownloadResolverFunc(func(context.Context, string) (contracts.ArtifactDownload, error) { return resolved, test.resolve }),
				HTTPClient: http.DefaultClient,
				Now:        func() time.Time { return directFetcherNow },
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = fetcher.Fetch(context.Background(), FetchRequest{Release: release})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDirectFetcherClassifiesArtifactHTTPFailures(t *testing.T) {
	release := directFetcherTestRelease()
	for _, test := range []struct {
		status    int
		retryable bool
	}{
		{status: http.StatusForbidden, retryable: false},
		{status: http.StatusNotFound, retryable: false},
		{status: http.StatusTooManyRequests, retryable: true},
		{status: http.StatusServiceUnavailable, retryable: true},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			fetcher, err := NewDirectFetcher(DirectFetcherConfig{
				Resolver: artifactDownloadResolverFunc(func(context.Context, string) (contracts.ArtifactDownload, error) {
					return resolvedArtifact(server.URL+"/artifact.zip", release), nil
				}),
				HTTPClient: server.Client(),
				Now:        func() time.Time { return directFetcherNow },
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = fetcher.Fetch(context.Background(), FetchRequest{Release: release})
			var domainError *contracts.DomainError
			if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeInstallFailed ||
				domainError.Retryable != test.retryable {
				t.Fatalf("status %d error = %#v", test.status, err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", test.status)) ||
				!strings.Contains(err.Error(), strings.TrimPrefix(server.URL, "https://")) {
				t.Fatalf("status %d error lacks safe response context: %v", test.status, err)
			}
			if strings.Contains(err.Error(), "/artifact.zip") {
				t.Fatalf("status %d error leaked resolved URL path: %v", test.status, err)
			}
		})
	}
}

func TestDirectFetcherRejectsUnsafeResolvedURL(t *testing.T) {
	release := directFetcherTestRelease()
	tests := []struct {
		name string
		url  string
	}{
		{name: "http", url: "http://artifacts.example.test/signed.zip"},
		{name: "missing host", url: "https:///signed.zip"},
		{name: "userinfo", url: "https://secret@artifacts.example.test/signed.zip"},
		{name: "fragment", url: "https://artifacts.example.test/signed.zip#secret"},
		{name: "overlong", url: "https://artifacts.example.test/signed.zip?token=" + strings.Repeat("a", maxResolvedArtifactURLBytes)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved := resolvedArtifact(test.url, release)
			fetcher, err := NewDirectFetcher(DirectFetcherConfig{
				Resolver:   artifactDownloadResolverFunc(func(context.Context, string) (contracts.ArtifactDownload, error) { return resolved, nil }),
				HTTPClient: http.DefaultClient,
				Now:        func() time.Time { return directFetcherNow },
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fetcher.Fetch(context.Background(), FetchRequest{Release: release}); err == nil || !strings.Contains(err.Error(), "URL is invalid") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDirectFetcherRejectsResolvedDescriptorMismatch(t *testing.T) {
	release := directFetcherTestRelease()
	tests := []struct {
		name   string
		mutate func(*contracts.ArtifactDownload)
		want   string
	}{
		{name: "release digest", mutate: func(value *contracts.ArtifactDownload) { value.ReleaseDigest = strings.Repeat("c", 64) }, want: "release digest"},
		{name: "sha256", mutate: func(value *contracts.ArtifactDownload) { value.SHA256 = strings.Repeat("c", 64) }, want: "SHA-256"},
		{name: "size", mutate: func(value *contracts.ArtifactDownload) { value.SizeBytes++ }, want: "size"},
		{name: "media type", mutate: func(value *contracts.ArtifactDownload) { value.MediaType = "application/gzip" }, want: "media type"},
		{name: "uppercase SHA-256", mutate: func(value *contracts.ArtifactDownload) { value.SHA256 = strings.Repeat("A", 64) }, want: "identity is invalid"},
		{name: "malformed release digest", mutate: func(value *contracts.ArtifactDownload) { value.ReleaseDigest = "not-a-digest" }, want: "identity is invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved := resolvedArtifact("https://artifacts.example.test/signed.zip", release)
			test.mutate(&resolved)
			fetcher, err := NewDirectFetcher(DirectFetcherConfig{
				Resolver:   artifactDownloadResolverFunc(func(context.Context, string) (contracts.ArtifactDownload, error) { return resolved, nil }),
				HTTPClient: http.DefaultClient,
				Now:        func() time.Time { return directFetcherNow },
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fetcher.Fetch(context.Background(), FetchRequest{Release: release}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDirectFetcherRejectsCrossOriginRedirect(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("cross-origin redirect target was reached")
	}))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+"/artifact.zip", http.StatusFound)
	}))
	defer source.Close()
	release := directFetcherTestRelease()
	fetcher, err := NewDirectFetcher(DirectFetcherConfig{
		Resolver: artifactDownloadResolverFunc(func(context.Context, string) (contracts.ArtifactDownload, error) {
			return resolvedArtifact(source.URL+"/signed.zip?signature=secret", release), nil
		}),
		HTTPClient: source.Client(), Now: func() time.Time { return directFetcherNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fetcher.Fetch(context.Background(), FetchRequest{Release: release})
	if !errors.Is(err, errArtifactRedirectOrigin) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "signature") || strings.Contains(err.Error(), source.URL) {
		t.Fatalf("signed URL leaked into error: %v", err)
	}
}

func TestDirectFetcherPreservesHostRedirectPolicy(t *testing.T) {
	policyError := errors.New("redirect blocked by host")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/artifact.zip", http.StatusFound)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return policyError }
	release := directFetcherTestRelease()
	fetcher, err := NewDirectFetcher(DirectFetcherConfig{
		Resolver: artifactDownloadResolverFunc(func(context.Context, string) (contracts.ArtifactDownload, error) {
			return resolvedArtifact(server.URL+"/signed.zip", release), nil
		}),
		HTTPClient: client, Now: func() time.Time { return directFetcherNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{Release: release}); !errors.Is(err, policyError) {
		t.Fatalf("error = %v", err)
	}
}

func TestDirectFetcherRequiresResolverAndHostHTTPClient(t *testing.T) {
	if _, err := NewDirectFetcher(DirectFetcherConfig{HTTPClient: http.DefaultClient}); err == nil || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("expected missing resolver error, got %v", err)
	}
	if _, err := NewDirectFetcher(DirectFetcherConfig{Resolver: artifactDownloadResolverFunc(func(context.Context, string) (contracts.ArtifactDownload, error) {
		return contracts.ArtifactDownload{}, nil
	})}); err == nil || !strings.Contains(err.Error(), "HTTP client") {
		t.Fatalf("expected missing HTTP client error, got %v", err)
	}
}

func resolvedArtifact(rawURL string, release contracts.Release) contracts.ArtifactDownload {
	return contracts.ArtifactDownload{
		URL: rawURL, ExpiresAt: directFetcherNow.Add(time.Minute), ReleaseDigest: release.ReleaseDigest,
		SHA256: release.Artifact.SHA256, SizeBytes: release.Artifact.SizeBytes, MediaType: release.Artifact.MediaType,
	}
}

func directFetcherTestRelease() contracts.Release {
	return contracts.Release{
		ConnectorKey: "github", Version: "1.0.0", ReleaseDigest: strings.Repeat("a", 64),
		Artifact:    contracts.Artifact{SHA256: strings.Repeat("b", 64), SizeBytes: 3, MediaType: "application/zip"},
		PublishedAt: time.Unix(1, 0).UTC(),
	}
}
