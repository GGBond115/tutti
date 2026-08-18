package api

import (
	"context"
	"encoding/json"
	"errors"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"time"
)

func (api DaemonAPI) connectorMarketAvailable() bool {
	return api.ConnectorCatalogQueries != nil &&
		api.ConnectorCatalogCommands != nil &&
		api.ConnectorInstallationCommands != nil &&
		api.ConnectorRuntimeCommands != nil &&
		api.ConnectorAuthorizationCommands != nil &&
		api.ConnectorOperationQueries != nil
}

func (api DaemonAPI) GetConnectorMarket(
	ctx context.Context,
	_ tuttigenerated.GetConnectorMarketRequestObject,
) (tuttigenerated.GetConnectorMarketResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.GetConnectorMarket503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	snapshot, err := api.ConnectorCatalogQueries.SnapshotViewForScope(
		ctx,
		contracts.OperationScope{AccountID: api.connectorMarketAccountID()},
	)
	if err != nil {
		return connectorMarketGetSnapshotError(err), nil
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketSnapshot](snapshot)
	if err != nil {
		return nil, err
	}
	return tuttigenerated.GetConnectorMarket200JSONResponse(projected), nil
}

func (api DaemonAPI) ListConnectorMarketCategories(
	ctx context.Context,
	_ tuttigenerated.ListConnectorMarketCategoriesRequestObject,
) (tuttigenerated.ListConnectorMarketCategoriesResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.ListConnectorMarketCategories503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	categories, err := api.ConnectorCatalogQueries.ListCatalogCategories(ctx)
	if err != nil {
		payload, _ := connectorMarketError(err)
		return tuttigenerated.ListConnectorMarketCategories503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: unavailableConnectorMarketResponse(payload)}, nil
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketCategoriesResponse](struct {
		Categories []contracts.CatalogCategory `json:"categories"`
	}{Categories: categories})
	if err != nil {
		return nil, err
	}
	return tuttigenerated.ListConnectorMarketCategories200JSONResponse(projected), nil
}

func (api DaemonAPI) ListConnectorMarketCatalog(
	ctx context.Context,
	request tuttigenerated.ListConnectorMarketCatalogRequestObject,
) (tuttigenerated.ListConnectorMarketCatalogResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.ListConnectorMarketCatalog503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	pageSize := 20
	if request.Params.PageSize != nil {
		pageSize = *request.Params.PageSize
	}
	pageToken := ""
	if request.Params.PageToken != nil {
		pageToken = *request.Params.PageToken
	}
	installationFilter := contracts.CatalogInstallationFilter("")
	if request.Params.Installation != nil {
		installationFilter = contracts.CatalogInstallationFilter(*request.Params.Installation)
	}
	page, err := api.ConnectorCatalogQueries.ListCatalogPageViewForScope(ctx,
		contracts.OperationScope{AccountID: api.connectorMarketAccountID()}, contracts.CatalogPageQuery{
			SectionID: request.Params.SectionId, PageSize: pageSize, PageToken: pageToken, InstallationFilter: installationFilter,
		})
	if err != nil {
		payload, status := connectorMarketError(err)
		if status == 400 {
			return tuttigenerated.ListConnectorMarketCatalog400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(payload)}, nil
		}
		return tuttigenerated.ListConnectorMarketCatalog503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: unavailableConnectorMarketResponse(payload)}, nil
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketCatalogPage](page)
	if err != nil {
		return nil, err
	}
	return tuttigenerated.ListConnectorMarketCatalog200JSONResponse(projected), nil
}

