package api

import (
	"context"
	application "github.com/tutti-os/tutti/packages/connector/application"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"net/http"
	"testing"
	"time"
)

type stubConnectorMarketService struct {
	application.StateQueries
	application.CatalogQueries
	application.CatalogCommands
	application.InstallationCommands
	application.AuthorizationCommands
	application.OperationQueries
	snapshotFn   func(context.Context) (contracts.Snapshot, error)
	categoriesFn func(context.Context) ([]contracts.CatalogCategory, error)
	pageFn       func(context.Context, contracts.CatalogPageQuery) (contracts.CatalogPage, error)
	connectorFn  func(context.Context, contracts.OperationScope, string) (contracts.Connector, error)
	installFn    func(context.Context, contracts.ConnectorMutation) contracts.CommandResult
	uninstallFn  func(context.Context, contracts.ConnectorMutation) contracts.CommandResult
	refreshFn    func(context.Context, contracts.Mutation) contracts.CommandResult
	operationFn  func(context.Context, contracts.OperationScope, string) (contracts.Operation, error)
	cancelFn     func(context.Context, contracts.CancelAuthorizationCommand) contracts.CommandResult
	beginFn      func(context.Context, contracts.ConnectorMutation, []byte) contracts.AuthorizationCommandResult
	projectionFn func(context.Context, string, string) (contracts.AuthorizationProjection, error)
}

func connectorMarketTestAPI(service stubConnectorMarketService) DaemonAPI {
	return DaemonAPI{
		ConnectorCatalogQueries:        service,
		ConnectorCatalogCommands:       service,
		ConnectorInstallationCommands:  service,
		ConnectorAuthorizationCommands: service,
		ConnectorOperationQueries:      service,
	}
}

func connectorMarketTestAPIWithScope(
	service stubConnectorMarketService,
	currentScope func() contracts.OperationScope,
) DaemonAPI {
	api := connectorMarketTestAPI(service)
	api.ConnectorMarketScope = currentScope
	return api
}

func (service stubConnectorMarketService) BeginAuthorization(
	ctx context.Context,
	mutation contracts.ConnectorMutation,
	secret []byte,
) contracts.AuthorizationCommandResult {
	return service.beginFn(ctx, mutation, secret)
}

func (service stubConnectorMarketService) CancelAuthorization(ctx context.Context, command contracts.CancelAuthorizationCommand) contracts.CommandResult {
	if service.cancelFn == nil {
		return contracts.CommandResult{Outcome: contracts.CommandCompleted}
	}
	return service.cancelFn(ctx, command)
}

func (service stubConnectorMarketService) GetAuthorizationProjection(ctx context.Context, accountID, connectorKey string) (contracts.AuthorizationProjection, error) {
	if service.projectionFn == nil {
		return contracts.AuthorizationProjection{}, contracts.ErrNotFound
	}
	return service.projectionFn(ctx, accountID, connectorKey)
}

func (service stubConnectorMarketService) Snapshot(ctx context.Context) (contracts.Snapshot, error) {
	return service.snapshotFn(ctx)
}

func (service stubConnectorMarketService) SnapshotForScope(ctx context.Context, scope contracts.OperationScope) (contracts.Snapshot, error) {
	snapshot, err := service.snapshotFn(ctx)
	if err != nil || service.projectionFn == nil {
		return snapshot, err
	}
	for index := range snapshot.Connectors {
		projection, projectionErr := service.projectionFn(ctx, scope.AccountID, snapshot.Connectors[index].Key)
		if projectionErr != nil {
			return contracts.Snapshot{}, projectionErr
		}
		snapshot.Connectors[index].Authorization = contracts.Authorization{State: projection.State, FailureCode: projection.FailureCode}
	}
	return snapshot, nil
}

func (service stubConnectorMarketService) SnapshotViewForScope(ctx context.Context, scope contracts.OperationScope) (contracts.SnapshotView, error) {
	snapshot, err := service.SnapshotForScope(ctx, scope)
	if err != nil {
		return contracts.SnapshotView{}, err
	}
	result := contracts.SnapshotView{
		CatalogFreshness: snapshot.CatalogFreshness, Operations: snapshot.Operations,
		Revision: snapshot.Revision, EventCursor: snapshot.EventCursor,
	}
	for _, connector := range snapshot.Connectors {
		result.Connectors = append(result.Connectors, connectorMarketTestView(connector))
	}
	return result, nil
}

