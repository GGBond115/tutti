package connectormarket

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	marketartifact "github.com/tutti-os/tutti/packages/connector/market/artifact"
)

func TestArtifactGrantFetcherBindsGrantToSignedRelease(t *testing.T) {
	now := time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != artifactGrantPath || request.Header.Get("Idempotency-Key") != "operation-1" {
			t.Fatalf("request = %s headers=%v", request.URL.Path, request.Header)
		}
		_, _ = writer.Write([]byte(`{"downloadUrl":"https://artifacts.example.test/github.zip","method":"GET","expiresAtMs":1785808980000,"connectorKey":"github","releaseDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","artifactSha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","sizeBytes":3,"mediaType":"application/zip","objectVersion":"generation-1"}`))
	}))
	defer server.Close()
	fetcher, err := NewArtifactGrantFetcher(ArtifactGrantFetcherConfig{
		BaseURL: server.URL, AllowedDownloadHosts: []string{"artifacts.example.test"},
		Now: func() time.Time { return now },
		DownloadClient: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("zip")),
				ContentLength: 3, Header: http.Header{"Content-Type": []string{"application/zip"}}, Request: request}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	release := catalogTestRelease()
	release.Artifact.SizeBytes = 3
	release.Artifact.MediaType = "application/zip"
	response, err := fetcher.Fetch(context.Background(), marketartifact.FetchRequest{OperationID: "operation-1", WorkspaceID: "workspace-1", Release: release})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, _ := io.ReadAll(response.Body)
	if string(content) != "zip" || response.ContentLength != 3 {
		t.Fatalf("response = %#v content=%q", response, content)
	}
}

func TestArtifactGrantFetcherRejectsMismatchedGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"downloadUrl":"https://artifacts.example.test/github.zip","method":"GET","expiresAtMs":1785808980000,"connectorKey":"github","releaseDigest":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","artifactSha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","sizeBytes":123,"mediaType":"application/vnd.tutti.connector+zip","objectVersion":"generation-1"}`))
	}))
	defer server.Close()
	fetcher, err := NewArtifactGrantFetcher(ArtifactGrantFetcherConfig{BaseURL: server.URL,
		AllowedDownloadHosts: []string{"artifacts.example.test"}, Now: func() time.Time { return time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fetcher.Fetch(context.Background(), marketartifact.FetchRequest{OperationID: "operation-1", WorkspaceID: "workspace-1", Release: catalogTestRelease()})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
