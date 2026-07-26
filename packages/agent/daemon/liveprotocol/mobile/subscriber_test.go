package liveprotocolmobile

import (
	"encoding/json"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/liveprotocol"
)

func TestSubscriberAppliesContiguousFrame(t *testing.T) {
	t.Parallel()
	subscriber, err := NewSubscriber(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	event, err := liveprotocol.NewMessageDeltaEvent(liveprotocol.MessageDeltaData{
		WorkspaceID:      "workspace-1",
		AgentSessionID:   "session-1",
		MessageID:        "message-1",
		TurnID:           "turn-1",
		Role:             "assistant",
		Kind:             "text",
		OccurredAtUnixMS: 10,
		Content: &liveprotocol.MessageContentOperation{
			Operation: "set",
			Value:     json.RawMessage(`"hello"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rawEvent, err := liveprotocol.MarshalEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := liveprotocol.EncodeFrame(liveprotocol.Frame{
		ProtocolRevision: liveprotocol.ProtocolRevision,
		StreamID:         "stream-1",
		BindingID:        "binding-1",
		Epoch:            1,
		Deliveries: []liveprotocol.Delivery{{
			Seq:   1,
			Kind:  liveprotocol.DeliveryKindEvent,
			Event: rawEvent,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := subscriber.Apply(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var decoded applyEnvelope
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Epoch != 1 || decoded.AfterSeq != 1 || len(decoded.Accepted) != 1 ||
		decoded.Accepted[0].Kind != "event" {
		t.Fatalf("unexpected mobile delivery: %+v", decoded)
	}
}

func TestSubscriberReportsSequenceGap(t *testing.T) {
	t.Parallel()
	subscriber, err := NewSubscriber(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := liveprotocol.EncodeFrame(liveprotocol.Frame{
		ProtocolRevision: liveprotocol.ProtocolRevision,
		StreamID:         "stream-1",
		BindingID:        "binding-1",
		Epoch:            1,
		Deliveries: []liveprotocol.Delivery{{
			Seq:  2,
			Kind: liveprotocol.DeliveryKindDiscontinuity,
			Discontinuity: &liveprotocol.Discontinuity{
				Reason: "canonical_update",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := subscriber.Apply(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var decoded applyEnvelope
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.ReconcileRequired || decoded.Reason != "sequence_gap" ||
		decoded.AfterSeq != 0 || len(decoded.Accepted) != 0 {
		t.Fatalf("unexpected gap result: %+v", decoded)
	}
}