func (service stubConnectorMarketService) Install(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	return service.installFn(ctx, mutation)
}

func (service stubConnectorMarketService) Uninstall(ctx context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
	return service.uninstallFn(ctx, mutation)
}

func (service stubConnectorMarketService) RefreshCatalog(ctx context.Context, mutation contracts.Mutation) contracts.CommandResult {
	return service.refreshFn(ctx, mutation)
}

func (service stubConnectorMarketService) GetOperationForScope(
	ctx context.Context,
	scope contracts.OperationScope,
	operationID string,
) (contracts.Operation, error) {
	return service.operationFn(ctx, scope, operationID)
}

func (service stubConnectorMarketService) ListCatalogCategories(ctx context.Context) ([]contracts.CatalogCategory, error) {
	return service.categoriesFn(ctx)
}

func (service stubConnectorMarketService) ListCatalogPageForScope(ctx context.Context, _ contracts.OperationScope, query contracts.CatalogPageQuery) (contracts.CatalogPage, error) {
	return service.pageFn(ctx, query)
}

func (service stubConnectorMarketService) GetConnectorForScope(ctx context.Context, scope contracts.OperationScope, connectorKey string) (contracts.Connector, error) {
	if service.connectorFn == nil {
		return contracts.Connector{}, contracts.ErrNotFound
	}
	return service.connectorFn(ctx, scope, connectorKey)
}

func (service stubConnectorMarketService) ListCatalogPageViewForScope(ctx context.Context, scope contracts.OperationScope, query contracts.CatalogPageQuery) (contracts.CatalogPageView, error) {
	page, err := service.ListCatalogPageForScope(ctx, scope, query)
	if err != nil {
		return contracts.CatalogPageView{}, err
	}
	result := contracts.CatalogPageView{
		SectionID: page.SectionID, NextPageToken: page.NextPageToken, Revision: page.Revision,
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, contracts.CatalogListingView{
			CategoryID: item.CategoryID, Featured: item.Featured, Connector: connectorMarketTestView(item.Connector),
		})
	}
	return result, nil
}

func (service stubConnectorMarketService) GetConnectorViewForScope(ctx context.Context, scope contracts.OperationScope, connectorKey string) (contracts.ConnectorView, error) {
	connector, err := service.GetConnectorForScope(ctx, scope, connectorKey)
	if err != nil {
		return contracts.ConnectorView{}, err
	}
	return connectorMarketTestView(connector), nil
}

func (stubConnectorMarketService) PresentConnectorForScope(_ context.Context, _ contracts.OperationScope, connector contracts.Connector) (contracts.ConnectorView, error) {
	return connectorMarketTestView(connector), nil
}

func connectorMarketTestView(connector contracts.Connector) contracts.ConnectorView {
	state := contracts.ConnectorStateSetupRequired
	reason := "connector_not_installed"
	actions := []contracts.ConnectorAction{contracts.ConnectorActionDetails, contracts.ConnectorActionRemoveSelection, contracts.ConnectorActionInstall}
	if connector.Installation.State == contracts.InstallationStateInstalled {
		state = contracts.ConnectorStateUnsupported
		reason = "test_runtime_observation_missing"
		actions = []contracts.ConnectorAction{contracts.ConnectorActionDetails, contracts.ConnectorActionRemoveSelection}
	}
	return contracts.ConnectorView{Connector: connector, Presentation: contracts.ConnectorPresentation{
		State: state, ReasonCode: reason, AllowedActions: actions,
	}}
}

