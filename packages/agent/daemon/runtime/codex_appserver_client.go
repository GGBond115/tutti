package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/runtime/codexproto"
)

type codexAppServerClient struct {
	raw       *acpClient
	scheduler *appServerRequestScheduler
	// messageRouter, when present, is the connection-wide dispatcher for
	// clients that own more than one app-server thread (for example a parent
	// plus ephemeral Side threads). It deliberately overrides per-RPC
	// handlers so notifications from another thread cannot be misattributed
	// merely because one thread has an RPC in flight.
	messageRouterMu sync.RWMutex
	messageRouter   acpMessageHandler
	// Close remains idempotent after success but retries a failed physical
	// transport close so lifecycle cleanup never loses process ownership.
	closeMu sync.Mutex
	closed  bool
	// parsedNotificationMethods tracks notification methods already run
	// through the typed schema parse (telemetry only).
	parsedNotificationMethods sync.Map
	stderrMu                  sync.Mutex
	mcpFailureScheduled       bool
}

type codexAppServerCaller struct {
	raw       *acpClient
	scheduler *appServerRequestScheduler
	timeout   time.Duration
	rawResult json.RawMessage
}

type providerProgressWaiter interface {
	WaitForProviderProgress(context.Context, time.Duration) error
}

func newCodexAppServerClient(conn ProcessConnection) *codexAppServerClient {
	return &codexAppServerClient{
		raw: newAppServerJSONRPCClient(conn), scheduler: newAppServerRequestScheduler(),
	}
}

func (c *codexAppServerClient) SetMessageHandler(handler acpMessageHandler) {
	if c == nil || c.raw == nil {
		return
	}
	c.raw.SetMessageHandler(func(ctx context.Context, message acpMessage) error {
		c.parseInboundMessage(message)
		if router := c.getMessageRouter(); router != nil {
			return router(ctx, message)
		}
		if handler == nil {
			return nil
		}
		return handler(ctx, message)
	})
}

func (c *codexAppServerClient) SetMessageRouter(router acpMessageHandler) {
	if c == nil {
		return
	}
	c.messageRouterMu.Lock()
	c.messageRouter = router
	c.messageRouterMu.Unlock()
}

func (c *codexAppServerClient) getMessageRouter() acpMessageHandler {
	if c == nil {
		return nil
	}
	c.messageRouterMu.RLock()
	defer c.messageRouterMu.RUnlock()
	return c.messageRouter
}

func (c *codexAppServerClient) SetStderrSink(sink func([]byte)) {
	if c == nil || c.raw == nil {
		return
	}
	c.raw.SetStderrSink(func(chunk []byte) {
		if sink != nil {
			sink(chunk)
		}
		c.observeMCPStderr(chunk)
	})
}

func (c *codexAppServerClient) observeMCPStderr(chunk []byte) {
	if c == nil || len(chunk) == 0 {
		return
	}
	diagnostics := c.raw.Diagnostics()
	detail := diagnostics.StderrTail
	failure := codexMCPServerStartupFailureFromStderr(detail)
	if failure == nil {
		return
	}
	c.stderrMu.Lock()
	if c.mcpFailureScheduled {
		c.stderrMu.Unlock()
		return
	}
	c.mcpFailureScheduled = true
	c.stderrMu.Unlock()
	slog.Warn("agent session Codex MCP server startup failed",
		"event", "agent_session.codex.mcp.startup_failed",
		"error", failure.Error(),
		"grace_ms", defaultCodexAppServerMCPFailureGraceWindow.Milliseconds(),
	)
	time.AfterFunc(defaultCodexAppServerMCPFailureGraceWindow, func() {
		c.failActiveCall(failure)
	})
}

func (c *codexAppServerClient) failActiveCall(err error) {
	if c == nil || c.raw == nil {
		return
	}
	c.scheduler.failActive(err)
	c.raw.failActiveHandler(err)
}

func (c *codexAppServerClient) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	c.scheduler.close()
	err := c.raw.Close()
	if err == nil {
		c.closed = true
	}
	return err
}

func (c *codexAppServerClient) Done() <-chan struct{} {
	if c == nil || c.raw == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return c.raw.Done()
}