func (api DaemonAPI) GetConnectorMarketConnector(
	ctx context.Context,
	request tuttigenerated.GetConnectorMarketConnectorRequestObject,
) (tuttigenerated.GetConnectorMarketConnectorResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.GetConnectorMarketConnector503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	connector, err := api.ConnectorCatalogQueries.GetConnectorViewForScope(ctx,
		contracts.OperationScope{AccountID: api.connectorMarketAccountID()}, request.ConnectorKey)
	if err != nil {
		payload, status := connectorMarketError(err)
		switch status {
		case 400:
			return tuttigenerated.GetConnectorMarketConnector400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(payload)}, nil
		case 404:
			return tuttigenerated.GetConnectorMarketConnector404JSONResponse{ConnectorMarketNotFoundErrorJSONResponse: notFoundConnectorMarketResponse(payload)}, nil
		default:
			return tuttigenerated.GetConnectorMarketConnector503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: unavailableConnectorMarketResponse(payload)}, nil
		}
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketConnector](connector)
	if err != nil {
		return nil, err
	}
	return tuttigenerated.GetConnectorMarketConnector200JSONResponse(projected), nil
}

func (api DaemonAPI) RefreshConnectorMarket(
	ctx context.Context,
	request tuttigenerated.RefreshConnectorMarketRequestObject,
) (tuttigenerated.RefreshConnectorMarketResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.RefreshConnectorMarket503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	mutation, err := connectorMarketMutation(request.Body)
	if err != nil {
		return tuttigenerated.RefreshConnectorMarket400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(connectorMarketErrorPayload(err))}, nil
	}
	mutation.Scope = contracts.OperationScope{AccountID: api.connectorMarketAccountID()}
	result := api.ConnectorCatalogCommands.RefreshCatalog(ctx, mutation)
	view, err := api.connectorMarketCommandResultView(ctx, result)
	if err != nil {
		return nil, err
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketMutationResponse](view)
	if err != nil {
		return nil, err
	}
	if result.Outcome != contracts.CommandAccepted {
		return tuttigenerated.RefreshConnectorMarket200JSONResponse(projected), nil
	}
	return tuttigenerated.RefreshConnectorMarket202JSONResponse(projected), nil
}

func (api DaemonAPI) InstallConnectorMarketConnector(
	ctx context.Context,
	request tuttigenerated.InstallConnectorMarketConnectorRequestObject,
) (tuttigenerated.InstallConnectorMarketConnectorResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.InstallConnectorMarketConnector503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	mutation, err := connectorMarketConnectorMutation(request.ConnectorKey, request.Body)
	if err != nil {
		return tuttigenerated.InstallConnectorMarketConnector400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(connectorMarketErrorPayload(err))}, nil
	}
	mutation.AccountID = api.connectorMarketAccountID()
	result := api.ConnectorInstallationCommands.Install(ctx, mutation)
	view, err := api.connectorMarketCommandResultView(ctx, result)
	if err != nil {
		return nil, err
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketMutationResponse](view)
	if err != nil {
		return nil, err
	}
	if result.Outcome != contracts.CommandAccepted {
		return tuttigenerated.InstallConnectorMarketConnector200JSONResponse(projected), nil
	}
	return tuttigenerated.InstallConnectorMarketConnector202JSONResponse(projected), nil
}

func (api DaemonAPI) UninstallConnectorMarketConnector(
	ctx context.Context,
	request tuttigenerated.UninstallConnectorMarketConnectorRequestObject,
) (tuttigenerated.UninstallConnectorMarketConnectorResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.UninstallConnectorMarketConnector503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	mutation, err := connectorMarketConnectorMutation(request.ConnectorKey, request.Body)
	if err != nil {
		return tuttigenerated.UninstallConnectorMarketConnector400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(connectorMarketErrorPayload(err))}, nil
	}
	mutation.AccountID = api.connectorMarketAccountID()
	result := api.ConnectorInstallationCommands.Uninstall(ctx, mutation)
	view, err := api.connectorMarketCommandResultView(ctx, result)
	if err != nil {
		return nil, err
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketMutationResponse](view)
	if err != nil {
		return nil, err
	}
	if result.Outcome != contracts.CommandAccepted {
		return tuttigenerated.UninstallConnectorMarketConnector200JSONResponse(projected), nil
	}
	return tuttigenerated.UninstallConnectorMarketConnector202JSONResponse(projected), nil
}

