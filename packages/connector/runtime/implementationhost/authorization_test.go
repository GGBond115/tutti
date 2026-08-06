package implementationhost

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	market "github.com/tutti-os/tutti/packages/connector/host"
	connectorruntime "github.com/tutti-os/tutti/packages/connector/runtime"
	"github.com/tutti-os/tutti/packages/connector/runtime/command"
)

const authorizationTestDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

type authorizationArtifactResolver struct {
	receipt market.PreparedArtifactReceipt
}

func (resolver authorizationArtifactResolver) ResolvePrepared(context.Context, market.Release) (market.PreparedArtifactReceipt, error) {
	return resolver.receipt, nil
}

type authorizationRuntimeResolver struct {
	executable connectorruntime.ConnectorExecutable
}

func (resolver authorizationRuntimeResolver) ResolveProfile(context.Context, string) (connectorruntime.ResolvedConnectorRuntime, error) {
	return connectorruntime.ResolvedConnectorRuntime{Root: filepath.Dir(resolver.executable.Path), Profile: connectorruntime.ConnectorNodeProfile,
		ABI: "node24-" + runtime.GOOS + "-" + runtime.GOARCH, Node: &resolver.executable, Components: map[string]string{"node": "24.18.0"}}, nil
}

func (resolver authorizationRuntimeResolver) VerifyLaunch(string, string) (connectorruntime.ConnectorExecutable, error) {
	return resolver.executable, nil
}

type authorizationCredentialBroker struct {
	mu    sync.Mutex
	grant string
}

type authorizationObserver struct {
	mu           sync.Mutex
	observations []AuthorizationObservation
}

func (observer *authorizationObserver) ObserveAuthorization(_ context.Context, observation AuthorizationObservation) {
	observer.mu.Lock()
	observer.observations = append(observer.observations, observation)
	observer.mu.Unlock()
}

