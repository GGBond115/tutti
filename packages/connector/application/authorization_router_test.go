package application

import (
	"context"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"testing"
)

type routedAuthorizationProvider struct {
	beginCount      int
	disconnectCount int
	observeCount    int
}

type inspectOnlyAuthorizationProvider struct {
	inspectCount int
	observation  contracts.AuthorizationObservation
}

func (*inspectOnlyAuthorizationProvider) Begin(context.Context, contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	return contracts.AuthorizationSession{}, nil
}

func (*inspectOnlyAuthorizationProvider) Disconnect(context.Context, contracts.AuthorizationDisconnectRequest) error {
	return nil
}

func (provider *inspectOnlyAuthorizationProvider) InspectAuthorization(
	context.Context,
	contracts.AuthorizationInspectRequest,
) (contracts.AuthorizationObservation, error) {
	provider.inspectCount++
	if provider.observation.State == "" {
		return contracts.AuthorizationObservation{State: contracts.AuthorizationObservationConnected}, nil
	}
	return provider.observation, nil
}

func TestImplementationAuthorizationRouterKeepsDisconnectedManagedSessionPending(t *testing.T) {
	managed := &inspectOnlyAuthorizationProvider{observation: contracts.AuthorizationObservation{State: contracts.AuthorizationObservationDisconnected}}
	router := NewImplementationAuthorizationRouter(managed, &routedAuthorizationProvider{})
	release := contracts.Release{Manifest: contracts.Manifest{Implementation: contracts.Implementation{Kind: contracts.ImplementationKindManagedStdio}}}

	observation, err := router.Observe(context.Background(), contracts.AuthorizationObserveRequest{
		Connector: contracts.Connector{Key: "wecom", Release: release}, Release: release,
		Session: contracts.AuthorizationSession{SessionID: "session-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != contracts.AuthorizationObservationPending {
		t.Fatalf("observation state = %q, want pending", observation.State)
	}
}

func TestImplementationAuthorizationRouterFailsExpiredManagedSession(t *testing.T) {
	managed := &inspectOnlyAuthorizationProvider{observation: contracts.AuthorizationObservation{State: contracts.AuthorizationObservationExpired}}
	router := NewImplementationAuthorizationRouter(managed, &routedAuthorizationProvider{})
	release := contracts.Release{Manifest: contracts.Manifest{Implementation: contracts.Implementation{Kind: contracts.ImplementationKindManagedStdio}}}

	observation, err := router.Observe(context.Background(), contracts.AuthorizationObserveRequest{
		Connector: contracts.Connector{Key: "wecom", Release: release}, Release: release,
		Session: contracts.AuthorizationSession{SessionID: "session-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != contracts.AuthorizationObservationFailed || observation.FailureCode != "connector_authorization_expired" {
		t.Fatalf("observation = %#v, want terminal expiry", observation)
	}
}

func (provider *routedAuthorizationProvider) Begin(_ context.Context, request contracts.AuthorizationStartRequest) (contracts.AuthorizationSession, error) {
	provider.beginCount++
	return contracts.AuthorizationSession{OperationID: request.OperationID, ConnectorKey: request.Connector.Key}, nil
}

func (provider *routedAuthorizationProvider) Disconnect(context.Context, contracts.AuthorizationDisconnectRequest) error {
	provider.disconnectCount++
	return nil
}

func (provider *routedAuthorizationProvider) Observe(context.Context, contracts.AuthorizationObserveRequest) (contracts.AuthorizationObservation, error) {
	provider.observeCount++
	return contracts.AuthorizationObservation{State: contracts.AuthorizationObservationConnected}, nil
}

func TestImplementationAuthorizationRouterUsesFrozenReleaseKind(t *testing.T) {
	managed := &routedAuthorizationProvider{}
	remote := &routedAuthorizationProvider{}
	router := NewImplementationAuthorizationRouter(managed, remote)
	remoteRelease := contracts.Release{Manifest: contracts.Manifest{Implementation: contracts.Implementation{Kind: contracts.ImplementationKindRemoteStreamableHTTP}}}
	managedRelease := contracts.Release{Manifest: contracts.Manifest{Implementation: contracts.Implementation{Kind: contracts.ImplementationKindManagedStdio}}}

	if _, err := router.Begin(context.Background(), contracts.AuthorizationStartRequest{
		OperationID: "begin-1", Connector: contracts.Connector{Key: "documents", Release: managedRelease}, Release: remoteRelease,
	}); err != nil {
		t.Fatal(err)
	}
	if err := router.Disconnect(context.Background(), contracts.AuthorizationDisconnectRequest{
		Connector: contracts.Connector{Key: "documents", Release: remoteRelease}, Release: managedRelease,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Observe(context.Background(), contracts.AuthorizationObserveRequest{
		Connector: contracts.Connector{Key: "documents", Release: managedRelease}, Release: remoteRelease,
	}); err != nil {
		t.Fatal(err)
	}
	if managed.beginCount != 0 || managed.disconnectCount != 1 || managed.observeCount != 0 {
		t.Fatalf("managed calls = begin:%d disconnect:%d observe:%d", managed.beginCount, managed.disconnectCount, managed.observeCount)
	}
	if remote.beginCount != 1 || remote.disconnectCount != 0 || remote.observeCount != 1 {
		t.Fatalf("remote calls = begin:%d disconnect:%d observe:%d", remote.beginCount, remote.disconnectCount, remote.observeCount)
	}
}

func TestImplementationAuthorizationRouterRejectsUnsupportedImplementation(t *testing.T) {
	router := NewImplementationAuthorizationRouter(&routedAuthorizationProvider{}, &routedAuthorizationProvider{})
	if _, err := router.Begin(context.Background(), contracts.AuthorizationStartRequest{Release: contracts.Release{}}); err == nil {
		t.Fatal("expected unsupported implementation error")
	}
}

func TestImplementationAuthorizationRouterUsesInspectorToRecoverManagedObservation(t *testing.T) {
	managed := &inspectOnlyAuthorizationProvider{}
	router := NewImplementationAuthorizationRouter(managed, &routedAuthorizationProvider{})
	release := contracts.Release{Manifest: contracts.Manifest{Implementation: contracts.Implementation{Kind: contracts.ImplementationKindManagedStdio}}}
	observation, err := router.Observe(context.Background(), contracts.AuthorizationObserveRequest{
		Connector: contracts.Connector{Key: "lark", Release: release}, Release: release,
		Session: contracts.AuthorizationSession{SessionID: "session-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if managed.inspectCount != 1 || observation.State != contracts.AuthorizationObservationConnected {
		t.Fatalf("managed inspection = %d, observation = %#v", managed.inspectCount, observation)
	}
}
