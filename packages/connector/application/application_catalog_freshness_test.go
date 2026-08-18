package application

import (
	"context"
	"errors"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"testing"
	"time"
)

func TestInstallRequiresFreshCatalogSnapshot(t *testing.T) {
	for _, test := range []struct {
		name      string
		freshness contracts.CatalogFreshness
	}{
		{name: "unavailable", freshness: contracts.CatalogFreshness{State: contracts.CatalogFreshnessUnavailable}},
		{name: "stale", freshness: contracts.CatalogFreshness{State: contracts.CatalogFreshnessStale, SnapshotID: "catalog-1"}},
		{name: "refreshing after stale", freshness: contracts.CatalogFreshness{State: contracts.CatalogFreshnessRefreshing, SnapshotID: "catalog-1", StaleSince: timePointer(time.Now())}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newMemoryRepository(testConnector("github"))
			repository.catalogView.Freshness = test.freshness
			application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
			_, err := application.Install(context.Background(), contracts.ConnectorMutation{
				Mutation: contracts.Mutation{ClientRequestID: "install-1"}, ConnectorKey: "github",
			})
			var domainError *contracts.DomainError
			if !errors.As(err, &domainError) || domainError.Code != contracts.ErrorCodeUpstreamUnavailable || !domainError.Retryable {
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
	repository.catalogView.Freshness = contracts.CatalogFreshness{State: contracts.CatalogFreshnessRefreshing, SnapshotID: "catalog-1"}
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, contracts.CatalogSnapshot{})
	if _, err := application.Install(context.Background(), contracts.ConnectorMutation{
		Mutation: contracts.Mutation{ClientRequestID: "install-1"}, ConnectorKey: "github",
	}); err != nil {
		t.Fatal(err)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
