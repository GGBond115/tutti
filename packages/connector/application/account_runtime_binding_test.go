package application

import (
	"context"
	contracts "github.com/tutti-os/tutti/packages/connector/contracts"
	"testing"
)

func TestAccountRuntimeBindingResolverKeepsAuthorizedConnectorInactiveWithoutProjection(t *testing.T) {
	release := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	release.Manifest.AuthorizationKind = "oauth2"
	resolver := AccountRuntimeBindingResolver{}
	binding, err := resolver.ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Scope: contracts.OperationScope{AccountID: "account-1"}, Connector: contracts.Connector{Key: "github"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Enabled || binding.ConnectionID != contracts.AccountRuntimeConnectionID("account-1", "github") {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestAccountRuntimeBindingResolverKeepsAuthorizedConnectorInactiveWhileSignedOut(t *testing.T) {
	release := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	release.Manifest.AuthorizationKind = "oauth2"
	binding, err := (AccountRuntimeBindingResolver{}).ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Connector: contracts.Connector{Key: "github"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Enabled || binding.ConnectionID != contracts.AccountRuntimeConnectionID("signed-out", "github") {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestAccountRuntimeBindingResolverIssuesGrantOnlyForConnectedProjection(t *testing.T) {
	release := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	release.Manifest.AuthorizationKind = "oauth2"
	release.Manifest.Implementation.ManagedStdio.CredentialBroker = nil
	projections := &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: "github", ConnectionID: "server-connection", State: contracts.AuthorizationStateConnected,
	}}
	credentials := &credentialGrantIssuerStub{grant: []byte("grant")}
	resolver := AccountRuntimeBindingResolver{Projections: projections, Credentials: credentials}
	binding, err := resolver.ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Scope: contracts.OperationScope{AccountID: "account-1"}, Connector: contracts.Connector{Key: "github"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Enabled || binding.ConnectionID != "server-connection" || string(binding.CredentialBrokerGrant) != "grant" || credentials.calls != 1 {
		t.Fatalf("binding = %#v, credential calls = %d", binding, credentials.calls)
	}
	projections.projection.State = contracts.AuthorizationStateExpired
	binding, err = resolver.ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Scope: contracts.OperationScope{AccountID: "account-1"}, Connector: contracts.Connector{Key: "github"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Enabled || len(binding.CredentialBrokerGrant) != 0 || credentials.calls != 1 {
		t.Fatalf("expired binding = %#v, credential calls = %d", binding, credentials.calls)
	}
	projections.projection.State = contracts.AuthorizationStateConnected
	binding, err = resolver.ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Scope: contracts.OperationScope{AccountID: "account-1"}, Purpose: contracts.RuntimeBindingPurposeDeactivate,
		Connector: contracts.Connector{Key: "github"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Enabled || len(binding.CredentialBrokerGrant) != 0 || credentials.calls != 1 {
		t.Fatalf("deactivation binding = %#v, credential calls = %d", binding, credentials.calls)
	}
}

func TestAccountRuntimeIntentResolverNeverIssuesCredentialGrant(t *testing.T) {
	release := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	release.Manifest.AuthorizationKind = "oauth2"
	release.Manifest.Implementation.ManagedStdio.CredentialBroker = nil
	projections := &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: "github", ConnectionID: "server-connection", State: contracts.AuthorizationStateConnected,
	}}
	credentials := &credentialGrantIssuerStub{grant: []byte("must-not-be-issued")}
	resolver := AccountRuntimeBindingResolver{Projections: projections, Credentials: credentials}
	binding, err := resolver.ResolveRuntimeIntent(context.Background(), contracts.RuntimeBindingRequest{
		Scope: contracts.OperationScope{AccountID: "account-1"}, Connector: contracts.Connector{Key: "github"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Enabled || binding.ConnectionID != "server-connection" || credentials.calls != 0 {
		t.Fatalf("binding = %#v, credential calls = %d", binding, credentials.calls)
	}
}

func TestAccountRuntimeBindingResolverUsesConnectorOwnedCredentialBrokerWithoutServerGrant(t *testing.T) {
	release := testReleaseWithImplementation("lark-cli", "1.0.0", contracts.ImplementationKindManagedStdio)
	release.Manifest.AuthorizationKind = "oauth2"
	release.Manifest.Implementation.ManagedStdio.CredentialBroker = &contracts.ManagedCredentialBroker{
		Protocol: contracts.CredentialBrokerProtocolV1, Entrypoint: "credential-broker.mjs", TimeoutMS: 30_000,
	}
	projections := &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: "lark-cli", State: contracts.AuthorizationStateConnected,
	}}
	binding, err := (AccountRuntimeBindingResolver{Projections: projections}).ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Scope: contracts.OperationScope{AccountID: "account-1"}, Connector: contracts.Connector{Key: "lark-cli"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Enabled || binding.ConnectionID != contracts.AccountRuntimeConnectionID("account-1", "lark-cli") || len(binding.CredentialBrokerGrant) != 0 {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestAccountRuntimeBindingResolverUsesServerConnectionForRemoteMCPWithoutGrant(t *testing.T) {
	release := testReleaseWithImplementation("tencent-docs", "1.0.0", contracts.ImplementationKindRemoteStreamableHTTP)
	release.Manifest.Implementation = contracts.Implementation{
		Kind: contracts.ImplementationKindRemoteStreamableHTTP,
		RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
			ProtocolVersion:     "2026-07-28",
			BindingRef:          "tencent-docs.primary",
			ContractVersion:     1,
			BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	release.Manifest.AuthorizationKind = "api_key"
	projections := &authorizationProjectionStoreStub{projection: contracts.AuthorizationProjection{
		AccountID: "account-1", ConnectorKey: "tencent-docs", ConnectionID: "server-connection", State: contracts.AuthorizationStateConnected,
		ServerSynchronized: true,
	}}
	binding, err := (AccountRuntimeBindingResolver{Projections: projections}).ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Scope: contracts.OperationScope{AccountID: "account-1"}, Connector: contracts.Connector{Key: "tencent-docs"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Enabled || binding.ConnectionID != contracts.AccountRuntimeConnectionID("account-1", "tencent-docs") || len(binding.CredentialBrokerGrant) != 0 {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestAccountRuntimeBindingResolverFailsClosedUntilRemoteSnapshotIsFresh(t *testing.T) {
	release := testReleaseWithImplementation("tencent-docs", "1.0.0", contracts.ImplementationKindRemoteStreamableHTTP)
	release.Manifest.Implementation = contracts.Implementation{Kind: contracts.ImplementationKindRemoteStreamableHTTP, RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{
		ProtocolVersion: "2026-07-28", BindingRef: "tencent-docs.primary", ContractVersion: 1,
		BindingContractHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}
	release.Manifest.AuthorizationKind = "api_key"
	projection := contracts.AuthorizationProjection{AccountID: "account-1", ConnectorKey: "tencent-docs", State: contracts.AuthorizationStateConnected}
	projections := &authorizationProjectionStoreStub{projection: projection}
	readiness := NewAuthorizationReadinessGate()
	resolver := AccountRuntimeBindingResolver{Projections: projections, Readiness: readiness}
	request := contracts.RuntimeBindingRequest{Scope: contracts.OperationScope{AccountID: "account-1"}, Connector: contracts.Connector{Key: "tencent-docs"}, Release: release}

	if binding, err := resolver.ResolveRuntimeBinding(context.Background(), request); err != nil || binding.Enabled {
		t.Fatalf("unsynchronized binding = %#v, %v", binding, err)
	}
	readiness.SetReady("account-1", true)
	if binding, err := resolver.ResolveRuntimeBinding(context.Background(), request); err != nil || binding.Enabled {
		t.Fatalf("legacy projection binding = %#v, %v", binding, err)
	}
	projections.projection.ServerSynchronized = true
	if binding, err := resolver.ResolveRuntimeBinding(context.Background(), request); err != nil || !binding.Enabled ||
		binding.ConnectionID != contracts.AccountRuntimeConnectionID("account-1", "tencent-docs") {
		t.Fatalf("fresh binding = %#v, %v", binding, err)
	}
}

func TestAccountRuntimeBindingResolverUsesDeviceBindingForNoAuthConnector(t *testing.T) {
	release := testReleaseWithImplementation("github", "1.0.0", contracts.ImplementationKindManagedStdio)
	binding, err := (AccountRuntimeBindingResolver{}).ResolveRuntimeBinding(context.Background(), contracts.RuntimeBindingRequest{
		Connector: contracts.Connector{Key: "github"}, Release: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Enabled || binding.ConnectionID != "device-github" {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestAccountRuntimeBindingResolverRequiresAccountForNoAuthRemoteConnector(t *testing.T) {
	release := testReleaseWithImplementation("public-search", "1.0.0", contracts.ImplementationKindRemoteStreamableHTTP)
	release.Manifest.Implementation = contracts.Implementation{Kind: contracts.ImplementationKindRemoteStreamableHTTP,
		RemoteStreamableHTTP: &contracts.RemoteStreamableHTTPImplementation{ProtocolVersion: "2026-07-28"}}
	resolver := AccountRuntimeBindingResolver{}
	request := contracts.RuntimeBindingRequest{Connector: contracts.Connector{Key: "public-search"}, Release: release}
	binding, err := resolver.ResolveRuntimeBinding(context.Background(), request)
	if err != nil || binding.Enabled {
		t.Fatalf("signed-out binding = %#v, %v", binding, err)
	}
	request.Scope = contracts.OperationScope{AccountID: "account-1"}
	binding, err = resolver.ResolveRuntimeBinding(context.Background(), request)
	if err != nil || !binding.Enabled || binding.ConnectionID != contracts.AccountRuntimeConnectionID("account-1", "public-search") {
		t.Fatalf("signed-in binding = %#v, %v", binding, err)
	}
}

type authorizationProjectionStoreStub struct {
	projection contracts.AuthorizationProjection
}

func (store *authorizationProjectionStoreStub) AuthorizationProjection(context.Context, string, string) (contracts.AuthorizationProjection, error) {
	return store.projection, nil
}

func (*authorizationProjectionStoreStub) SaveAuthorizationProjection(context.Context, contracts.AuthorizationProjection) error {
	return nil
}

type credentialGrantIssuerStub struct {
	grant []byte
	calls int
}

func (issuer *credentialGrantIssuerStub) IssueCredentialBrokerGrant(context.Context, string, string, string) ([]byte, error) {
	issuer.calls++
	return issuer.grant, nil
}
