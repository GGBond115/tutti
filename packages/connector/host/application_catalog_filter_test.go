package host

import (
	"context"
	"testing"
)

func TestApplicationCatalogPageFiltersInstalledConnectorsBeforePagination(t *testing.T) {
	installed := installedTestConnector("github")
	available := testConnector("notion")
	repository := newMemoryRepository(installed, available)
	setMemoryCatalogListings(repository, "other", installed, available)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, CatalogSnapshot{})

	page, err := application.ListCatalogPage(context.Background(), CatalogPageQuery{
		SectionID: "other", PageSize: 1, InstallationFilter: CatalogInstallationFilterNotInstalled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Connector.Key != "notion" || page.NextPageToken != "" {
		t.Fatalf("filtered page = %#v", page)
	}
}

func TestApplicationCatalogPageTreatsPhysicalRepairAsNotInstalled(t *testing.T) {
	repair := testConnector("github")
	repair.Installation = Installation{
		State: InstallationStateFailed, InstalledVersion: repair.Release.Version,
		InstalledReleaseID: repair.Release.ReleaseID, InstalledReleaseDigest: repair.Release.ReleaseDigest,
		FailureCode: InstallationFailureCodePhysicallyAbsent,
	}
	repository := newMemoryRepository(repair)
	setMemoryCatalogListings(repository, "other", repair)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, CatalogSnapshot{})

	page, err := application.ListCatalogPage(context.Background(), CatalogPageQuery{
		SectionID: "other", PageSize: 20, InstallationFilter: CatalogInstallationFilterNotInstalled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Connector.Key != "github" {
		t.Fatalf("repair page = %#v", page)
	}
}

func TestApplicationCatalogPageExhaustsInstalledOnlySnapshot(t *testing.T) {
	github := installedTestConnector("github")
	slack := installedTestConnector("slack")
	repository := newMemoryRepository(github, slack)
	setMemoryCatalogListings(repository, "other", github, slack)
	application := newTestApplication(t, repository, &memoryScheduler{}, &memoryInstallRuntime{}, CatalogSnapshot{})

	page, err := application.ListCatalogPage(context.Background(), CatalogPageQuery{
		SectionID: "other", PageSize: 20, InstallationFilter: CatalogInstallationFilterNotInstalled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.NextPageToken != "" {
		t.Fatalf("installed-only page = %#v", page)
	}
}

func setMemoryCatalogListings(repository *memoryRepository, sectionID string, connectors ...Connector) {
	listings := make([]CatalogListing, 0, len(connectors))
	for _, connector := range connectors {
		listings = append(listings, CatalogListing{CategoryID: sectionID, Connector: connector})
	}
	repository.catalogView.Categories = []CatalogCategory{{CategoryID: sectionID, Kind: "category", ItemCount: int64(len(connectors))}}
	repository.catalogView.ListingsBySection[sectionID] = listings
}

func installedTestConnector(key string) Connector {
	connector := testConnector(key)
	connector.Installation = Installation{
		State: InstallationStateInstalled, InstalledVersion: connector.Release.Version,
		InstalledReleaseID: connector.Release.ReleaseID, InstalledReleaseDigest: connector.Release.ReleaseDigest,
	}
	return connector
}
