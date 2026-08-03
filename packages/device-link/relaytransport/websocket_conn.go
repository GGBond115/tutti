package relaytransport

import (
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type websocketByteConn struct {
	ws *websocket.Conn

	readMu  sync.Mutex
	readBuf []byte
	writeMu sync.Mutex
}

func newWebSocketByteConn(ws *websocket.Conn) *websocketByteConn {
	return &websocketByteConn{ws: ws}
}

func (c *websocketByteConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	for len(c.readBuf) == 0 {
		messageType, payload, err := c.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		if messageType == websocket.BinaryMessage {
			c.readBuf = payload
		}
	}
	count := copy(p, c.readBuf)
	c.readBuf = c.readBuf[count:]
	return count, nil
}

func (c *websocketByteConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *websocketByteConn) Close() error         { return c.ws.Close() }
func (c *websocketByteConn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *websocketByteConn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }
func (c *websocketByteConn) SetReadDeadline(value time.Time) error {
	return c.ws.UnderlyingConn().SetReadDeadline(value)
}

func (c *websocketByteConn) SetDeadline(value time.Time) error {
	if err := c.ws.SetReadDeadline(value); err != nil {
		return err
	}
	return c.SetWriteDeadline(value)
}

func (c *websocketByteConn) SetWriteDeadline(value time.Time) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.SetWriteDeadline(value)
}
