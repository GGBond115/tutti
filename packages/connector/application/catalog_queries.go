package application

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"github.com/tutti-os/tutti/packages/connector/contracts"
)

func (application *service) Snapshot(ctx context.Context) (contracts.Snapshot, error) {
	snapshot, err := application.config.Repository.Snapshot(ctx)
	return publicSnapshot(snapshot, contracts.OperationScope{}), err
}

func (application *service) SnapshotForScope(ctx context.Context, scope contracts.OperationScope) (contracts.Snapshot, error) {
	var snapshot contracts.Snapshot
	var err error
	if repository, ok := application.config.Repository.(ScopedSnapshotReader); ok {
		snapshot, err = repository.SnapshotForScope(ctx, scope)
	} else {
		snapshot, err = application.config.Repository.Snapshot(ctx)
	}
	if err != nil {
		return publicSnapshot(snapshot, scope), err
	}
	for index := range snapshot.Connectors {
		projected, projectionErr := application.projectConnectorForScope(ctx, scope, snapshot.Connectors[index])
		if projectionErr != nil {
			return contracts.Snapshot{}, projectionErr
		}
		snapshot.Connectors[index] = projected
	}
	return publicSnapshot(snapshot, scope), nil
}

// projectConnectorForScope is the sole owner of account authorization
// projection. Repositories return base catalog state; transports and Agent
// consumers receive this already-projected value and must not derive it again.
func (application *service) projectConnectorForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	connector contracts.Connector,
) (contracts.Connector, error) {
	if connector.Release.Manifest.AuthorizationKind == "none" {
		connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStateNotRequired}
		return connector, nil
	}
	disconnected := contracts.Authorization{State: contracts.AuthorizationStateDisconnected}
	accountID := strings.TrimSpace(scope.AccountID)
	if accountID == "" || application.config.AuthorizationProjections == nil {
		connector.Authorization = disconnected
		return connector, nil
	}
	projection, err := application.config.AuthorizationProjections.AuthorizationProjection(ctx, accountID, connector.Key)
	if errors.Is(err, contracts.ErrNotFound) {
		connector.Authorization = disconnected
		return connector, nil
	}
	if err != nil {
		return contracts.Connector{}, err
	}
	if projection.AccountID != accountID || projection.ConnectorKey != connector.Key {
		return contracts.Connector{}, invalidOperationReceipt("authorization projection identity does not match connector scope")
	}
	remote := connector.Release.Manifest.Implementation.RemoteStreamableHTTP != nil
	if remote && (!projection.ServerSynchronized || application.config.AuthorizationReadiness != nil &&
		!application.config.AuthorizationReadiness.Ready(accountID)) {
		connector.Authorization = disconnected
		return connector, nil
	}
	connector.Authorization = contracts.Authorization{State: projection.State, FailureCode: projection.FailureCode}
	return connector, nil
}

func publicSnapshot(snapshot contracts.Snapshot, scope contracts.OperationScope) contracts.Snapshot {
	operations := snapshot.Operations[:0]
	for _, operation := range snapshot.Operations {
		if contracts.OperationVisibleToScope(operation, scope) {
			operations = append(operations, operation)
		}
	}
	snapshot.Operations = operations
	return snapshot
}

func (application *service) ListCatalogCategories(ctx context.Context) ([]contracts.CatalogCategory, error) {
	view, err := application.config.Repository.CatalogView(ctx)
	if err != nil {
		return nil, err
	}
	if view.Freshness.SnapshotID == "" {
		return nil, catalogUnavailable(view.Freshness)
	}
	return view.Categories, nil
}

