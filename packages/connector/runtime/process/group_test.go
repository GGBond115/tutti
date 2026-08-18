package process

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type groupConnection struct {
	mu         sync.Mutex
	closeCount int
}

func (*groupConnection) Send([]byte) error    { return nil }
func (*groupConnection) Recv() (Frame, error) { return Frame{}, io.EOF }
func (connection *groupConnection) Close() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.closeCount++
	return nil
}

func TestGroupFencesPendingAndFutureStarts(t *testing.T) {
	group := NewGroup()
	processContext, processID, ok := group.Begin(context.Background())
	if !ok || processID == 0 {
		t.Fatalf("Begin() = %#v, %d, %t", processContext, processID, ok)
	}
	group.Fence()
	if !group.IsFenced() {
		t.Fatal("group was not fenced")
	}
	if err := processContext.Err(); err != context.Canceled {
		t.Fatalf("pending start context error = %v", err)
	}
	if group.CommitStart(processID, &groupConnection{}) {
		t.Fatal("late process start escaped fence")
	}
	if nextContext, nextID, allowed := group.Begin(context.Background()); allowed || nextContext != nil || nextID != 0 {
		t.Fatalf("Begin() after fence = %#v, %d, %t", nextContext, nextID, allowed)
	}
}

func TestGroupOwnsCommittedProcessUntilRelease(t *testing.T) {
	group := NewGroup()
	processContext, processID, ok := group.Begin(context.Background())
	if !ok {
		t.Fatal("Begin() rejected active group")
	}
	connection := &groupConnection{}
	if !group.CommitStart(processID, connection) {
		t.Fatal("CommitStart() rejected pending process")
	}
	if got := group.ActiveCount(); got != 1 {
		t.Fatalf("ActiveCount() = %d, want 1", got)
	}
	if err := group.ReleaseWithError(processID, connection); err != nil {
		t.Fatal(err)
	}
	if processContext.Err() != context.Canceled {
		t.Fatalf("owned process context error = %v", processContext.Err())
	}
	if got := group.ActiveCount(); got != 0 {
		t.Fatalf("ActiveCount() = %d, want 0", got)
	}
	group.Release(processID, connection)
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.closeCount != 1 {
		t.Fatalf("Close() count = %d, want 1", connection.closeCount)
	}
}

func TestGroupCloseFencesAndClosesEveryOwnedProcess(t *testing.T) {
	group := NewGroup()
	connections := []*groupConnection{{}, {}}
	for _, connection := range connections {
		_, processID, ok := group.Begin(context.Background())
		if !ok || !group.CommitStart(processID, connection) {
			t.Fatal("failed to register process")
		}
	}
	if err := group.Close(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if !group.IsFenced() || group.ActiveCount() != 0 {
		t.Fatalf("group state after Close(): fenced=%t active=%d", group.IsFenced(), group.ActiveCount())
	}
	for index, connection := range connections {
		connection.mu.Lock()
		count := connection.closeCount
		connection.mu.Unlock()
		if count != 1 {
			t.Fatalf("connection %d Close() count = %d, want 1", index, count)
		}
	}
}
