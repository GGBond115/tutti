package liveprotocolmobile

import (
	"encoding/hex"
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

func TestSubscriberForwardsAttachmentRecoveryFence(t *testing.T) {
	t.Parallel()
	subscriber, err := NewSubscriber(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	attachment := liveprotocol.AttachmentChanged{
		BindingID:          "binding-1",
		WorkspaceID:        "workspace-1",
		AgentSessionID:     "session-1",
		CanonicalTurnID:    "canonical-turn-1",
		CallerTurnID:       "caller-turn-1",
		AttachmentRevision: 3,
	}
	caughtUp := liveprotocol.AttachmentCaughtUp{
		BindingID:          "binding-1",
		WorkspaceID:        "workspace-1",
		AgentSessionID:     "session-1",
		CanonicalTurnID:    "canonical-turn-1",
		CallerTurnID:       "caller-turn-1",
		AttachmentRevision: 3,
	}
	encoded, err := liveprotocol.EncodeFrame(liveprotocol.Frame{
		ProtocolRevision: liveprotocol.ProtocolRevision,
		StreamID:         "stream-1",
		BindingID:        "binding-1",
		Epoch:            4,
		Deliveries: []liveprotocol.Delivery{
			{
				Seq:               1,
				Kind:              liveprotocol.DeliveryKindAttachmentChanged,
				AttachmentChanged: &attachment,
			},
			{
				Seq:                2,
				Kind:               liveprotocol.DeliveryKindAttachmentCaughtUp,
				AttachmentCaughtUp: &caughtUp,
			},
		},
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
	if decoded.Epoch != 4 || decoded.AfterSeq != 2 || len(decoded.Accepted) != 2 {
		t.Fatalf("unexpected mobile attachment fence envelope: %+v", decoded)
	}
	if changed := decoded.Accepted[0]; changed.Kind != "attachment_changed" ||
		changed.AttachmentChanged == nil ||
		changed.AttachmentChanged.AttachmentRevision != 3 ||
		changed.AttachmentChanged.CallerTurnID != "caller-turn-1" {
		t.Fatalf("unexpected mobile attachment change: %+v", changed)
	}
	if barrier := decoded.Accepted[1]; barrier.Kind != "attachment_caught_up" ||
		barrier.AttachmentCaughtUp == nil ||
		barrier.AttachmentCaughtUp.AttachmentRevision != 3 ||
		barrier.AttachmentCaughtUp.CanonicalTurnID != "canonical-turn-1" {
		t.Fatalf("unexpected mobile attachment catch-up: %+v", barrier)
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

func TestSubscriberSupportsExplicitHistoricalRevision(t *testing.T) {
	t.Parallel()
	const revision = "sha256:7101e69f2559036c"
	const published7101EventFrame = "0a177368613235363a37313031653639663235353930333663120d6c65676163792d73747265616d1a0962696e64696e672d3120072af701080110011af0017b22776f726b73706163654964223a22776f726b73706163652d31222c226167656e7453657373696f6e4964223a2273657373696f6e2d31222c226576656e7454797065223a2272756e74696d655f61637469766974795f757064617465222c2264617461223a7b22776f726b73706163654964223a22776f726b73706163652d31222c226167656e7453657373696f6e4964223a2273657373696f6e2d31222c226576656e7454797065223a2272756e74696d655f61637469766974795f757064617465222c227374617465223a2272756e6e696e67222c226f636375727265644174556e69784d73223a34327d7d"
	subscriber, err := NewSubscriberForRevision(revision, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := hex.DecodeString(published7101EventFrame)
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
	if len(decoded.Accepted) != 1 || decoded.Accepted[0].Kind != "event" ||
		len(decoded.Accepted[0].Event) == 0 {
		t.Fatalf("historical delivery = %+v", decoded)
	}
}
