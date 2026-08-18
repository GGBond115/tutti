package process

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
)

type recordingConnection struct {
	mu     sync.Mutex
	writes [][]byte
	err    error
}

func (connection *recordingConnection) Send(data []byte) error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.writes = append(connection.writes, append([]byte(nil), data...))
	return connection.err
}

func (*recordingConnection) Recv() (Frame, error) { return Frame{}, io.EOF }
func (*recordingConnection) Close() error         { return nil }

func TestNDJSONWriterSendsOneEncodedLine(t *testing.T) {
	connection := &recordingConnection{}
	writer := NewNDJSONWriter(connection)
	if err := writer.SendJSON(struct {
		Name string `json:"name"`
	}{Name: "calendar"}); err != nil {
		t.Fatal(err)
	}
	if len(connection.writes) != 1 || string(connection.writes[0]) != "{\"name\":\"calendar\"}\n" {
		t.Fatalf("writes = %q", connection.writes)
	}
}

func TestNDJSONWriterRejectsUnencodablePayloadBeforeSending(t *testing.T) {
	connection := &recordingConnection{}
	writer := NewNDJSONWriter(connection)
	if err := writer.SendJSON(make(chan struct{})); err == nil {
		t.Fatal("SendJSON() error = nil, want encoding rejection")
	}
	if len(connection.writes) != 0 {
		t.Fatalf("writes = %q, want no process input", connection.writes)
	}
}

func TestNDJSONWriterSerializesConcurrentLines(t *testing.T) {
	connection := &recordingConnection{}
	writer := NewNDJSONWriter(connection)
	const count = 32
	var group sync.WaitGroup
	group.Add(count)
	for index := 0; index < count; index++ {
		go func(index int) {
			defer group.Done()
			payload, err := json.Marshal(map[string]int{"index": index})
			if err != nil {
				t.Errorf("marshal payload: %v", err)
				return
			}
			if err := writer.SendLine(append(payload, '\n')); err != nil {
				t.Errorf("SendLine(): %v", err)
			}
		}(index)
	}
	group.Wait()
	if len(connection.writes) != count {
		t.Fatalf("write count = %d, want %d", len(connection.writes), count)
	}
	for _, line := range connection.writes {
		var payload map[string]int
		if err := json.Unmarshal(line, &payload); err != nil {
			t.Fatalf("invalid complete NDJSON line %q: %v", line, err)
		}
	}
}

func TestNDJSONWriterPreservesConnectionError(t *testing.T) {
	want := errors.New("closed")
	writer := NewNDJSONWriter(&recordingConnection{err: want})
	if err := writer.SendLine([]byte("{}\n")); !errors.Is(err, want) {
		t.Fatalf("SendLine() error = %v, want %v", err, want)
	}
}
