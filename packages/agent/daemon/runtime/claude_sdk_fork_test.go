package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
)

func TestClaudeSDKForkCapabilitiesUsesStatelessTranscriptInspection(t *testing.T) {
	conn := &claudeSDKForkTestConnection{
		responseType: "ok",
		responsePayload: map[string]any{
			"providerTurnIds": []string{"prompt-1", "prompt-2"},
		},
	}
	adapter := NewClaudeCodeSDKAdapter(claudeSDKForkTestTransport{conn: conn})
	source := standardTestSession(ProviderClaudeCode)
	source.ProviderSessionID = "claude-source"

	capabilities, err := adapter.ForkCapabilities(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.DriverKind != claudeSDKForkDriverKind ||
		capabilities.DriverVersion != claudeSDKForkDriverVersion ||
		!capabilities.ThroughTurn ||
		!capabilities.ThroughProviderTurnIDsKnown ||
		!reflect.DeepEqual(
			capabilities.ThroughProviderTurnIDs,
			[]string{"prompt-1", "prompt-2"},
		) {
		t.Fatalf("capabilities=%#v", capabilities)
	}
	requests := conn.requests()
	if len(requests) != 1 ||
		requests[0].Type != "inspect_fork_checkpoints" ||
		payloadString(requests[0].Payload, "providerSessionId") != "claude-source" {
		t.Fatalf("requests=%#v", requests)
	}
}

func TestClaudeSDKForkReturnsProviderOwnedIdentityEvidence(t *testing.T) {
	conn := &claudeSDKForkTestConnection{
		responseType: "ok",
		responsePayload: map[string]any{
			"providerSessionId":     "claude-child",
			"targetProviderTurnIds": []string{"child-prompt-1", "child-prompt-2"},
			"stateBindingMode":      "provider_owned",
			"stateBindingReceipt":   "claude-sdk-fork-v1:receipt",
			"deliveryDisposition":   "accepted",
		},
	}
	adapter := NewClaudeCodeSDKAdapter(claudeSDKForkTestTransport{conn: conn})
	source := standardTestSession(ProviderClaudeCode)
	source.ProviderSessionID = "claude-source"

	result, err := adapter.Fork(t.Context(), SessionForkInput{
		Source: source, ProviderTurnID: "prompt-2",
		ProviderTurnIDs: []string{"prompt-1", "prompt-2"},
		TargetTitle:     "Claude session (2)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderSessionID != "claude-child" ||
		result.DeliveryDisposition != SessionForkDeliveryAccepted ||
		result.StateBindingMode != "provider_owned" ||
		result.StateBindingReceipt == "" ||
		!reflect.DeepEqual(
			result.TargetProviderTurnIDs,
			[]string{"child-prompt-1", "child-prompt-2"},
		) {
		t.Fatalf("result=%#v", result)
	}
	requests := conn.requests()
	if len(requests) != 1 || requests[0].Type != "fork_session" ||
		payloadString(requests[0].Payload, "title") != "Claude session (2)" {
		t.Fatalf("requests=%#v", requests)
	}
}

func TestClaudeSDKForkPreservesUnknownDispositionAfterDispatch(t *testing.T) {
	conn := &claudeSDKForkTestConnection{
		responseType: "error",
		responsePayload: map[string]any{
			"error":               "Claude SDK session fork failed",
			"deliveryDisposition": "unknown",
		},
	}
	adapter := NewClaudeCodeSDKAdapter(claudeSDKForkTestTransport{conn: conn})
	source := standardTestSession(ProviderClaudeCode)
	source.ProviderSessionID = "claude-source"
	result, err := adapter.Fork(t.Context(), SessionForkInput{
		Source: source, ProviderTurnID: "prompt-1",
		ProviderTurnIDs: []string{"prompt-1"}, TargetTitle: "Child",
	})
	if err == nil || result.DeliveryDisposition != SessionForkDeliveryUnknown {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

type claudeSDKForkTestTransport struct {
	conn ProcessConnection
}

func (t claudeSDKForkTestTransport) Start(
	context.Context,
	ProcessSpec,
) (ProcessConnection, error) {
	if t.conn == nil {
		return nil, errors.New("test connection is unavailable")
	}
	return t.conn, nil
}

type claudeSDKForkTestConnection struct {
	mu              sync.Mutex
	sent            []claudeSDKSidecarRequest
	frames          []ProcessFrame
	responseType    string
	responsePayload map[string]any
}

func (c *claudeSDKForkTestConnection) Send(data []byte) error {
	var request claudeSDKSidecarRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	response, err := json.Marshal(claudeSDKSidecarEvent{
		Version: claudeSDKSidecarProtocolVersion,
		ID:      request.ID,
		Type:    c.responseType,
		Payload: clonePayload(c.responsePayload),
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.sent = append(c.sent, request)
	c.frames = append(c.frames, ProcessFrame{Stdout: append(response, '\n')})
	c.mu.Unlock()
	return nil
}

func (c *claudeSDKForkTestConnection) Recv() (ProcessFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.frames) == 0 {
		return ProcessFrame{}, io.EOF
	}
	frame := c.frames[0]
	c.frames = c.frames[1:]
	return frame, nil
}

func (*claudeSDKForkTestConnection) Close() error { return nil }

func (c *claudeSDKForkTestConnection) requests() []claudeSDKSidecarRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]claudeSDKSidecarRequest(nil), c.sent...)
}
