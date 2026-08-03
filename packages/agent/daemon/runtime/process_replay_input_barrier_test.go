package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	sessionreplay "github.com/tutti-os/tutti/packages/agent/session-replay"
)

func TestReplayProviderInputBarrierHoldsConnectionsIndependently(t *testing.T) {
	t.Parallel()

	barrier := newReplayProviderInputBarrier()
	if err := barrier.setTargets([]sessionreplay.ProviderUnitPosition{
		{ConnectionID: "connection-1", ChunkSeq: 4, UnitIndex: 1},
		{ConnectionID: "connection-2", ChunkSeq: 9, UnitIndex: 2},
	}); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- barrier.complete(context.Background(), ProviderInputUnit{
			Position: sessionreplay.ProviderUnitPosition{
				ConnectionID: "connection-1", ChunkSeq: 4, UnitIndex: 1,
			},
		}, closed)
	}()

	select {
	case err := <-firstDone:
		t.Fatalf("connection-1 was not held: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	if err := barrier.complete(context.Background(), ProviderInputUnit{
		Position: sessionreplay.ProviderUnitPosition{
			ConnectionID: "connection-2", ChunkSeq: 9, UnitIndex: 1,
		},
	}, closed); err != nil {
		t.Fatalf("slower connection was blocked before target: %v", err)
	}
	barrier.clearTargets()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("connection-1 did not resume")
	}
}

func TestReplayProviderInputBarrierFailsClosedOnOvershoot(t *testing.T) {
	t.Parallel()

	barrier := newReplayProviderInputBarrier()
	if err := barrier.setTargets([]sessionreplay.ProviderUnitPosition{{
		ConnectionID: "connection-1", ChunkSeq: 4, UnitIndex: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	err := barrier.complete(context.Background(), ProviderInputUnit{
		Position: sessionreplay.ProviderUnitPosition{
			ConnectionID: "connection-1", ChunkSeq: 4, UnitIndex: 2,
		},
	}, make(chan struct{}))
	if !errors.Is(err, ErrReplayProviderOvershot) {
		t.Fatalf("overshoot error = %v", err)
	}
}