func (api DaemonAPI) RestartConnectorMarketRuntime(
	ctx context.Context,
	request tuttigenerated.RestartConnectorMarketRuntimeRequestObject,
) (tuttigenerated.RestartConnectorMarketRuntimeResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.RestartConnectorMarketRuntime503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	mutation, err := connectorMarketConnectorMutation(request.ConnectorKey, request.Body)
	if err != nil {
		return tuttigenerated.RestartConnectorMarketRuntime400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(connectorMarketErrorPayload(err))}, nil
	}
	mutation.AccountID = api.connectorMarketAccountID()
	result := api.ConnectorRuntimeCommands.RestartRuntime(ctx, mutation)
	view, err := api.connectorMarketCommandResultView(ctx, result)
	if err != nil {
		return nil, err
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketMutationResponse](view)
	if err != nil {
		return nil, err
	}
	if result.Outcome != contracts.CommandAccepted {
		return tuttigenerated.RestartConnectorMarketRuntime200JSONResponse(projected), nil
	}
	return tuttigenerated.RestartConnectorMarketRuntime202JSONResponse(projected), nil
}

func (api DaemonAPI) StartConnectorMarketAuthorization(
	ctx context.Context,
	request tuttigenerated.StartConnectorMarketAuthorizationRequestObject,
) (tuttigenerated.StartConnectorMarketAuthorizationResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.StartConnectorMarketAuthorization503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	mutation, secret, err := connectorMarketAuthorizationMutation(request.ConnectorKey, request.Body)
	if err != nil {
		return tuttigenerated.StartConnectorMarketAuthorization400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(connectorMarketErrorPayload(err))}, nil
	}
	mutation.AccountID = api.connectorMarketAccountID()
	defer clear(secret)
	result := api.ConnectorAuthorizationCommands.BeginAuthorization(ctx, mutation, secret)
	view, err := api.connectorMarketAuthorizationCommandResultView(ctx, result)
	if err != nil {
		return nil, err
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketAuthorizationResponse](view)
	if err != nil {
		return nil, err
	}
	return tuttigenerated.StartConnectorMarketAuthorization200JSONResponse(projected), nil
}

func (api DaemonAPI) CancelConnectorMarketAuthorization(
	ctx context.Context,
	request tuttigenerated.CancelConnectorMarketAuthorizationRequestObject,
) (tuttigenerated.CancelConnectorMarketAuthorizationResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.CancelConnectorMarketAuthorization503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	command, err := connectorMarketCancelAuthorizationCommand(string(request.ConnectorKey), request.Body)
	if err != nil {
		return tuttigenerated.CancelConnectorMarketAuthorization400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(connectorMarketErrorPayload(err))}, nil
	}
	command.AccountID = api.connectorMarketAccountID()
	result := api.ConnectorAuthorizationCommands.CancelAuthorization(ctx, command)
	view, err := api.connectorMarketCommandResultView(ctx, result)
	if err != nil {
		return nil, err
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketMutationResponse](view)
	if err != nil {
		return nil, err
	}
	return tuttigenerated.CancelConnectorMarketAuthorization200JSONResponse(projected), nil
}

func (api DaemonAPI) DisconnectConnectorMarketAuthorization(
	ctx context.Context,
	request tuttigenerated.DisconnectConnectorMarketAuthorizationRequestObject,
) (tuttigenerated.DisconnectConnectorMarketAuthorizationResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.DisconnectConnectorMarketAuthorization503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	mutation, err := connectorMarketConnectorMutation(request.ConnectorKey, request.Body)
	if err != nil {
		return tuttigenerated.DisconnectConnectorMarketAuthorization400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(connectorMarketErrorPayload(err))}, nil
	}
	mutation.AccountID = api.connectorMarketAccountID()
	result := api.ConnectorAuthorizationCommands.DisconnectAuthorization(ctx, mutation)
	view, err := api.connectorMarketCommandResultView(ctx, result)
	if err != nil {
		return nil, err
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketMutationResponse](view)
	if err != nil {
		return nil, err
	}
	if result.Outcome != contracts.CommandAccepted {
		return tuttigenerated.DisconnectConnectorMarketAuthorization200JSONResponse(projected), nil
	}
	return tuttigenerated.DisconnectConnectorMarketAuthorization202JSONResponse(projected), nil
}