func TestDaemonAPIConnectorMarketSnapshotHidesImplementationConfig(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 18, 1, 2, 3, 0, time.UTC)
	staleSince := acceptedAt.Add(time.Hour)
	service := stubConnectorMarketService{
		snapshotFn: func(_ context.Context) (contracts.Snapshot, error) {
			return contracts.Snapshot{
				CatalogFreshness: contracts.CatalogFreshness{
					State: contracts.CatalogFreshnessStale, SnapshotID: "catalog-7", SourceRevision: "server-revision-7",
					AcceptedAt: &acceptedAt, StaleSince: &staleSince, LastFailure: "connector_market_upstream_unavailable",
				},
				Connectors: []contracts.Connector{connectorMarketTestConnector()}, Operations: []contracts.Operation{},
				Revision: 7,
			}, nil
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPI(service)))

	recorder := performGeneratedRouteRequest(t, mux, http.MethodGet, "/v1/connector-market", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var raw map[string]any
	decodeGeneratedRouteResponse(t, recorder, &raw)
	freshness := raw["catalogFreshness"].(map[string]any)
	if freshness["state"] != string(contracts.CatalogFreshnessStale) || freshness["snapshotId"] != "catalog-7" ||
		freshness["sourceRevision"] != "server-revision-7" || freshness["acceptedAt"] != acceptedAt.Format(time.RFC3339) ||
		freshness["staleSince"] != staleSince.Format(time.RFC3339) || freshness["lastFailure"] != "connector_market_upstream_unavailable" {
		t.Fatalf("catalog freshness = %#v", freshness)
	}
	connectors := raw["connectors"].([]any)
	connector := connectors[0].(map[string]any)
	presentation := connector["presentation"].(map[string]any)
	if presentation["state"] != string(contracts.ConnectorStateSetupRequired) {
		t.Fatalf("presentation = %#v", presentation)
	}
	release := connector["release"].(map[string]any)
	manifest := release["manifest"].(map[string]any)
	implementation := manifest["implementation"].(map[string]any)
	if _, exists := implementation["config"]; exists {
		t.Fatalf("public implementation leaked config: %#v", implementation)
	}
	if implementation["kind"] != contracts.ImplementationKindManagedStdio {
		t.Fatalf("implementation.kind = %#v, want managed_stdio", implementation["kind"])
	}
	if manifest["authorizationInteractionMode"] != contracts.AuthorizationInteractionModeManaged {
		t.Fatalf("authorizationInteractionMode = %#v, want managed", manifest["authorizationInteractionMode"])
	}
	routing := manifest["agentRouting"].(map[string]any)
	aliases := routing["aliases"].([]any)
	if len(aliases) != 2 || aliases[0] != "Notion" || aliases[1] != "Notion AI" {
		t.Fatalf("public agent routing aliases = %#v", aliases)
	}
}

func TestDaemonAPIConnectorMarketEmitsPresentationOnPageGetAndCommandConnector(t *testing.T) {
	connector := connectorMarketTestConnector()
	service := stubConnectorMarketService{
		pageFn: func(context.Context, contracts.CatalogPageQuery) (contracts.CatalogPage, error) {
			return contracts.CatalogPage{
				SectionID: "all", Items: []contracts.CatalogListing{{CategoryID: "all", Connector: connector}}, Revision: 7,
			}, nil
		},
		connectorFn: func(_ context.Context, _ contracts.OperationScope, key string) (contracts.Connector, error) {
			if key != connector.Key {
				return contracts.Connector{}, contracts.ErrNotFound
			}
			return connector, nil
		},
		installFn: func(_ context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
			operation := contracts.Operation{
				OperationID: "operation-1", ClientRequestID: mutation.ClientRequestID, ConnectorKey: connector.Key,
				Kind: contracts.OperationKindInstall, State: contracts.OperationStateAccepted,
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}
			return contracts.CommandResult{
				Outcome: contracts.CommandAccepted, Revision: 8, Connector: &connector, Operation: &operation,
			}
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPI(service)))

	requests := []struct {
		method string
		path   string
		body   any
		pluck  func(map[string]any) map[string]any
	}{
		{method: http.MethodGet, path: "/v1/connector-market/catalog?sectionId=all", pluck: func(body map[string]any) map[string]any {
			return body["items"].([]any)[0].(map[string]any)["connector"].(map[string]any)
		}},
		{method: http.MethodGet, path: "/v1/connector-market/connectors/notion", pluck: func(body map[string]any) map[string]any { return body }},
		{method: http.MethodPost, path: "/v1/connector-market/connectors/notion:install", body: map[string]any{
			"clientRequestId": "request-1", "expectedRevision": 7,
		}, pluck: func(body map[string]any) map[string]any { return body["connector"].(map[string]any) }},
	}
	for _, request := range requests {
		recorder := performGeneratedRouteRequest(t, mux, request.method, request.path, request.body)
		if recorder.Code != http.StatusOK && recorder.Code != http.StatusAccepted {
			t.Fatalf("%s %s status = %d; body: %s", request.method, request.path, recorder.Code, recorder.Body.String())
		}
		var body map[string]any
		decodeGeneratedRouteResponse(t, recorder, &body)
		presentation, ok := request.pluck(body)["presentation"].(map[string]any)
		if !ok || presentation["state"] != string(contracts.ConnectorStateSetupRequired) {
			t.Fatalf("%s %s presentation = %#v", request.method, request.path, presentation)
		}
	}
}