func (broker *authorizationCredentialBroker) Open(_ context.Context, request CredentialRequest) (CredentialFile, error) {
	broker.mu.Lock()
	broker.grant = string(request.Grant)
	broker.mu.Unlock()
	file, err := os.CreateTemp("", "connector-credential-test-")
	if err != nil {
		return CredentialFile{}, err
	}
	_ = os.Remove(file.Name())
	if _, err := file.WriteString(`{"schemaVersion":"tutti.connector.credentials.v1","token":"secret"}`); err != nil {
		_ = file.Close()
		return CredentialFile{}, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close()
		return CredentialFile{}, err
	}
	return CredentialFile{File: file, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

type authorizationProcessTransport struct {
	mu                sync.Mutex
	credentialPayload string
}

func (transport *authorizationProcessTransport) Start(_ context.Context, spec agentruntime.ProcessSpec) (agentruntime.ProcessConnection, error) {
	if len(spec.SensitiveInheritedFiles) == 1 {
		payload, _ := io.ReadAll(spec.SensitiveInheritedFiles[0].File)
		transport.mu.Lock()
		transport.credentialPayload = string(payload)
		transport.mu.Unlock()
	}
	exitCode := 0
	return &authorizationConnection{frames: []agentruntime.ProcessFrame{{Stdout: []byte(`{"ok":true}`)}, {ExitCode: &exitCode}}}, nil
}

type authorizationConnection struct{ frames []agentruntime.ProcessFrame }

func (*authorizationConnection) Send([]byte) error { return nil }
func (*authorizationConnection) Close() error      { return nil }
func (*authorizationConnection) CloseInput() error { return nil }
func (*authorizationConnection) Terminate() error  { return nil }
func (*authorizationConnection) Kill() error       { return nil }
func (connection *authorizationConnection) Recv() (agentruntime.ProcessFrame, error) {
	if len(connection.frames) == 0 {
		return agentruntime.ProcessFrame{}, io.EOF
	}
	frame := connection.frames[0]
	connection.frames = connection.frames[1:]
	return frame, nil
}

func TestAuthorizedConnectorUsesCredentialBrokerFDWithoutGrantInProcessEnvironment(t *testing.T) {
	root := t.TempDir()
	entrypoint := filepath.Join(root, "connector.js")
	runtimePath := filepath.Join(root, "node")
	if err := os.WriteFile(entrypoint, []byte("// connector"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, []byte("runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	inventory, err := connectorruntime.ExecutionInventoryDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	executable := connectorruntime.ConnectorExecutable{Path: runtimePath,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SizeBytes: 7}
	credentials := &authorizationCredentialBroker{}
	authorization := &authorizationObserver{}
	processes := &authorizationProcessTransport{}
	commands := NewCommandRegistry()
	host, err := New(Config{
		Artifacts: authorizationArtifactResolver{receipt: market.PreparedArtifactReceipt{PreparedPath: root, InventoryDigest: inventory}},
		Runtimes:  authorizationRuntimeResolver{executable: executable}, Processes: processes, Credentials: credentials,
		Authorization: authorization,
		Commands:      commands, StateRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	release := market.Release{SchemaVersion: "1", ReleaseID: "github@1.0.0", ConnectorKey: "github", Version: "1.0.0",
		ReleaseDigest: authorizationTestDigest, ManifestDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Artifact: market.Artifact{StorageRealm: "tutti.connector.artifacts.v1", Key: "connectors/github/1.0.0.zip", ObjectVersion: "v1",
			SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", SizeBytes: 1024,
			MediaType: "application/vnd.tutti.connector+zip"}, PublishedAt: time.Unix(1, 0).UTC(), Status: market.ReleaseStatusAvailable,
		Manifest: market.Manifest{SchemaVersion: "1", DisplayName: "GitHub", AuthorizationKind: "oauth2",
			Implementation: market.Implementation{Kind: market.ImplementationKindManagedStdio,
				ManagedStdio: &market.ManagedStdioImplementation{Runtime: market.RuntimeRequirement{Language: "node",
					Profile: connectorruntime.ConnectorNodeProfile, ABI: "node24-" + runtime.GOOS + "-" + runtime.GOARCH,
					VersionRange: ">=24.0.0 <25.0.0"}, CredentialBrokerProtocol: market.CredentialBrokerProtocolV1,
					CLI: &market.ManagedCLIInterface{Entrypoint: "connector.js", Commands: []market.CLICommand{{Name: "status",
						InputSchema: map[string]any{"type": "object"}, TimeoutMS: 1000}}}}}}}
	connector := market.Connector{Key: "github", Release: release,
		Installation:  market.Installation{State: market.InstallationStateInstalled, InstalledReleaseDigest: authorizationTestDigest},
		Authorization: market.Authorization{State: market.AuthorizationStateConnected}}
	credentialGrant := []byte("opaque-grant")
	receipt, err := host.Reconcile(context.Background(), ReconcileRequest{Runtime: market.RuntimeReconcileRequest{
		OperationID: "op-1", ConnectionID: "connection-1", Connector: connector, Enabled: true,
		Generation: market.HostGeneration{BootEpoch: "boot-1", Generation: 1}}, CredentialGrant: credentialGrant})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commands.Invoke(context.Background(), command.InvokeRequest{CommandID: receipt.RouteIDs[0], Input: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	credentials.mu.Lock()
	grant := credentials.grant
	credentials.mu.Unlock()
	authorization.mu.Lock()
	observations := append([]AuthorizationObservation(nil), authorization.observations...)
	authorization.mu.Unlock()
	processes.mu.Lock()
	payload := processes.credentialPayload
	processes.mu.Unlock()
	if grant != "opaque-grant" || payload == "" {
		t.Fatalf("credential binding grant=%q payload=%q", grant, payload)
	}
	for _, value := range credentialGrant {
		if value != 0 {
			t.Fatal("ImplementationHost retained the caller's credential grant")
		}
	}
	if len(observations) == 0 || observations[0].State != market.AuthorizationStateConnected || observations[0].ConnectorKey != "github" {
		t.Fatalf("authorization observations = %+v", observations)
	}
}

func TestAuthorizedConnectorRejectsMissingCredentialGrant(t *testing.T) {
	// Authorization binding is tested before any artifact or process side effect.
	host := &Host{credentials: &authorizationCredentialBroker{}}
	release := market.Release{Manifest: market.Manifest{AuthorizationKind: "oauth2", Implementation: market.Implementation{
		ManagedStdio: &market.ManagedStdioImplementation{CredentialBrokerProtocol: market.CredentialBrokerProtocolV1}}}}
	err := host.validateAuthorization(market.RuntimeReconcileRequest{Connector: market.Connector{Release: release,
		Authorization: market.Authorization{State: market.AuthorizationStateConnected}}}, nil)
	if err == nil {
		t.Fatal("missing credential grant was accepted")
	}
}

func TestExpiredCredentialGrantRetiresPublishedRoute(t *testing.T) {
	routes := connectorruntime.NewRouteTable()
	observer := &authorizationObserver{}
	route := &connectorRoute{
		id: "connection-1\x00github", connectionID: "connection-1", connectorKey: "github",
		releaseDigest: authorizationTestDigest, generation: market.HostGeneration{BootEpoch: "boot-1", Generation: 1},
		credentials: expiredCredentialBroker{}, authKind: "oauth2", credentialGrant: []byte("expired-grant"),
		processes: connectorruntime.NewProcessGroup(),
	}
	if err := routes.Commit(route); err != nil {
		t.Fatal(err)
	}
	host := &Host{routes: routes, authorization: observer}
	if _, _, err := host.startProcess(context.Background(), route, agentruntime.ProcessSpec{}, true); !errors.Is(err, ErrCredentialGrantExpired) {
		t.Fatalf("start process error = %v", err)
	}
	if routes.Route(route.id) != nil {
		t.Fatal("expired authorization left the route published")
	}
	observer.mu.Lock()
	observations := append([]AuthorizationObservation(nil), observer.observations...)
	observer.mu.Unlock()
	if len(observations) != 1 || observations[0].State != market.AuthorizationStateExpired {
		t.Fatalf("authorization observations = %+v", observations)
	}
}

type expiredCredentialBroker struct{}

func (expiredCredentialBroker) Open(context.Context, CredentialRequest) (CredentialFile, error) {
	return CredentialFile{}, ErrCredentialGrantExpired
}