func (api DaemonAPI) GetConnectorMarketOperation(
	ctx context.Context,
	request tuttigenerated.GetConnectorMarketOperationRequestObject,
) (tuttigenerated.GetConnectorMarketOperationResponseObject, error) {
	if !api.connectorMarketAvailable() {
		return tuttigenerated.GetConnectorMarketOperation503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: connectorMarketUnavailableError()}, nil
	}
	operation, err := api.ConnectorOperationQueries.GetOperationForScope(
		ctx,
		contracts.OperationScope{AccountID: api.connectorMarketAccountID()},
		request.OperationID,
	)
	if err != nil {
		payload, status := connectorMarketError(err)
		switch status {
		case 400:
			return tuttigenerated.GetConnectorMarketOperation400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(payload)}, nil
		case 404:
			return tuttigenerated.GetConnectorMarketOperation404JSONResponse{ConnectorMarketNotFoundErrorJSONResponse: notFoundConnectorMarketResponse(payload)}, nil
		default:
			return tuttigenerated.GetConnectorMarketOperation503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: unavailableConnectorMarketResponse(payload)}, nil
		}
	}
	projected, err := projectConnectorMarket[tuttigenerated.ConnectorMarketOperation](operation)
	if err != nil {
		return nil, err
	}
	return tuttigenerated.GetConnectorMarketOperation200JSONResponse(projected), nil
}

func connectorMarketMutation(body *tuttigenerated.ConnectorMarketMutationRequest) (contracts.Mutation, error) {
	if body == nil || body.ExpectedRevision < 0 {
		return contracts.Mutation{}, invalidConnectorMarketRequest()
	}
	return contracts.Mutation{ClientRequestID: body.ClientRequestId, ExpectedRevision: uint64(body.ExpectedRevision)}, nil
}

func connectorMarketConnectorMutation(
	connectorKey string,
	body *tuttigenerated.ConnectorMarketMutationRequest,
) (contracts.ConnectorMutation, error) {
	mutation, err := connectorMarketMutation(body)
	if err != nil {
		return contracts.ConnectorMutation{}, err
	}
	if body.ExpectedConnectorRevision != nil && *body.ExpectedConnectorRevision < 0 {
		return contracts.ConnectorMutation{}, invalidConnectorMarketRequest()
	}
	result := contracts.ConnectorMutation{Mutation: mutation, ConnectorKey: connectorKey}
	if body.ExpectedConnectorRevision != nil {
		revision := uint64(*body.ExpectedConnectorRevision)
		result.ExpectedConnectorRevision = &revision
	}
	return result, nil
}

func connectorMarketAuthorizationMutation(
	connectorKey string,
	body *tuttigenerated.ConnectorMarketAuthorizationRequest,
) (contracts.ConnectorMutation, []byte, error) {
	if body == nil || body.ExpectedRevision < 0 ||
		(body.ExpectedConnectorRevision != nil && *body.ExpectedConnectorRevision < 0) {
		return contracts.ConnectorMutation{}, nil, invalidConnectorMarketRequest()
	}
	var secret []byte
	if body.Secret != nil {
		secret = []byte(*body.Secret)
		if len(secret) == 0 || len(secret) > 16384 {
			clear(secret)
			return contracts.ConnectorMutation{}, nil, invalidConnectorMarketRequest()
		}
	}
	result := contracts.ConnectorMutation{
		Mutation:     contracts.Mutation{ClientRequestID: body.ClientRequestId, ExpectedRevision: uint64(body.ExpectedRevision)},
		ConnectorKey: connectorKey,
	}
	if body.ReplacementPolicy != nil {
		result.ReplacementPolicy = contracts.AuthorizationReplacementPolicy(*body.ReplacementPolicy)
	}
	if body.ExpectedConnectorRevision != nil {
		revision := uint64(*body.ExpectedConnectorRevision)
		result.ExpectedConnectorRevision = &revision
	}
	return result, secret, nil
}

