package computer

import (
	"context"
	"encoding/json"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	mcpservice "github.com/tutti-os/tutti/packages/connector/runtime/mcp"
	connectorprocess "github.com/tutti-os/tutti/packages/connector/runtime/process"
)

// mcpClient preserves the computer session's private adapter while sharing the
// protocol implementation with browser and connector hosts.
type mcpClient struct{ shared *mcpservice.StdioClient }

func newMCPClient(connection agentruntime.ProcessConnection) *mcpClient {
	shared, err := mcpservice.NewStdioClient(mcpservice.StdioClientConfig{
		Connection:  computerMCPProcessConnection(connection),
		ProcessName: "computer MCP",
		// Preserve the existing computer behavior. Connector clients omit this
		// handler and therefore fail closed with method-not-supported.
		ServerRequestHandler: func(request mcpservice.ServerRequest) (any, *mcpservice.RPCError) {
			if request.Method == "elicitation/create" {
				return map[string]any{"action": "accept", "content": map[string]any{}}, nil
			}
			return nil, &mcpservice.RPCError{Code: -32601, Message: "method not supported"}
		},
	})
	if err != nil {
		panic(err)
	}
	return &mcpClient{shared: shared}
}

// computerProcessConnection keeps computer lifecycle and process ownership in
// Agent runtime while adapting only the shared MCP protocol frame boundary.
type computerProcessConnection struct {
	agentruntime.ProcessConnection
}

func computerMCPProcessConnection(connection agentruntime.ProcessConnection) connectorprocess.Connection {
	if connection == nil {
		return nil
	}
	return computerProcessConnection{ProcessConnection: connection}
}

func (connection computerProcessConnection) Recv() (connectorprocess.Frame, error) {
	frame, err := connection.ProcessConnection.Recv()
	return connectorprocess.Frame{Stdout: frame.Stdout, Stderr: frame.Stderr, ExitCode: frame.ExitCode}, err
}

func (client *mcpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return client.shared.Call(ctx, method, params)
}

func (client *mcpClient) notify(method string, params any) error {
	return client.shared.Notify(method, params)
}

func (client *mcpClient) isClosed() bool { return client == nil || client.shared.IsClosed() }