func (application *service) ListCatalogPageForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	query contracts.CatalogPageQuery,
) (contracts.CatalogPage, error) {
	query.SectionID = strings.TrimSpace(query.SectionID)
	query.PageToken = strings.TrimSpace(query.PageToken)
	query.InstallationFilter = contracts.CatalogInstallationFilter(strings.TrimSpace(string(query.InstallationFilter)))
	if query.SectionID == "" || query.PageSize < 1 || query.PageSize > 100 {
		return contracts.CatalogPage{}, invalidRequest("sectionId and a pageSize between 1 and 100 are required")
	}
	if query.InstallationFilter != "" && query.InstallationFilter != contracts.CatalogInstallationFilterNotInstalled {
		return contracts.CatalogPage{}, invalidRequest("installation filter is invalid")
	}
	view, err := application.config.Repository.CatalogView(ctx)
	if err != nil {
		return contracts.CatalogPage{}, err
	}
	if view.Freshness.SnapshotID == "" {
		return contracts.CatalogPage{}, catalogUnavailable(view.Freshness)
	}
	offset, err := decodeCatalogPageToken(query.PageToken, view.Freshness.SnapshotID, query.SectionID, query.InstallationFilter)
	if err != nil {
		return contracts.CatalogPage{}, err
	}
	items := view.ListingsBySection[query.SectionID]
	if query.InstallationFilter == contracts.CatalogInstallationFilterNotInstalled {
		filtered := make([]contracts.CatalogListing, 0, len(items))
		for _, item := range items {
			if !connectorHasInstalledArtifact(item.Connector) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if offset > len(items) {
		return contracts.CatalogPage{}, invalidRequest("pageToken is outside the active catalog snapshot")
	}
	end := offset + query.PageSize
	if end > len(items) {
		end = len(items)
	}
	result := contracts.CatalogPage{SectionID: query.SectionID, Items: append([]contracts.CatalogListing(nil), items[offset:end]...), Revision: view.Revision}
	for index := range result.Items {
		projected, projectionErr := application.projectConnectorForScope(ctx, scope, result.Items[index].Connector)
		if projectionErr != nil {
			return contracts.CatalogPage{}, projectionErr
		}
		result.Items[index].Connector = projected
	}
	if end < len(items) {
		result.NextPageToken = encodeCatalogPageToken(view.Freshness.SnapshotID, query.SectionID, query.InstallationFilter, end)
	}
	return result, nil
}

func connectorHasInstalledArtifact(connector contracts.Connector) bool {
	installation := connector.Installation
	if installation.State == contracts.InstallationStateNotInstalled || installation.State == contracts.InstallationStateInstalling {
		return false
	}
	if installationRequiresPhysicalRepair(installation) {
		return false
	}
	if installation.InstalledReleaseDigest != "" || installation.InstalledReleaseID != "" || installation.InstalledVersion != "" {
		return true
	}
	return installation.State == contracts.InstallationStateInstalled ||
		installation.State == contracts.InstallationStateUpdating ||
		installation.State == contracts.InstallationStateUninstalling
}

func catalogUnavailable(freshness contracts.CatalogFreshness) error {
	message := "connector catalog has no accepted snapshot"
	if freshness.LastFailure != "" {
		message += ": " + freshness.LastFailure
	}
	return contracts.NewDomainError(contracts.ErrorCodeUnavailable, message, true, nil)
}

func encodeCatalogPageToken(snapshotID, sectionID string, filter contracts.CatalogInstallationFilter, offset int) string {
	payload := strings.Join([]string{snapshotID, sectionID, string(filter), strconv.Itoa(offset)}, "\n")
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCatalogPageToken(token, activeSnapshotID, sectionID string, filter contracts.CatalogInstallationFilter) (int, error) {
	if token == "" {
		return 0, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, invalidRequest("pageToken is invalid")
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 4 || parts[0] != activeSnapshotID {
		return 0, contracts.NewDomainError(contracts.ErrorCodeRevisionConflict, "pageToken belongs to a different catalog snapshot", true, nil)
	}
	if parts[1] != sectionID || parts[2] != string(filter) {
		return 0, invalidRequest("pageToken belongs to a different catalog query")
	}
	offset, err := strconv.Atoi(parts[3])
	if err != nil || offset < 0 {
		return 0, invalidRequest("pageToken is invalid")
	}
	return offset, nil
}

func (application *service) GetConnectorForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	connectorKey string,
) (contracts.Connector, error) {
	if strings.TrimSpace(connectorKey) == "" {
		return contracts.Connector{}, invalidRequest("connectorKey is required")
	}
	connector, err := application.config.Repository.Connector(ctx, connectorKey)
	if err != nil {
		return contracts.Connector{}, err
	}
	return application.projectConnectorForScope(ctx, scope, connector)
}

func (application *service) GetOperation(ctx context.Context, operationID string) (contracts.Operation, error) {
	if strings.TrimSpace(operationID) == "" {
		return contracts.Operation{}, invalidRequest("operationID is required")
	}
	operation, err := application.config.Repository.Operation(ctx, operationID)
	if err != nil {
		return contracts.Operation{}, err
	}
	if operation.Kind == contracts.OperationKindReconcileRuntime {
		return contracts.Operation{}, contracts.ErrNotFound
	}
	return operation, nil
}

func (application *service) GetOperationForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	operationID string,
) (contracts.Operation, error) {
	if strings.TrimSpace(operationID) == "" {
		return contracts.Operation{}, invalidRequest("operationID is required")
	}
	operation, err := application.config.Repository.OperationForScope(ctx, scope, operationID)
	if err != nil {
		return contracts.Operation{}, err
	}
	if !contracts.OperationVisibleToScope(operation, scope) {
		return contracts.Operation{}, contracts.ErrNotFound
	}
	return operation, nil
}

func (application *service) RefreshCatalog(
	ctx context.Context,
	mutation contracts.Mutation,
) (contracts.MutationResult, error) {
	return application.acceptOperation(ctx, mutation, contracts.OperationKindRefreshCatalog, "")
}
