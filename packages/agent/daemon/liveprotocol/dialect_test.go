package liveprotocol

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

const legacyProtocolRevision7101 = "sha256:7101e69f2559036c"

const (
	historical7101EventFrame              = "0a177368613235363a37313031653639663235353930333663120d6c65676163792d73747265616d1a0962696e64696e672d3120072af701080110011af0017b22776f726b73706163654964223a22776f726b73706163652d31222c226167656e7453657373696f6e4964223a2273657373696f6e2d31222c226576656e7454797065223a2272756e74696d655f61637469766974795f757064617465222c2264617461223a7b22776f726b73706163654964223a22776f726b73706163652d31222c226167656e7453657373696f6e4964223a2273657373696f6e2d31222c226576656e7454797065223a2272756e74696d655f61637469766974795f757064617465222c227374617465223a2272756e6e696e67222c226f636375727265644174556e69784d73223a34327d7d"
	historical7101DiscontinuityFrame      = "0a177368613235363a37313031653639663235353930333663120d6c65676163792d73747265616d1a0962696e64696e672d3120072a810108011002227b7b22726561736f6e223a2263616e6f6e6963616c5f757064617465222c227265636f6e63696c654b657973223a5b7b226b696e64223a2273657373696f6e222c22776f726b73706163654964223a22776f726b73706163652d31222c226167656e7453657373696f6e4964223a2273657373696f6e2d31227d5d7d"
	historical7101UnprojectedRestoreFrame = "0a177368613235363a37313031653639663235353930333663120d6c65676163792d73747265616d1a0962696e64696e672d3120072a810108011002227b7b22726561736f6e223a2273657373696f6e5f726573746f726564222c227265636f6e63696c654b657973223a5b7b226b696e64223a2273657373696f6e222c22776f726b73706163654964223a22776f726b73706163652d31222c226167656e7453657373696f6e4964223a2273657373696f6e2d31227d5d7d"
)

func TestAcceptedProtocolRevisionsAreExplicit(t *testing.T) {
	t.Parallel()
	want := []string{ProtocolRevision, legacyProtocolRevision7101}
	if got := AcceptedProtocolRevisions(); !slices.Equal(got, want) {
		t.Fatalf("accepted revisions = %v, want %v", got, want)
	}
	if !SupportsProtocolRevision(legacyProtocolRevision7101) {
		t.Fatal("legacy revision must be accepted")
	}
	if SupportsProtocolRevision("sha256:0000000000000000") {
		t.Fatal("unknown revision must not be accepted")
	}
}