func TestConnectorMarketPresentationActionRejectsUnimplementedRetry(t *testing.T) {
	if tuttigenerated.ConnectorMarketPresentationAction("retry").Valid() {
		t.Fatal("retry unexpectedly remained a valid Connector presentation action")
	}
}

func TestProjectConnectorMarketPreservesRuntimeAuthorizationView(t *testing.T) {
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketAuthorizationResponse](contracts.AuthorizationResult{
		AuthorizationView: &contracts.AuthorizationViewEnvelope{
			Protocol: contracts.AuthorizationViewProtocolV1,
			ViewID:   "authorization-session-1",
			View: contracts.AuthorizationView{
				Type: contracts.AuthorizationViewTypeQRCode,
				Source: &contracts.AuthorizationQRCodeSource{
					Type: contracts.AuthorizationQRCodeSourcePayload, Value: "opaque-payload",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projected.AuthorizationView == nil {
		t.Fatal("runtime authorization view was dropped")
	}
	view, ok := (*projected.AuthorizationView)["view"].(map[string]any)
	if !ok || view["type"] != contracts.AuthorizationViewTypeQRCode {
		t.Fatalf("projected authorization view = %#v", projected.AuthorizationView)
	}
}

func TestDaemonAPICancelsConnectorAuthorizationForActiveAccount(t *testing.T) {
	var got contracts.CancelAuthorizationCommand
	service := stubConnectorMarketService{cancelFn: func(_ context.Context, command contracts.CancelAuthorizationCommand) contracts.CommandResult {
		got = command
		return contracts.CommandResult{Outcome: contracts.CommandCompleted, Revision: 9}
	}}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPIWithScope(service, func() contracts.OperationScope {
		return contracts.OperationScope{AccountID: "account-1"}
	})))

	recorder := performGeneratedRouteRequest(t, mux, http.MethodPost,
		"/v1/connector-market/connectors/supabase/authorization:cancel", map[string]any{
			"clientRequestId": "cancel-1", "expectedRevision": 8,
			"expectedConnectorRevision": 7, "operationId": "authorization-1",
		})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got.AccountID != "account-1" || got.ConnectorKey != "supabase" || got.OperationID != "authorization-1" ||
		got.ClientRequestID != "cancel-1" || got.ExpectedConnectorRevision == nil || *got.ExpectedConnectorRevision != 7 {
		t.Fatalf("cancel command=%#v", got)
	}
}

func TestDaemonAPIForwardsReplaceActiveAuthorizationPolicy(t *testing.T) {
	var received contracts.ConnectorMutation
	service := stubConnectorMarketService{beginFn: func(
		_ context.Context,
		mutation contracts.ConnectorMutation,
		_ []byte,
	) contracts.AuthorizationCommandResult {
		received = mutation
		connector := connectorMarketTestConnector()
		connector.Authorization = contracts.Authorization{State: contracts.AuthorizationStatePending}
		operation := contracts.Operation{
			OperationID: "operation-b", ClientRequestID: mutation.ClientRequestID,
			ConnectorKey: connector.Key, Kind: contracts.OperationKindStartAuthorization,
			State: contracts.OperationStateAccepted, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		expiresAt := time.Now().Add(time.Minute)
		return contracts.AuthorizationCommandResult{
			CommandResult:    contracts.CommandResult{Outcome: contracts.CommandAccepted, Connector: &connector, Operation: &operation, Revision: 2},
			AuthorizationURL: "https://accounts.example.com/oauth", AuthorizationExpiresAt: &expiresAt,
		}
	}}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPIWithScope(service, func() contracts.OperationScope {
		return contracts.OperationScope{AccountID: "account-1"}
	})))

	recorder := performGeneratedRouteRequest(t, mux, http.MethodPost,
		"/v1/connector-market/connectors/notion/authorization:start", map[string]any{
			"clientRequestId": "authorization-b", "expectedRevision": 1,
			"replacementPolicy": "replace_active",
		})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if received.AccountID != "account-1" || received.ClientRequestID != "authorization-b" ||
		received.ReplacementPolicy != contracts.AuthorizationReplacementPolicyReplaceActive {
		t.Fatalf("authorization mutation = %#v", received)
	}
}

func TestDaemonAPIConnectorMarketProjectsApplicationAuthorizationWithoutDerivation(t *testing.T) {
	service := stubConnectorMarketService{
		snapshotFn: func(context.Context) (contracts.Snapshot, error) {
			return contracts.Snapshot{Connectors: []contracts.Connector{connectorMarketTestConnector()}}, nil
		},
		projectionFn: func(_ context.Context, accountID, connectorKey string) (contracts.AuthorizationProjection, error) {
			if accountID != "account-1" || connectorKey != "notion" {
				t.Fatalf("projection scope = %q/%q", accountID, connectorKey)
			}
			return contracts.AuthorizationProjection{AccountID: accountID, ConnectorKey: connectorKey, State: contracts.AuthorizationStateConnected}, nil
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPIWithScope(service, func() contracts.OperationScope {
		return contracts.OperationScope{AccountID: "account-1"}
	})))
	recorder := performGeneratedRouteRequest(t, mux, http.MethodGet, "/v1/connector-market", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Connectors []struct {
			Authorization struct {
				State string `json:"state"`
			} `json:"authorization"`
		} `json:"connectors"`
	}
	decodeGeneratedRouteResponse(t, recorder, &body)
	if len(body.Connectors) != 1 || body.Connectors[0].Authorization.State != string(contracts.AuthorizationStateConnected) {
		t.Fatalf("body = %#v", body)
	}
}

func TestDaemonAPIConnectorMarketInstallMapsUnsupportedImplementation(t *testing.T) {
	service := stubConnectorMarketService{
		installFn: func(_ context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
			if mutation.ConnectorKey != "notion" || mutation.ClientRequestID != "request-1" || mutation.ExpectedRevision != 7 {
				t.Fatalf("mutation = %#v", mutation)
			}
			return contracts.CommandResult{Outcome: contracts.CommandRejected, Failure: &contracts.CommandFailure{
				Code: contracts.ErrorCodeUnsupportedImplementation, Message: "connector implementation is not registered",
			}}
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPI(service)))

	recorder := performGeneratedRouteRequest(t, mux, http.MethodPost, "/v1/connector-market/connectors/notion:install", map[string]any{
		"clientRequestId":  "request-1",
		"expectedRevision": 7,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response tuttigenerated.ConnectorMarketMutationResponse
	decodeGeneratedRouteResponse(t, recorder, &response)
	if response.Outcome != tuttigenerated.ConnectorMarketCommandOutcomeRejected || response.Failure == nil ||
		response.Failure.Code != tuttigenerated.ConnectorImplementationUnsupported || response.Failure.Retryable {
		t.Fatalf("response = %#v", response)
	}
}

func TestDaemonAPIConnectorMarketRefusesMalformedStructuredCommandResult(t *testing.T) {
	service := stubConnectorMarketService{
		installFn: func(context.Context, contracts.ConnectorMutation) contracts.CommandResult {
			return contracts.CommandResult{Outcome: contracts.CommandAccepted, Revision: 8}
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPI(service)))
	recorder := performGeneratedRouteRequest(t, mux, http.MethodPost,
		"/v1/connector-market/connectors/notion:install", map[string]any{
			"clientRequestId": "request-invalid", "expectedRevision": 7,
			"expectedConnectorRevision": 7,
		})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestDaemonAPIConnectorMarketUninstallPreservesMutationScope(t *testing.T) {
	now := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	service := stubConnectorMarketService{
		uninstallFn: func(_ context.Context, mutation contracts.ConnectorMutation) contracts.CommandResult {
			if mutation.ConnectorKey != "notion" || mutation.ClientRequestID != "request-uninstall-1" ||
				mutation.ExpectedRevision != 7 || mutation.AccountID != "account-1" {
				t.Fatalf("mutation = %#v", mutation)
			}
			operation := contracts.Operation{
				OperationID: "operation-uninstall-1", ClientRequestID: mutation.ClientRequestID,
				ConnectorKey: mutation.ConnectorKey, Kind: contracts.OperationKindUninstall,
				State: contracts.OperationStateAccepted, Stage: contracts.OperationStageAccepted,
				CreatedAt: now, UpdatedAt: now,
			}
			return contracts.CommandResult{Outcome: contracts.CommandAccepted, Operation: &operation, Revision: 8}
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPIWithScope(service, func() contracts.OperationScope {
		return contracts.OperationScope{AccountID: "account-1"}
	})))

	recorder := performGeneratedRouteRequest(t, mux, http.MethodPost, "/v1/connector-market/connectors/notion:uninstall", map[string]any{
		"clientRequestId":  "request-uninstall-1",
		"expectedRevision": 7,
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	var response tuttigenerated.ConnectorMarketMutationResponse
	decodeGeneratedRouteResponse(t, recorder, &response)
	if response.Outcome != tuttigenerated.ConnectorMarketCommandOutcomeAccepted || response.Operation == nil ||
		response.Operation.Kind != tuttigenerated.Uninstall || response.Revision != 8 {
		t.Fatalf("response = %#v", response)
	}
}

func TestDaemonAPIConnectorMarketRefreshRejectsNegativeRevision(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPI(stubConnectorMarketService{})))

	recorder := performGeneratedRouteRequest(t, mux, http.MethodPost, "/v1/connector-market:refresh", map[string]any{
		"clientRequestId":  "request-1",
		"expectedRevision": -1,
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestDaemonAPIConnectorMarketRefreshBindsActiveAccount(t *testing.T) {
	service := stubConnectorMarketService{refreshFn: func(_ context.Context, mutation contracts.Mutation) contracts.CommandResult {
		if mutation.Scope.AccountID != "account-a" || mutation.ClientRequestID != "request-refresh" {
			t.Fatalf("refresh mutation = %#v", mutation)
		}
		operation := contracts.Operation{
			OperationID: "refresh-1", ClientRequestID: mutation.ClientRequestID, Kind: contracts.OperationKindRefreshCatalog,
			Scope: mutation.Scope, State: contracts.OperationStateAccepted, CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(),
		}
		return contracts.CommandResult{Outcome: contracts.CommandAccepted, Operation: &operation}
	}}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPIWithScope(service, func() contracts.OperationScope {
		return contracts.OperationScope{AccountID: "account-a"}
	})))
	recorder := performGeneratedRouteRequest(t, mux, http.MethodPost, "/v1/connector-market:refresh", map[string]any{
		"clientRequestId": "request-refresh", "expectedRevision": 0,
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDaemonAPIConnectorMarketOperationUsesScopedRead(t *testing.T) {
	service := stubConnectorMarketService{operationFn: func(_ context.Context, scope contracts.OperationScope, operationID string) (contracts.Operation, error) {
		if scope.AccountID != "account-b" || operationID != "operation-a" {
			t.Fatalf("operation scope=%#v id=%q", scope, operationID)
		}
		return contracts.Operation{}, contracts.ErrNotFound
	}}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPIWithScope(service, func() contracts.OperationScope {
		return contracts.OperationScope{AccountID: "account-b"}
	})))
	recorder := performGeneratedRouteRequest(t, mux, http.MethodGet, "/v1/connector-market/operations/operation-a", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDaemonAPIConnectorMarketServesCategoriesAndCursorPage(t *testing.T) {
	service := stubConnectorMarketService{
		categoriesFn: func(context.Context) ([]contracts.CatalogCategory, error) {
			return []contracts.CatalogCategory{{
				CategoryID: "developer-tools", Kind: "category", SortOrder: 40, ItemCount: 1,
				DisplayNameZH: "开发者工具", DisplayNameEN: "Developer Tools",
			}}, nil
		},
		pageFn: func(_ context.Context, query contracts.CatalogPageQuery) (contracts.CatalogPage, error) {
			if query.SectionID != "developer-tools" || query.PageSize != 20 || query.PageToken != "cursor-1" ||
				query.InstallationFilter != contracts.CatalogInstallationFilterNotInstalled {
				t.Fatalf("query = %#v", query)
			}
			return contracts.CatalogPage{
				SectionID:     "developer-tools",
				Items:         []contracts.CatalogListing{{CategoryID: "developer-tools", Connector: connectorMarketTestConnector()}},
				NextPageToken: "cursor-2",
				Revision:      8,
			}, nil
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPI(service)))

	categories := performGeneratedRouteRequest(t, mux, http.MethodGet, "/v1/connector-market/categories", nil)
	if categories.Code != http.StatusOK {
		t.Fatalf("categories status = %d; body: %s", categories.Code, categories.Body.String())
	}
	var categoryResponse tuttigenerated.ConnectorMarketCategoriesResponse
	decodeGeneratedRouteResponse(t, categories, &categoryResponse)
	if len(categoryResponse.Categories) != 1 || categoryResponse.Categories[0].CategoryId != "developer-tools" ||
		categoryResponse.Categories[0].DisplayNameZh == nil || *categoryResponse.Categories[0].DisplayNameZh != "开发者工具" ||
		categoryResponse.Categories[0].DisplayNameEn == nil || *categoryResponse.Categories[0].DisplayNameEn != "Developer Tools" {
		t.Fatalf("categories response = %#v", categoryResponse)
	}
	page := performGeneratedRouteRequest(t, mux, http.MethodGet, "/v1/connector-market/catalog?sectionId=developer-tools&installation=not_installed&pageSize=20&pageToken=cursor-1", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("page status = %d; body: %s", page.Code, page.Body.String())
	}
	var response tuttigenerated.ConnectorMarketCatalogPage
	decodeGeneratedRouteResponse(t, page, &response)
	if response.SectionId != "developer-tools" || response.Revision != 8 || len(response.Items) != 1 || response.Items[0].Connector.Key != "notion" {
		t.Fatalf("response = %#v", response)
	}
}

func TestDaemonAPIConnectorMarketEmptyCatalogPageUsesEmptyItemsArray(t *testing.T) {
	service := stubConnectorMarketService{
		pageFn: func(context.Context, contracts.CatalogPageQuery) (contracts.CatalogPage, error) {
			return contracts.CatalogPage{SectionID: "featured", Revision: 8}, nil
		},
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(connectorMarketTestAPI(service)))

	page := performGeneratedRouteRequest(t, mux, http.MethodGet, "/v1/connector-market/catalog?sectionId=featured&pageSize=20", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("page status = %d; body: %s", page.Code, page.Body.String())
	}
	var raw map[string]any
	decodeGeneratedRouteResponse(t, page, &raw)
	items, ok := raw["items"].([]any)
	if !ok || items == nil {
		t.Fatalf("items = %#v, want an empty JSON array", raw["items"])
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v, want empty", items)
	}
}

func connectorMarketTestConnector() contracts.Connector {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return contracts.Connector{
		Key: "notion",
		Release: contracts.Release{
			SchemaVersion:  "1",
			ReleaseID:      "notion@1.0.0",
			ConnectorKey:   "notion",
			Version:        "1.0.0",
			ReleaseDigest:  digest,
			ManifestDigest: digest,
			Manifest: contracts.Manifest{
				IconURL:       "data:image/png;base64,iVBORw0KGgo=",
				SchemaVersion: "1",
				DisplayName:   "Notion",
				AgentRouting:  &contracts.AgentRouting{Aliases: []string{"Notion", "Notion AI"}},
				Permissions:   []string{"pages.read"},
				Implementation: contracts.Implementation{
					Kind: contracts.ImplementationKindManagedStdio,
					ManagedStdio: &contracts.ManagedStdioImplementation{
						Runtime: contracts.RuntimeRequirement{Language: "node", Profile: "connector-node-static", ABI: "node20-darwin-arm64", VersionRange: ">=20.0.0 <21.0.0"},
						CLI: &contracts.ManagedCLIInterface{Entrypoint: "notion", TimeoutMS: 120_000,
							Commands: []contracts.CLICommand{{Name: "run", InputSchema: map[string]any{"type": "object"}, TimeoutMS: 30_000}}},
						CredentialBroker: &contracts.ManagedCredentialBroker{Protocol: contracts.CredentialBrokerProtocolV1,
							Entrypoint: "authorization/broker.mjs", TimeoutMS: 300_000, AllowedHosts: []string{"notion.so"}},
					},
				},
				AuthorizationKind: "oauth2",
			},
			Artifact: contracts.Artifact{
				SHA256:    digest,
				SizeBytes: 128,
				MediaType: "application/gzip",
			},
			PublishedAt: time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC), Status: contracts.ReleaseStatusAvailable,
		},
		Installation:  contracts.Installation{State: contracts.InstallationStateNotInstalled},
		Authorization: contracts.Authorization{State: contracts.AuthorizationStateDisconnected},
		Compatibility: contracts.Compatibility{State: contracts.CompatibilityStateSupported},
		Revision:      7,
	}
}
