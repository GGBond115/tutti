package application

import (
	"context"
	"errors"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
)

// ImplementationAuthorizationRouter selects the authorization owner from the
// exact release frozen into an operation. Managed stdio implementations keep
// authorization with their local implementation host; remote HTTP
// implementations use the account control plane supplied by the product host.
type ImplementationAuthorizationRouter struct {
	managed AuthorizationProvider
	remote  AuthorizationProvider
}

func NewImplementationAuthorizationRouter(
	managed AuthorizationProvider,
	remote AuthorizationProvider,
) *ImplementationAuthorizationRouter {
	return &ImplementationAuthorizationRouter{managed: managed, remote: remote}
}

func (router *ImplementationAuthorizationRouter) provider(release contracts.Release) (AuthorizationProvider, error) {
	if router == nil {
		return nil, errors.New("connector authorization router is unavailable")
	}
	var provider AuthorizationProvider
	switch release.Manifest.Implementation.Kind {
	case contracts.ImplementationKindManagedStdio:
		provider = router.managed
	case contracts.ImplementationKindRemoteStreamableHTTP:
		provider = router.remote
	default:
		return nil, errors.New("connector authorization implementation is unsupported")
	}
	if provider == nil {
		return nil, errors.New("connector authorization provider is unavailable")
	}
	return provider, nil
}

func (router *ImplementationAuthorizationRouter) Begin(
	ctx context.Context,
	request contracts.AuthorizationStartRequest,
) (contracts.AuthorizationSession, error) {
	provider, err := router.provider(request.Release)
	if err != nil {
		return contracts.AuthorizationSession{}, err
	}
	return provider.Begin(ctx, request)
}

func (router *ImplementationAuthorizationRouter) Disconnect(
	ctx context.Context,
	request contracts.AuthorizationDisconnectRequest,
) error {
	provider, err := router.provider(request.Release)
	if err != nil {
		return err
	}
	return provider.Disconnect(ctx, request)
}

func (router *ImplementationAuthorizationRouter) Cancel(
	ctx context.Context,
	request contracts.AuthorizationCancelRequest,
) error {
	provider, err := router.provider(request.Release)
	if err != nil {
		return err
	}
	canceler, ok := provider.(AuthorizationAttemptCanceler)
	if !ok {
		return errors.New("connector authorization provider does not support attempt cancellation")
	}
	return canceler.Cancel(ctx, request)
}

func (router *ImplementationAuthorizationRouter) Observe(
	ctx context.Context,
	request contracts.AuthorizationObserveRequest,
) (contracts.AuthorizationObservation, error) {
	provider, err := router.provider(request.Release)
	if err != nil {
		return contracts.AuthorizationObservation{}, err
	}
	observer, ok := provider.(AuthorizationObserver)
	if !ok {
		if request.Release.Manifest.Implementation.Kind == contracts.ImplementationKindManagedStdio {
			inspector, inspectOK := provider.(AuthorizationInspector)
			if !inspectOK {
				return contracts.AuthorizationObservation{}, errors.New("connector authorization inspector is unavailable")
			}
			connector := request.Connector
			connector.Release = request.Release
			observation, inspectErr := inspector.InspectAuthorization(ctx, contracts.AuthorizationInspectRequest{
				Scope: request.Scope, Connector: connector,
				AuthorizationSessionID: request.Session.SessionID,
			})
			if inspectErr != nil {
				return contracts.AuthorizationObservation{}, inspectErr
			}
			// Inspect reports durable credential state, while Observe reconciles an
			// unresolved authorization attempt. A disconnected credential is still
			// pending until that attempt expires.
			switch observation.State {
			case contracts.AuthorizationObservationDisconnected:
				observation.State = contracts.AuthorizationObservationPending
			case contracts.AuthorizationObservationExpired:
				observation.State = contracts.AuthorizationObservationFailed
				if observation.FailureCode == "" {
					observation.FailureCode = "connector_authorization_expired"
				}
			}
			return observation, nil
		}
		return contracts.AuthorizationObservation{}, errors.New("connector authorization observer is unavailable")
	}
	return observer.Observe(ctx, request)
}

func (router *ImplementationAuthorizationRouter) InspectAuthorization(
	ctx context.Context,
	request contracts.AuthorizationInspectRequest,
) (contracts.AuthorizationObservation, error) {
	provider, err := router.provider(request.Connector.Release)
	if err != nil {
		return contracts.AuthorizationObservation{}, err
	}
	inspector, ok := provider.(AuthorizationInspector)
	if !ok {
		return contracts.AuthorizationObservation{}, errors.New("connector authorization inspector is unavailable")
	}
	return inspector.InspectAuthorization(ctx, request)
}

var _ AuthorizationProvider = (*ImplementationAuthorizationRouter)(nil)
var _ AuthorizationAttemptCanceler = (*ImplementationAuthorizationRouter)(nil)
var _ AuthorizationObserver = (*ImplementationAuthorizationRouter)(nil)
var _ AuthorizationInspector = (*ImplementationAuthorizationRouter)(nil)
