package host

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInstallRequiresFreshCatalogSnapshot(t *testing.T) {
	for _, test := range []struct {
		name      string
		freshness CatalogFreshness
	}{
		{name: "unavailable", freshness: CatalogFreshness{State: CatalogFreshnessUnavailable}},
		{name: "stale", freshness: CatalogFreshness{State: CatalogFreshnessStale, SnapshotID: "catalog-1"}},
		{name: "refreshing after stale", freshness: CatalogFreshness{State: CatalogFreshnessRefreshing, SnapshotID: "catalog-1", StaleSince: timePointer(time.Now())}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryRepository(testConnector("github"))
			repository.catalogView.Freshness = test.freshness
			application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, CatalogSnapshot{})
			_, err := application.Install(context.Background(), ConnectorMutation{
				Mutation: Mutation{ClientRequestID: "install-1"}, ConnectorKey: "github",
			})
			var domainError *DomainError
			if !errors.As(err, &domainError) || domainError.Code != ErrorCodeUpstreamUnavailable || !domainError.Retryable {
				t.Fatalf("error = %#v", err)
			}
			if len(repository.operations) != 0 {
				t.Fatalf("operations = %#v", repository.operations)
			}
		})
	}
}

func TestInstallAllowsInitialRefreshWithAcceptedSnapshot(t *testing.T) {
	repository := newMemoryRepository(testConnector("github"))
	repository.catalogView.Freshness = CatalogFreshness{State: CatalogFreshnessRefreshing, SnapshotID: "catalog-1"}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, CatalogSnapshot{})
	if _, err := application.Install(context.Background(), ConnectorMutation{
		Mutation: Mutation{ClientRequestID: "install-1"}, ConnectorKey: "github",
	}); err != nil {
		t.Fatal(err)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