func connectorMarketCancelAuthorizationCommand(
	connectorKey string,
	body *tuttigenerated.ConnectorMarketAuthorizationCancelRequest,
) (contracts.CancelAuthorizationCommand, error) {
	if body == nil || body.ExpectedRevision < 0 || body.ExpectedConnectorRevision < 0 {
		return contracts.CancelAuthorizationCommand{}, invalidConnectorMarketRequest()
	}
	connectorRevision := uint64(body.ExpectedConnectorRevision)
	return contracts.CancelAuthorizationCommand{
		ConnectorMutation: contracts.ConnectorMutation{
			Mutation: contracts.Mutation{
				ClientRequestID:  body.ClientRequestId,
				ExpectedRevision: uint64(body.ExpectedRevision),
			},
			ConnectorKey:              connectorKey,
			ExpectedConnectorRevision: &connectorRevision,
		},
		OperationID: body.OperationId,
	}, nil
}

func invalidConnectorMarketRequest() error {
	return contracts.NewDomainError(contracts.ErrorCodeInvalidRequest, "connector market request is invalid", false, nil)
}

func (api DaemonAPI) connectorMarketAccountID() string {
	if api.ConnectorMarketScope == nil {
		return ""
	}
	return api.ConnectorMarketScope().AccountID
}

type connectorMarketCommandView struct {
	Outcome   contracts.CommandOutcome  `json:"outcome"`
	Revision  uint64                    `json:"revision"`
	Connector *contracts.ConnectorView  `json:"connector,omitempty"`
	Operation *contracts.Operation      `json:"operation,omitempty"`
	Failure   *contracts.CommandFailure `json:"failure,omitempty"`
}

type connectorMarketAuthorizationCommandView struct {
	connectorMarketCommandView
	AuthorizationURL       string                               `json:"authorizationUrl,omitempty"`
	AuthorizationView      *contracts.AuthorizationViewEnvelope `json:"authorizationView,omitempty"`
	AuthorizationExpiresAt *time.Time                           `json:"authorizationExpiresAt,omitempty"`
}

func (api DaemonAPI) connectorMarketCommandResultView(
	ctx context.Context,
	result contracts.CommandResult,
) (connectorMarketCommandView, error) {
	if err := result.Validate(); err != nil {
		return connectorMarketCommandView{}, err
	}
	view := connectorMarketCommandView{
		Outcome: result.Outcome, Revision: result.Revision, Operation: result.Operation, Failure: result.Failure,
	}
	if result.Connector == nil {
		return view, nil
	}
	connector, err := api.ConnectorCatalogQueries.PresentConnectorForScope(
		ctx, contracts.OperationScope{AccountID: api.connectorMarketAccountID()}, *result.Connector,
	)
	if err != nil {
		return connectorMarketCommandView{}, err
	}
	view.Connector = &connector
	return view, nil
}

func (api DaemonAPI) connectorMarketAuthorizationCommandResultView(
	ctx context.Context,
	result contracts.AuthorizationCommandResult,
) (connectorMarketAuthorizationCommandView, error) {
	view, err := api.connectorMarketCommandResultView(ctx, result.CommandResult)
	if err != nil {
		return connectorMarketAuthorizationCommandView{}, err
	}
	return connectorMarketAuthorizationCommandView{
		connectorMarketCommandView: view,
		AuthorizationURL:           result.AuthorizationURL, AuthorizationView: result.AuthorizationView,
		AuthorizationExpiresAt: result.AuthorizationExpiresAt,
	}, nil
}

