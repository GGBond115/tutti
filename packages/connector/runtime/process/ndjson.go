package process

import (
	"encoding/json"
	"sync"
)

// NDJSONWriter serializes newline-delimited writes to one process connection.
type NDJSONWriter struct {
	connection Connection
	mu         sync.Mutex
}

func NewNDJSONWriter(connection Connection) NDJSONWriter {
	return NDJSONWriter{connection: connection}
}

func (writer *NDJSONWriter) SendJSON(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writer.SendLine(append(data, '\n'))
}

func (writer *NDJSONWriter) SendLine(data []byte) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.connection.Send(data)
}