func (c *codexAppServerClient) waitForProviderProgress(
	ctx context.Context,
	duration time.Duration,
) error {
	if c == nil || c.raw == nil || c.raw.conn == nil {
		return ErrSessionDisconnected
	}
	if waiter, ok := c.raw.conn.(providerProgressWaiter); ok {
		return waiter.WaitForProviderProgress(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.Done():
		return ErrSessionDisconnected
	case <-timer.C:
		return nil
	}
}

func (c *codexAppServerClient) Err() error {
	if c == nil || c.raw == nil {
		return ErrSessionDisconnected
	}
	return c.raw.Err()
}

func (c *codexAppServerClient) Diagnostics() acpClientDiagnostics {
	if c == nil || c.raw == nil {
		return acpClientDiagnostics{}
	}
	return c.raw.Diagnostics()
}

func (c *codexAppServerClient) Respond(ctx context.Context, id json.RawMessage, result any, responseErr *acpError) error {
	if c == nil || c.raw == nil {
		return errors.New("app-server client is nil")
	}
	_, err := c.scheduler.do(ctx, appServerSchedulerResponseMethod, map[string]any{"id": string(id)}, func(runCtx context.Context) ([]byte, error) {
		return nil, c.raw.Respond(runCtx, id, result, responseErr)
	})
	return err
}

func (c *codexAppServerClient) Initialized(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return errors.New("app-server client is nil")
	}
	return c.raw.Notify(ctx, appServerMethodInitialized, nil)
}

func (c *codexAppServerClient) typed(timeout time.Duration, handler acpMessageHandler, noHandler bool) (*codexproto.Client, *codexAppServerCaller) {
	// App-server connections have one permanent message router installed with
	// SetMessageHandler. Request-local handlers were inherited from the
	// single-session ACP client and made every handler-carrying request claim a
	// global slot for its complete lifetime. That ownership model cannot route
	// notifications from several provider threads. Keep the parameters while
	// callers migrate, but all app-server requests now correlate independently
	// by JSON-RPC request id and the permanent router owns server traffic.
	_ = handler
	_ = noHandler
	caller := &codexAppServerCaller{
		raw:       c.raw,
		scheduler: c.scheduler,
		timeout:   timeout,
	}
	return codexproto.NewClient(caller), caller
}

func (c *codexAppServerClient) wrapHandler(handler acpMessageHandler) acpMessageHandler {
	if handler == nil {
		return nil
	}
	return func(ctx context.Context, message acpMessage) error {
		c.parseInboundMessage(message)
		if router := c.getMessageRouter(); router != nil {
			return router(ctx, message)
		}
		return handler(ctx, message)
	}
}
func (c *codexAppServerClient) parseInboundMessage(message acpMessage) {
	method := strings.TrimSpace(message.Method)
	if method == "" {
		return
	}
	if len(message.ID) > 0 {
		_, err := codexproto.ParseServerRequest(method, message.Params)
		if err != nil {
			slog.Warn("agent session app-server server request parse failed",
				"event", "agent_session.app_server.server_request.parse_failed",
				"method", method,
				"error", err.Error(),
			)
			return
		}
		if !codexproto.IsKnownServerRequestMethod(method) {
			slog.Warn("agent session app-server unknown server request",
				"event", "agent_session.app_server.server_request.unknown",
				"method", method,
			)
		}
		return
	}
	// Notifications are the hot path (token-delta traffic) and the reducer
	// re-decodes params into map[string]any anyway (D7): the typed parse here
	// is schema-drift telemetry only, so run it once per method per client
	// instead of paying a second full decode on every frame.
	if _, seen := c.parsedNotificationMethods.LoadOrStore(method, struct{}{}); seen {
		return
	}
	_, err := codexproto.ParseServerNotification(method, message.Params)
	if err != nil {
		slog.Warn("agent session app-server notification parse failed",
			"event", "agent_session.app_server.notification.parse_failed",
			"method", method,
			"error", err.Error(),
		)
		return
	}
	if !codexproto.IsKnownServerNotificationMethod(method) {
		slog.Warn("agent session app-server unknown notification",
			"event", "agent_session.app_server.notification.unknown",
			"method", method,
		)
	}
}