func projectConnectorMarket[T any](value any) (T, error) {
	var projected T
	if err := validateConnectorMarketCommandResult(value); err != nil {
		return projected, err
	}
	value = exposeConnectorMarketAuthorizationInteractionMode(value)
	payload, err := json.Marshal(value)
	if err != nil {
		return projected, err
	}
	if err := json.Unmarshal(payload, &projected); err != nil {
		return projected, err
	}
	return projected, nil
}

func validateConnectorMarketCommandResult(value any) error {
	switch typed := value.(type) {
	case contracts.CommandResult:
		return typed.Validate()
	case contracts.AuthorizationCommandResult:
		return typed.Validate()
	default:
		return nil
	}
}

func exposeConnectorMarketAuthorizationInteractionMode(value any) any {
	switch typed := value.(type) {
	case contracts.SnapshotView:
		typed.Connectors = append([]contracts.ConnectorView{}, typed.Connectors...)
		for index := range typed.Connectors {
			typed.Connectors[index].Connector = exposeConnectorAuthorizationInteractionMode(typed.Connectors[index].Connector)
		}
		return struct {
			contracts.SnapshotView
			CatalogState   string `json:"catalogState"`
			SourceRevision string `json:"sourceRevision,omitempty"`
		}{
			SnapshotView: typed, CatalogState: legacyConnectorCatalogState(typed.CatalogFreshness.State),
			SourceRevision: typed.CatalogFreshness.SourceRevision,
		}
	case contracts.Snapshot:
		typed.Connectors = append([]contracts.Connector{}, typed.Connectors...)
		for index := range typed.Connectors {
			typed.Connectors[index] = exposeConnectorAuthorizationInteractionMode(typed.Connectors[index])
		}
		return struct {
			contracts.Snapshot
			CatalogState   string `json:"catalogState"`
			SourceRevision string `json:"sourceRevision,omitempty"`
		}{
			Snapshot: typed, CatalogState: legacyConnectorCatalogState(typed.CatalogFreshness.State),
			SourceRevision: typed.CatalogFreshness.SourceRevision,
		}
	case contracts.CatalogPage:
		typed.Items = append([]contracts.CatalogListing{}, typed.Items...)
		for index := range typed.Items {
			typed.Items[index].Connector = exposeConnectorAuthorizationInteractionMode(typed.Items[index].Connector)
		}
		return typed
	case contracts.CatalogPageView:
		typed.Items = append([]contracts.CatalogListingView{}, typed.Items...)
		for index := range typed.Items {
			typed.Items[index].Connector.Connector = exposeConnectorAuthorizationInteractionMode(typed.Items[index].Connector.Connector)
		}
		return typed
	case contracts.ConnectorView:
		typed.Connector = exposeConnectorAuthorizationInteractionMode(typed.Connector)
		return typed
	case connectorMarketCommandView:
		if typed.Connector != nil {
			connector := *typed.Connector
			connector.Connector = exposeConnectorAuthorizationInteractionMode(connector.Connector)
			typed.Connector = &connector
		}
		return typed
	case connectorMarketAuthorizationCommandView:
		if typed.Connector != nil {
			connector := *typed.Connector
			connector.Connector = exposeConnectorAuthorizationInteractionMode(connector.Connector)
			typed.Connector = &connector
		}
		return typed
	case contracts.Connector:
		return exposeConnectorAuthorizationInteractionMode(typed)
	case contracts.MutationResult:
		if typed.Connector != nil {
			connector := exposeConnectorAuthorizationInteractionMode(*typed.Connector)
			typed.Connector = &connector
		}
		return typed
	case contracts.AuthorizationResult:
		typed.Connector = exposeConnectorAuthorizationInteractionMode(typed.Connector)
		return typed
	case contracts.CommandResult:
		if typed.Connector != nil {
			connector := exposeConnectorAuthorizationInteractionMode(*typed.Connector)
			typed.Connector = &connector
		}
		return typed
	case contracts.AuthorizationCommandResult:
		if typed.Connector != nil {
			connector := exposeConnectorAuthorizationInteractionMode(*typed.Connector)
			typed.Connector = &connector
		}
		return typed
	default:
		return value
	}
}

