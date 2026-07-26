package authenticated

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestErrorPhaseFindsWrappedConnectError(t *testing.T) {
	root := errors.New("connectivity check failed")
	err := fmt.Errorf("establish peer: %w", &ConnectError{
		Phase: ConnectErrorPhaseConnectivity,
		Err:   root,
	})

	phase, ok := ErrorPhase(err)
	if !ok || phase != ConnectErrorPhaseConnectivity {
		t.Fatalf("ErrorPhase() = %q, %v; want %q, true", phase, ok, ConnectErrorPhaseConnectivity)
	}
	if !errors.Is(err, root) {
		t.Fatal("ConnectError does not unwrap its cause")
	}
}

func TestClassifyConnectFailurePreservesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := classifyConnectFailure(
		ctx,
		ConnectErrorPhaseAuthenticatedTransport,
		errors.New("late transport failure"),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("classifyConnectFailure() = %v; want context cancellation", err)
	}
	if _, ok := ErrorPhase(err); ok {
		t.Fatal("caller cancellation was classified as a path or transport failure")
	}
}
