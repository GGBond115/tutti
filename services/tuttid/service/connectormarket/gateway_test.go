package connectormarket

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestGatewayUsesOpaqueRoutesAndRevalidatesCaller(t *testing.T) {
	recorder := &gatewaySecurityRecorder{}
	gateway, err := NewGateway(GatewayConfig{
		PeerAuthorizer: peerAuthorizerFunc(func(_ context.Context, caller GatewayCaller) error {
			if caller.Peer.Nonce != "nonce-1" {
				return errors.New("invalid peer")
			}
			return nil
		}),
		SchemaValidator: schemaValidatorFunc(func(_, input json.RawMessage) error {
			if string(input) != `{"owner":"tutti"}` {
				return errors.New("invalid input")
			}
			return nil
		}),
		CredentialBroker: recorder, ResourceGrants: recorder, NetworkEgress: recorder, Processes: recorder,
	})
	if err != nil {
		t.Fatal(err)
	}
	route := GatewayRoute{WorkspaceID: "workspace-1", SessionID: "session-1", ConnectorKey: "github",
		ReleaseDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UpstreamName: "search_repositories",
		Kind: GatewayToolMCP, Generation: 7, InputSchema: json.RawMessage(`{"type":"object"}`),
		Invoke: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		},
	}
	ids, err := gateway.Register([]GatewayRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] == "github__search_repositories" {
		t.Fatalf("ids = %#v", ids)
	}
	result, err := gateway.Invoke(context.Background(), GatewayCaller{WorkspaceID: "workspace-1", SessionID: "session-1", Peer: PeerProof{Nonce: "nonce-1"}}, ids[0], json.RawMessage(`{"owner":"tutti"}`))
	if err != nil || string(result) != `{"ok":true}` {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if _, err := gateway.Invoke(context.Background(), GatewayCaller{WorkspaceID: "workspace-2", SessionID: "session-1", Peer: PeerProof{Nonce: "nonce-1"}}, ids[0], json.RawMessage(`{"owner":"tutti"}`)); err == nil {
		t.Fatal("cross-workspace route was accepted")
	}
}

func TestGatewaySecurityRevocationFencesBeforeTermination(t *testing.T) {
	recorder := &gatewaySecurityRecorder{}
	gateway, err := NewGateway(GatewayConfig{PeerAuthorizer: peerAuthorizerFunc(func(context.Context, GatewayCaller) error { return nil }),
		SchemaValidator:  schemaValidatorFunc(func(json.RawMessage, json.RawMessage) error { return nil }),
		CredentialBroker: namedRevoker{name: "broker", recorder: recorder}, ResourceGrants: namedRevoker{name: "grant", recorder: recorder},
		NetworkEgress: namedRevoker{name: "egress", recorder: recorder}, Processes: recorder})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.SecurityRevoke(context.Background(), "workspace-1", "github", 9, time.Now()); err != nil {
		t.Fatal(err)
	}
	want := []string{"broker", "grant", "egress", "cancel", "kill"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("calls = %#v, want %#v", recorder.calls, want)
	}
}

type peerAuthorizerFunc func(context.Context, GatewayCaller) error

func (function peerAuthorizerFunc) Authorize(ctx context.Context, caller GatewayCaller) error {
	return function(ctx, caller)
}

type schemaValidatorFunc func(json.RawMessage, json.RawMessage) error

func (function schemaValidatorFunc) Validate(schema, input json.RawMessage) error {
	return function(schema, input)
}

type gatewaySecurityRecorder struct{ calls []string }

func (recorder *gatewaySecurityRecorder) RevokeGeneration(context.Context, string, string, uint64) error {
	recorder.calls = append(recorder.calls, "revoke")
	return nil
}
func (recorder *gatewaySecurityRecorder) CancelGeneration(context.Context, string, string, uint64) error {
	recorder.calls = append(recorder.calls, "cancel")
	return nil
}
func (recorder *gatewaySecurityRecorder) KillGeneration(context.Context, string, string, uint64) error {
	recorder.calls = append(recorder.calls, "kill")
	return nil
}

type namedRevoker struct {
	name     string
	recorder *gatewaySecurityRecorder
}

func (revoker namedRevoker) RevokeGeneration(context.Context, string, string, uint64) error {
	revoker.recorder.calls = append(revoker.recorder.calls, revoker.name)
	return nil
}