func legacyConnectorCatalogState(state contracts.CatalogFreshnessState) string {
	switch state {
	case contracts.CatalogFreshnessFresh:
		return "ready"
	case contracts.CatalogFreshnessRefreshing:
		return "refreshing"
	case contracts.CatalogFreshnessStale:
		return "stale"
	default:
		return "failed"
	}
}

func exposeConnectorAuthorizationInteractionMode(connector contracts.Connector) contracts.Connector {
	managed := connector.Release.Manifest.Implementation.ManagedStdio
	if managed != nil && managed.CredentialBroker != nil {
		connector.Release.Manifest.AuthorizationInteractionMode = contracts.AuthorizationInteractionModeManaged
	}
	return connector
}

func connectorMarketGetSnapshotError(err error) tuttigenerated.GetConnectorMarketResponseObject {
	payload, status := connectorMarketError(err)
	if status == 400 {
		return tuttigenerated.GetConnectorMarket400JSONResponse{ConnectorMarketInvalidRequestErrorJSONResponse: invalidConnectorMarketResponse(payload)}
	}
	return tuttigenerated.GetConnectorMarket503JSONResponse{ConnectorMarketUnavailableErrorJSONResponse: unavailableConnectorMarketResponse(payload)}
}

func connectorMarketError(err error) (tuttigenerated.ConnectorMarketError, int) {
	payload := connectorMarketErrorPayload(err)
	if errors.Is(err, contracts.ErrNotFound) {
		payload.Code = tuttigenerated.ConnectorNotFound
		payload.Message = "connector market resource was not found"
		return payload, 404
	}
	var domainError *contracts.DomainError
	if !errors.As(err, &domainError) {
		return payload, 503
	}
	switch domainError.Code {
	case contracts.ErrorCodeInvalidRequest:
		return payload, 400
	case contracts.ErrorCodeNotFound:
		return payload, 404
	case contracts.ErrorCodeRevisionConflict, contracts.ErrorCodeOperationInProgress:
		return payload, 409
	case contracts.ErrorCodeIncompatible, contracts.ErrorCodeInvalidManifest, contracts.ErrorCodeUnsupportedImplementation:
		return payload, 422
	default:
		return payload, 503
	}
}

func connectorMarketErrorPayload(err error) tuttigenerated.ConnectorMarketError {
	result := tuttigenerated.ConnectorMarketError{
		Code:      tuttigenerated.ConnectorMarketUnavailable,
		Message:   "connector market is temporarily unavailable",
		Retryable: true,
	}
	var domainError *contracts.DomainError
	if errors.As(err, &domainError) {
		result.Code = tuttigenerated.ConnectorMarketErrorCode(domainError.Code)
		result.Message = domainError.Message
		result.Retryable = domainError.Retryable
	}
	return result
}

func connectorMarketUnavailableError() tuttigenerated.ConnectorMarketUnavailableErrorJSONResponse {
	return unavailableConnectorMarketResponse(tuttigenerated.ConnectorMarketError{
		Code: tuttigenerated.ConnectorMarketUnavailable, Message: "connector market is unavailable", Retryable: true,
	})
}

func invalidConnectorMarketResponse(payload tuttigenerated.ConnectorMarketError) tuttigenerated.ConnectorMarketInvalidRequestErrorJSONResponse {
	return tuttigenerated.ConnectorMarketInvalidRequestErrorJSONResponse(payload)
}

func notFoundConnectorMarketResponse(payload tuttigenerated.ConnectorMarketError) tuttigenerated.ConnectorMarketNotFoundErrorJSONResponse {
	return tuttigenerated.ConnectorMarketNotFoundErrorJSONResponse(payload)
}

func unavailableConnectorMarketResponse(payload tuttigenerated.ConnectorMarketError) tuttigenerated.ConnectorMarketUnavailableErrorJSONResponse {
	return tuttigenerated.ConnectorMarketUnavailableErrorJSONResponse(payload)
}