func (c *codexAppServerCaller) Call(ctx context.Context, method string, params any, result any) error {
	if c.raw == nil {
		return errors.New("app-server client is nil")
	}
	raw, err := c.scheduler.do(ctx, method, params, func(runCtx context.Context) ([]byte, error) {
		return c.raw.CallNoHandlerWithTimeout(runCtx, c.timeout, method, params)
	})
	if err != nil {
		return err
	}
	c.rawResult = append(c.rawResult[:0], raw...)
	if result == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, result); err != nil {
		slog.Warn("agent session app-server typed response decode failed",
			"event", "agent_session.app_server.typed_response.decode_failed",
			"method", method,
			"error", err.Error(),
		)
	}
	return nil
}

func codexProtoParams[T any](params map[string]any) (T, error) {
	var out T
	if params == nil {
		return out, nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *codexAppServerClient) Initialize(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.InitializeParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.Initialize(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) AccountRead(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.GetAccountParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.AccountRead(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ModelList(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ModelListParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.ModelList(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ModelListNoHandler(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ModelListParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, nil, true)
	_, err = client.ModelList(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

// RawCallNoHandler is the narrow connection-global escape hatch for catalog
// methods that are not part of the generated Codex protocol surface. It still
// goes through the shared scheduler, so catalog RPCs cannot race or steal
// response routing from Thread traffic.
func (c *codexAppServerClient) RawCallNoHandler(
	ctx context.Context,
	timeout time.Duration,
	method string,
	params map[string]any,
) (json.RawMessage, error) {
	if c == nil || c.raw == nil {
		return nil, errors.New("app-server client is nil")
	}
	raw, err := c.scheduler.do(ctx, method, params, func(runCtx context.Context) ([]byte, error) {
		return c.raw.CallNoHandlerWithTimeout(runCtx, timeout, method, params)
	})
	return raw, err
}

func (c *codexAppServerClient) AccountRateLimitsRead(
	ctx context.Context,
	timeout time.Duration,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	client, caller := c.typed(timeout, handler, false)
	_, err := client.AccountRateLimitsRead(ctx)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) AccountRateLimitsReadNoHandler(
	ctx context.Context,
	timeout time.Duration,
) (json.RawMessage, error) {
	client, caller := c.typed(timeout, nil, true)
	_, err := client.AccountRateLimitsRead(ctx)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) CollaborationModeList(
	ctx context.Context,
	timeout time.Duration,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	client, caller := c.typed(timeout, handler, false)
	_, err := client.CollaborationModeList(ctx, codexproto.CollaborationModeListParams{})
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) SkillsExtraRootsSet(
	ctx context.Context,
	timeout time.Duration,
	extraRoots []string,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.SkillsExtraRootsSetParams](map[string]any{
		"extraRoots": append([]string(nil), extraRoots...),
	})
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.SkillsExtraRootsSet(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadStart(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadStartParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.ThreadStart(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadResume(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadResumeParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.ThreadResume(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadUnsubscribe(
	ctx context.Context,
	timeout time.Duration,
	threadID string,
) (json.RawMessage, error) {
	client, caller := c.typed(timeout, nil, true)
	_, err := client.ThreadUnsubscribe(ctx, codexproto.ThreadUnsubscribeParams{
		ThreadID: strings.TrimSpace(threadID),
	})
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadFork(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadForkParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.ThreadFork(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

type codexSideThreadForkParams struct {
	ThreadID              string  `json:"threadId"`
	Ephemeral             *bool   `json:"ephemeral,omitempty"`
	ExcludeTurns          *bool   `json:"excludeTurns,omitempty"`
	DeveloperInstructions *string `json:"developerInstructions,omitempty"`
}

func (c *codexAppServerClient) ThreadForkSide(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
	lateResult func(json.RawMessage),
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexSideThreadForkParams](params)
	if err != nil {
		return nil, err
	}
	_ = handler
	return c.raw.CallNoHandlerWithLateResult(
		ctx,
		timeout,
		appServerMethodThreadFork,
		typedParams,
		lateResult,
	)
}

func (c *codexAppServerClient) ThreadInjectItems(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadInjectItemsParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, handler, false)
	_, err = client.ThreadInjectItems(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadUnsubscribeNoHandler(
	ctx context.Context,
	timeout time.Duration,
	threadID string,
) error {
	if c == nil || c.raw == nil {
		return errors.New("app-server client is nil")
	}
	_, err := c.raw.CallNoHandlerWithTimeout(
		ctx,
		timeout,
		appServerMethodThreadUnsubscribe,
		codexproto.ThreadUnsubscribeParams{
			ThreadID: strings.TrimSpace(threadID),
		},
	)
	return err
}

func (c *codexAppServerClient) TurnStart(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.TurnStartParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, nil, false)
	_, err = client.TurnStart(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) TurnSteerNoHandler(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.TurnSteerParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, nil, true)
	_, err = client.TurnSteer(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadCompactStart(
	ctx context.Context,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadCompactStartParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, handler, false)
	_, err = client.ThreadCompactStart(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadGoalSet(
	ctx context.Context,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadGoalSetParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, handler, false)
	_, err = client.ThreadGoalSet(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadGoalGet(
	ctx context.Context,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadGoalGetParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, handler, false)
	_, err = client.ThreadGoalGet(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadGoalClear(
	ctx context.Context,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadGoalClearParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, handler, false)
	_, err = client.ThreadGoalClear(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

// The NoHandler names remain source-compatible with callers that used them
// before the app-server transport gained a permanent message router. All
// app-server requests now use the same concurrent request-id correlation path.

func (c *codexAppServerClient) ThreadGoalSetNoHandler(
	ctx context.Context,
	params map[string]any,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadGoalSetParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, nil, true)
	_, err = client.ThreadGoalSet(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadGoalGetNoHandler(
	ctx context.Context,
	params map[string]any,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadGoalGetParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, nil, true)
	_, err = client.ThreadGoalGet(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadGoalClearNoHandler(
	ctx context.Context,
	params map[string]any,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadGoalClearParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, nil, true)
	_, err = client.ThreadGoalClear(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

// callGoalNoHandler mirrors callGoal for the NoHandler variants.
func (s *codexAppServerSession) callGoalNoHandler(
	ctx context.Context,
	method string,
	params map[string]any,
) (json.RawMessage, error) {
	if s == nil || s.client == nil {
		return nil, ErrSessionDisconnected
	}
	switch method {
	case appServerMethodThreadGoalClear:
		return s.client.ThreadGoalClearNoHandler(ctx, params)
	case appServerMethodThreadGoalGet:
		return s.client.ThreadGoalGetNoHandler(ctx, params)
	case appServerMethodThreadGoalSet:
		return s.client.ThreadGoalSetNoHandler(ctx, params)
	default:
		return nil, errors.New("unsupported app-server goal method")
	}
}

func (s *codexAppServerSession) callGoal(
	ctx context.Context,
	method string,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	if s == nil || s.client == nil {
		return nil, ErrSessionDisconnected
	}
	switch method {
	case appServerMethodThreadGoalClear:
		return s.client.ThreadGoalClear(ctx, params, handler)
	case appServerMethodThreadGoalGet:
		return s.client.ThreadGoalGet(ctx, params, handler)
	case appServerMethodThreadGoalSet:
		return s.client.ThreadGoalSet(ctx, params, handler)
	default:
		return nil, errors.New("unsupported app-server goal method")
	}
}

func (c *codexAppServerClient) ThreadRollback(
	ctx context.Context,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadRollbackParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, handler, false)
	_, err = client.ThreadRollback(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadRollbackNoHandler(
	ctx context.Context,
	timeout time.Duration,
	params map[string]any,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadRollbackParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, nil, true)
	_, err = client.ThreadRollback(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ReviewStart(
	ctx context.Context,
	params map[string]any,
	handler acpMessageHandler,
) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ReviewStartParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(0, handler, false)
	_, err = client.ReviewStart(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) ThreadReadNoHandler(ctx context.Context, timeout time.Duration, params map[string]any) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.ThreadReadParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, nil, true)
	_, err = client.ThreadRead(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}

func (c *codexAppServerClient) TurnInterruptNoHandler(ctx context.Context, timeout time.Duration, params map[string]any) (json.RawMessage, error) {
	typedParams, err := codexProtoParams[codexproto.TurnInterruptParams](params)
	if err != nil {
		return nil, err
	}
	client, caller := c.typed(timeout, nil, true)
	_, err = client.TurnInterrupt(ctx, typedParams)
	if err != nil {
		return nil, err
	}
	return caller.rawResult, nil
}