func TestLegacyDialectDowngradesSessionRestore(t *testing.T) {
	t.Parallel()
	publisher, err := NewPublisher(PublisherConfig{
		ProtocolRevision: legacyProtocolRevision7101,
		StreamID:         "stream-1",
		BindingID:        "binding-1",
		Epoch:            1,
	})
	if err != nil {
		t.Fatal(err)
	}
	frames, err := publisher.Publish(PublishInput{
		Discontinuity: &Discontinuity{
			Reason: "session_restored",
			ReconcileKeys: []ReconcileKey{{
				Kind:           "session",
				WorkspaceID:    "workspace-1",
				AgentSessionID: "session-1",
			}},
		},
		Immediate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].ProtocolRevision != legacyProtocolRevision7101 ||
		len(frames[0].Deliveries) != 1 ||
		frames[0].Deliveries[0].Discontinuity == nil ||
		frames[0].Deliveries[0].Discontinuity.Reason != "canonical_update" {
		t.Fatalf("legacy restore projection = %#v", frames)
	}
	encoded, err := EncodeFrame(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := NewSubscriber(SubscriberConfig{ProtocolRevision: legacyProtocolRevision7101})
	if err != nil {
		t.Fatal(err)
	}
	result, err := DecodeAndApply(subscriber, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ReconcileRequired || result.Reason != "canonical_update" {
		t.Fatalf("legacy apply result = %#v", result)
	}
}

func TestLegacyDialectRejectsUnprojectedSessionRestoreFrame(t *testing.T) {
	t.Parallel()
	frame := Frame{
		ProtocolRevision: legacyProtocolRevision7101,
		StreamID:         "legacy-stream",
		BindingID:        "binding-1",
		Epoch:            7,
		Deliveries: []Delivery{{
			Seq:  1,
			Kind: DeliveryKindDiscontinuity,
			Discontinuity: &Discontinuity{
				Reason: "session_restored",
				ReconcileKeys: []ReconcileKey{{
					Kind:           "session",
					WorkspaceID:    "workspace-1",
					AgentSessionID: "session-1",
				}},
			},
		}},
	}
	if _, err := EncodeFrame(frame); !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("encode error = %v, want %v", err, ErrProtocolMismatch)
	}
	raw, err := hex.DecodeString(historical7101UnprojectedRestoreFrame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeFrame(raw); !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("decode error = %v, want %v", err, ErrProtocolMismatch)
	}
}

func TestHistorical7101PublishedFixturesRemainStable(t *testing.T) {
	t.Parallel()
	event := Event{
		WorkspaceID:    "workspace-1",
		AgentSessionID: "session-1",
		EventType:      EventTypeRuntimeActivityUpdate,
		Data:           json.RawMessage(`{"workspaceId":"workspace-1","agentSessionId":"session-1","eventType":"runtime_activity_update","state":"running","occurredAtUnixMs":42}`),
	}
	tests := []struct {
		name    string
		input   PublishInput
		fixture string
		kind    DeliveryKind
	}{
		{
			name:    "event",
			input:   PublishInput{Event: &event, Immediate: true},
			fixture: historical7101EventFrame,
			kind:    DeliveryKindEvent,
		},
		{
			name: "discontinuity",
			input: PublishInput{
				Discontinuity: &Discontinuity{
					Reason: "canonical_update",
					ReconcileKeys: []ReconcileKey{{
						Kind:           "session",
						WorkspaceID:    "workspace-1",
						AgentSessionID: "session-1",
					}},
				},
				Immediate: true,
			},
			fixture: historical7101DiscontinuityFrame,
			kind:    DeliveryKindDiscontinuity,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := hex.DecodeString(tt.fixture)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeFrame(raw)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.ProtocolRevision != legacyProtocolRevision7101 ||
				len(decoded.Deliveries) != 1 || decoded.Deliveries[0].Kind != tt.kind {
				t.Fatalf("decoded historical frame = %#v", decoded)
			}
			publisher, err := NewPublisher(PublisherConfig{
				ProtocolRevision: legacyProtocolRevision7101,
				StreamID:         "legacy-stream",
				BindingID:        "binding-1",
				Epoch:            7,
			})
			if err != nil {
				t.Fatal(err)
			}
			frames, err := publisher.Publish(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := EncodeFrame(frames[0])
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(encoded); got != tt.fixture {
				t.Fatalf("historical output = %s, want %s", got, tt.fixture)
			}
		})
	}
}

func TestUnknownDialectProfileFailsClosed(t *testing.T) {
	t.Parallel()
	for _, dialect := range generatedProtocolDialects {
		if !supportsProtocolDialectProfile(dialect.Profile) {
			t.Fatalf("generated dialect has no implementation: %#v", dialect)
		}
	}
	if supportsProtocolDialectProfile("future-unimplemented") {
		t.Fatal("unknown profile must not be supported")
	}
}

func TestPublisherRejectsUnknownDialect(t *testing.T) {
	t.Parallel()
	_, err := NewPublisher(PublisherConfig{
		ProtocolRevision: "sha256:0000000000000000",
		StreamID:         "stream-1",
		BindingID:        "binding-1",
		Epoch:            1,
	})
	if !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("publisher error = %v, want %v", err, ErrProtocolMismatch)
	}
}

func TestStreamReadyRevisionMustMatchOuterFrame(t *testing.T) {
	t.Parallel()
	_, err := EncodeFrame(Frame{
		ProtocolRevision: legacyProtocolRevision7101,
		StreamID:         "stream-1",
		BindingID:        "binding-1",
		Epoch:            1,
		Deliveries: []Delivery{{
			Seq:  1,
			Kind: DeliveryKindStreamReady,
			StreamReady: &StreamReady{
				ProtocolRevision: ProtocolRevision,
				StreamID:         "stream-1",
				BindingID:        "binding-1",
			},
		}},
	})
	if !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("encode error = %v, want %v", err, ErrInvalidFrame)
	}
}

func TestHistorical7101StreamReadyGoldenFrame(t *testing.T) {
	t.Parallel()
	encoded, err := EncodeFrame(Frame{
		ProtocolRevision: legacyProtocolRevision7101,
		StreamID:         "legacy-stream",
		BindingID:        "binding-1",
		Epoch:            1,
		Deliveries: []Delivery{{
			Seq:  1,
			Kind: DeliveryKindStreamReady,
			StreamReady: &StreamReady{
				ProtocolRevision: legacyProtocolRevision7101,
				StreamID:         "legacy-stream",
				BindingID:        "binding-1",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const golden = "0a177368613235363a37313031653639663235353930333663120d6c65676163792d73747265616d1a0962696e64696e672d3120012a67080110053a617b2270726f746f636f6c5265766973696f6e223a227368613235363a37313031653639663235353930333663222c2273747265616d4964223a226c65676163792d73747265616d222c2262696e64696e674964223a2262696e64696e672d31227d"
	if got := hex.EncodeToString(encoded); got != golden {
		t.Fatalf("historical 7101 frame = %s", got)
	}
}
