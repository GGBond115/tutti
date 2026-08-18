package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
)

const (
	appServerUnknownThreadLimit     = 64
	appServerUnknownThreadByteLimit = 256 * 1024
	appServerUnknownThreadTTL       = 5 * time.Second
	appServerBindingQueueCapacity   = 256
	appServerBindingCriticalReserve = 32
)

type appServerConnectionKey struct {
	Provider             string
	ExecutionHostID      string
	RuntimeGeneration    string
	TransportScopeID     string
	ExecutableIdentity   string
	ProcessProfileDigest string
	CaptureScope         string
}

type appServerConnectionRegistry struct {
	adapter *CodexAppServerAdapter

	mu              sync.Mutex
	next            atomic.Uint64
	byKey           map[appServerConnectionKey]*appServerConnection
	byClient        map[*codexAppServerClient]*appServerConnection
	retired         map[uint64]*appServerConnection
	pendingCleanups []*appServerPendingCleanup
	shuttingDown    bool
}

type appServerConnection struct {
	registry           *appServerConnectionRegistry
	key                appServerConnectionKey
	generation         uint64
	client             *codexAppServerClient
	initializeResult   json.RawMessage
	attachedCheckpoint bool

	mu                     sync.Mutex
	bindingsBySession      map[string]*appServerThreadBinding
	ownerByThread          map[string]string
	replacementByThread    map[string]*appServerThreadBinding
	unknownByThread        map[string][]appServerRoutedMessage
	unknownCount           int
	unknownBytes           int
	unknownDroppedCapacity uint64
	unknownDroppedExpired  uint64
	clock                  func() time.Time
	dead                   bool
	closing                bool
	initializing           acpMessageHandler
	modelsMu               sync.Mutex
	modelsReady            bool
	modelsLoading          chan struct{}
	models                 []map[string]any
	bindingCleanupMu       sync.Mutex
	retiredThreadCleanups  map[*appServerThreadBinding]struct{}
	// replacementPublishHook is a test-only barrier immediately before a
	// provisional replacement is published.
	replacementPublishHook func()
}

type appServerThreadBinding struct {
	connection     *appServerConnection
	agentSessionID string
	generation     uint64

	mu               sync.Mutex
	runtimeSession   Session
	expectedThreadID string
	providerThreadID string
	replayedUsage    acpUsageState
	replayedKnown    bool
	closed           bool
	threadCleanup    func(context.Context) error
	startupTrace     *codexAppServerStartupTrace
	overlay          AppServerThreadOverlay
	replacementOf    atomic.Pointer[appServerThreadBinding]
	queue            []appServerRoutedMessage
	droppedMessages  uint64
	overloaded       bool
	wake             chan struct{}
	done             chan struct{}
}

type appServerRoutedMessage struct {
	ctx        context.Context
	message    acpMessage
	barrier    chan struct{}
	receivedAt time.Time
}

type appServerUnknownThreadTelemetry struct {
	BufferedCount   int
	BufferedBytes   int
	DroppedCapacity uint64
	DroppedExpired  uint64
}

type appServerPendingCleanup struct {
	cleanup func(context.Context) error
}

func (c *appServerConnection) modelList(
	ctx context.Context,
	trace *codexAppServerStartupTrace,
) []map[string]any {
	if c == nil || c.client == nil {
		return nil
	}
	c.modelsMu.Lock()
	if c.modelsReady {
		models := cloneCodexAppServerModels(c.models)
		c.modelsMu.Unlock()
		return models
	}
	if loading := c.modelsLoading; loading != nil {
		c.modelsMu.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case <-loading:
		}
		c.modelsMu.Lock()
		models := cloneCodexAppServerModels(c.models)
		c.modelsMu.Unlock()
		return models
	}
	loading := make(chan struct{})
	c.modelsLoading = loading
	c.modelsMu.Unlock()

	result, err := c.client.ModelListNoHandler(ctx, acpStartCallTimeout, map[string]any{})
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err == nil {
		err = json.Unmarshal(result, &payload)
	}
	c.modelsMu.Lock()
	if err == nil {
		c.models = cloneCodexAppServerModels(payload.Data)
		c.modelsReady = true
	}
	c.modelsLoading = nil
	close(loading)
	models := cloneCodexAppServerModels(c.models)
	c.modelsMu.Unlock()
	if trace != nil {
		trace.Log("models.coalesced", map[string]any{"count": len(models), "error": acpProtocolErrorMessage(err)})
	}
	return models
}

func (c *appServerConnection) registerBinding(
	session Session,
	expectedThreadID string,
	overlay AppServerThreadOverlay,
	threadCleanup func(context.Context) error,
) (*appServerThreadBinding, error) {
	if c == nil {
		return nil, errors.New("app-server connection is nil")
	}
	sessionID := strings.TrimSpace(session.AgentSessionID)
	if sessionID == "" {
		return nil, errors.New("app-server binding requires agent session id")
	}
	binding := &appServerThreadBinding{
		connection:       c,
		agentSessionID:   sessionID,
		generation:       c.generation,
		runtimeSession:   session,
		expectedThreadID: strings.TrimSpace(expectedThreadID),
		threadCleanup:    threadCleanup,
		overlay:          overlay,
		wake:             make(chan struct{}, 1),
		done:             make(chan struct{}),
	}
	c.mu.Lock()
	if c.dead || c.closing {
		c.mu.Unlock()
		return nil, errors.New("app-server connection is not healthy")
	}
	if existing := c.bindingsBySession[sessionID]; existing != nil {
		if binding.expectedThreadID == "" ||
			strings.TrimSpace(existing.providerThreadID) != binding.expectedThreadID {
			c.mu.Unlock()
			return nil, fmt.Errorf("app-server session %s is already bound", sessionID)
		}
		binding.replacementOf.Store(existing)
		if c.replacementByThread == nil {
			c.replacementByThread = make(map[string]*appServerThreadBinding)
		}
		if c.replacementByThread[binding.expectedThreadID] != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("app-server thread %s already has a provisional replacement", binding.expectedThreadID)
		}
		c.replacementByThread[binding.expectedThreadID] = binding
		c.mu.Unlock()
		go binding.run()
		return binding, nil
	}
	if binding.expectedThreadID != "" {
		if owner := c.ownerByThread[binding.expectedThreadID]; owner != "" && owner != sessionID {
			c.mu.Unlock()
			return nil, fmt.Errorf("app-server thread %s is already owned by another session", binding.expectedThreadID)
		}
		c.ownerByThread[binding.expectedThreadID] = sessionID
		binding.providerThreadID = binding.expectedThreadID
	}
	c.bindingsBySession[sessionID] = binding
	early := c.takeUnknownLocked(binding.expectedThreadID)
	c.mu.Unlock()
	go binding.run()
	for _, routed := range early {
		binding.enqueue(routed)
	}
	return binding, nil
}

func (c *appServerConnection) bindThread(binding *appServerThreadBinding, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if c == nil || binding == nil || threadID == "" {
		return errors.New("app-server thread binding requires a thread id")
	}
	c.mu.Lock()
	current := c.bindingsBySession[binding.agentSessionID]
	if c.dead || binding.generation != c.generation ||
		(current != binding && binding.replacementOf.Load() != current) {
		c.mu.Unlock()
		return errors.New("app-server binding generation is stale")
	}
	if binding.expectedThreadID != "" && binding.expectedThreadID != threadID {
		c.mu.Unlock()
		return fmt.Errorf("app-server resumed thread %s, want %s", threadID, binding.expectedThreadID)
	}
	if owner := c.ownerByThread[threadID]; owner != "" && owner != binding.agentSessionID {
		c.mu.Unlock()
		return fmt.Errorf("app-server thread %s is already owned by another session", threadID)
	}
	if binding.replacementOf.Load() == nil {
		c.ownerByThread[threadID] = binding.agentSessionID
	}
	binding.mu.Lock()
	binding.providerThreadID = threadID
	binding.mu.Unlock()
	early := c.takeUnknownLocked(threadID)
	c.mu.Unlock()
	for _, routed := range early {
		binding.enqueue(routed)
	}
	return nil
}

func (c *appServerConnection) bindChildThreads(binding *appServerThreadBinding, threadIDs []string) {
	if c == nil || binding == nil || len(threadIDs) == 0 {
		return
	}
	c.mu.Lock()
	if c.dead || binding.generation != c.generation || c.bindingsBySession[binding.agentSessionID] != binding {
		c.mu.Unlock()
		return
	}
	var early []appServerRoutedMessage
	for _, value := range threadIDs {
		threadID := strings.TrimSpace(value)
		if threadID == "" {
			continue
		}
		if owner := c.ownerByThread[threadID]; owner != "" && owner != binding.agentSessionID {
			continue
		}
		c.ownerByThread[threadID] = binding.agentSessionID
		early = append(early, c.takeUnknownLocked(threadID)...)
	}
	c.mu.Unlock()
	for _, routed := range early {
		binding.enqueue(routed)
	}
}

func (c *appServerConnection) commitBindingReplacement(binding *appServerThreadBinding) error {
	if c == nil || binding == nil || binding.replacementOf.Load() == nil {
		return nil
	}
	previous := binding.replacementOf.Load()
	c.mu.Lock()
	if c.dead || c.generation != binding.generation ||
		c.bindingsBySession[binding.agentSessionID] != previous {
		c.mu.Unlock()
		return errors.New("app-server replacement binding is stale")
	}
	previous.mu.Lock()
	binding.mu.Lock()
	binding.replayedUsage = mergeACPUsageState(previous.replayedUsage, binding.replayedUsage)
	binding.replayedKnown = previous.replayedKnown || binding.replayedKnown
	binding.mu.Unlock()
	previous.mu.Unlock()
	c.bindingsBySession[binding.agentSessionID] = binding
	for threadID, owner := range c.ownerByThread {
		if owner == binding.agentSessionID {
			delete(c.ownerByThread, threadID)
		}
	}
	c.ownerByThread[binding.providerThreadID] = binding.agentSessionID
	c.mu.Unlock()
	return nil
}

func (c *appServerConnection) publishBindingReplacement(binding *appServerThreadBinding) {
	if c == nil || binding == nil {
		return
	}
	c.mu.Lock()
	previous := binding.replacementOf.Load()
	if previous == nil || c.bindingsBySession[binding.agentSessionID] != binding {
		c.mu.Unlock()
		return
	}
	delete(c.replacementByThread, binding.providerThreadID)
	if c.replacementPublishHook != nil {
		c.replacementPublishHook()
	}
	binding.replacementOf.Store(nil)
	c.mu.Unlock()
	_ = c.closeBinding(previous)
}

func (c *appServerConnection) detachBinding(binding *appServerThreadBinding) error {
	if c == nil || binding == nil {
		return nil
	}
	c.mu.Lock()
	installed := c.bindingsBySession[binding.agentSessionID] == binding
	if binding.replacementOf.Load() != nil && c.replacementByThread[binding.expectedThreadID] == binding {
		delete(c.replacementByThread, binding.expectedThreadID)
	}
	if installed {
		delete(c.bindingsBySession, binding.agentSessionID)
		for threadID, owner := range c.ownerByThread {
			if owner == binding.agentSessionID {
				delete(c.ownerByThread, threadID)
			}
		}
	}
	c.mu.Unlock()
	cleanupErr := c.closeBinding(binding)
	return cleanupErr
}

func (c *appServerConnection) bindingForStartup(agentSessionID, expectedThreadID string) *appServerThreadBinding {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if expectedThreadID = strings.TrimSpace(expectedThreadID); expectedThreadID != "" {
		if provisional := c.replacementByThread[expectedThreadID]; provisional != nil &&
			provisional.agentSessionID == strings.TrimSpace(agentSessionID) {
			return provisional
		}
	}
	return c.bindingsBySession[strings.TrimSpace(agentSessionID)]
}

func (c *appServerConnection) route(ctx context.Context, message acpMessage) error {
	if c == nil {
		return nil
	}
	routed := appServerRoutedMessage{ctx: ctx, message: message, receivedAt: c.nowTime()}
	threadID := appServerMessageThreadID(message)
	c.mu.Lock()
	if c.dead || c.closing {
		c.mu.Unlock()
		return nil
	}
	if threadID != "" {
		owner := c.ownerByThread[threadID]
		binding := c.bindingsBySession[owner]
		if replacement := c.replacementByThread[threadID]; replacement != nil {
			binding = replacement
		}
		if binding == nil {
			c.bufferUnknownLocked(threadID, routed)
			c.mu.Unlock()
			return nil
		}
		if binding.generation != c.generation {
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()
		binding.captureReplayUsage(message)
		binding.enqueue(routed)
		return nil
	}
	if c.initializing != nil {
		observer := c.initializing
		c.mu.Unlock()
		return observer(ctx, message)
	}
	bindings := make([]*appServerThreadBinding, 0, len(c.bindingsBySession))
	for _, binding := range c.bindingsBySession {
		bindings = append(bindings, binding)
	}
	c.mu.Unlock()
	if len(message.ID) > 0 && message.Method != "" && len(bindings) != 1 {
		_ = c.client.raw.Respond(context.Background(), message.ID, nil, &acpError{
			Code: -32602, Message: "app-server request has no unambiguous thread owner",
		})
		return nil
	}
	for _, binding := range bindings {
		binding.captureReplayUsage(message)
		binding.enqueue(routed)
	}
	return nil
}

func (c *appServerConnection) setInitializingObserver(observer acpMessageHandler) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.initializing = observer
	c.mu.Unlock()
}

func (c *appServerConnection) bufferUnknownLocked(threadID string, routed appServerRoutedMessage) {
	if threadID == "" {
		return
	}
	now := routed.receivedAt
	if now.IsZero() {
		now = c.nowTime()
		routed.receivedAt = now
	}
	c.pruneExpiredUnknownLocked(now)
	size := appServerRoutedMessageSize(routed)
	if c.unknownCount >= appServerUnknownThreadLimit || size > appServerUnknownThreadByteLimit-c.unknownBytes {
		c.unknownDroppedCapacity++
		return
	}
	c.unknownByThread[threadID] = append(c.unknownByThread[threadID], routed)
	c.unknownCount++
	c.unknownBytes += size
}

func (c *appServerConnection) takeUnknownLocked(threadID string) []appServerRoutedMessage {
	if threadID == "" {
		return nil
	}
	c.pruneExpiredUnknownLocked(c.nowTime())
	values := c.unknownByThread[threadID]
	delete(c.unknownByThread, threadID)
	c.unknownCount -= len(values)
	for _, routed := range values {
		c.unknownBytes -= appServerRoutedMessageSize(routed)
	}
	if c.unknownCount < 0 {
		c.unknownCount = 0
	}
	if c.unknownBytes < 0 {
		c.unknownBytes = 0
	}
	return values
}

func (c *appServerConnection) pruneExpiredUnknownLocked(now time.Time) {
	if c == nil || now.IsZero() {
		return
	}
	cutoff := now.Add(-appServerUnknownThreadTTL)
	for threadID, values := range c.unknownByThread {
		kept := values[:0]
		for _, routed := range values {
			if routed.receivedAt.IsZero() || routed.receivedAt.After(cutoff) {
				kept = append(kept, routed)
				continue
			}
			c.unknownCount--
			c.unknownBytes -= appServerRoutedMessageSize(routed)
			c.unknownDroppedExpired++
		}
		if len(kept) == 0 {
			delete(c.unknownByThread, threadID)
		} else {
			c.unknownByThread[threadID] = kept
		}
	}
	if c.unknownCount < 0 {
		c.unknownCount = 0
	}
	if c.unknownBytes < 0 {
		c.unknownBytes = 0
	}
}

func (c *appServerConnection) nowTime() time.Time {
	if c != nil && c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

func appServerRoutedMessageSize(routed appServerRoutedMessage) int {
	return len(routed.message.ID) + len(routed.message.Method) + len(routed.message.Params)
}

func (c *appServerConnection) unknownThreadTelemetry() appServerUnknownThreadTelemetry {
	if c == nil {
		return appServerUnknownThreadTelemetry{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneExpiredUnknownLocked(c.nowTime())
	return appServerUnknownThreadTelemetry{
		BufferedCount: c.unknownCount, BufferedBytes: c.unknownBytes,
		DroppedCapacity: c.unknownDroppedCapacity, DroppedExpired: c.unknownDroppedExpired,
	}
}

func (b *appServerThreadBinding) enqueue(message appServerRoutedMessage) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	critical := appServerMessageCritical(message)
	normalLimit := appServerBindingQueueCapacity - appServerBindingCriticalReserve
	if len(b.queue) >= normalLimit && !critical {
		b.droppedMessages++
		firstOverload := !b.overloaded
		b.overloaded = true
		b.mu.Unlock()
		if firstOverload && b.connection != nil {
			go b.connection.forceClose()
		}
		return
	}
	if len(b.queue) >= appServerBindingQueueCapacity {
		b.droppedMessages++
		firstOverload := !b.overloaded
		b.overloaded = true
		b.mu.Unlock()
		if firstOverload && b.connection != nil {
			go b.connection.forceClose()
		}
		return
	}
	b.queue = append(b.queue, message)
	b.mu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func appServerMessageCritical(routed appServerRoutedMessage) bool {
	if routed.barrier != nil {
		return true
	}
	message := routed.message
	if len(message.ID) > 0 && message.Method != "" {
		return true
	}
	switch message.Method {
	case appServerNotifyTurnStarted, appServerNotifyTurnCompleted,
		appServerNotifyItemStarted, appServerNotifyItemCompleted,
		appServerNotifyError, appServerNotifyThreadStarted:
		return true
	default:
		return false
	}
}

func (b *appServerThreadBinding) run() {
	for {
		select {
		case <-b.done:
			return
		case <-b.wake:
			for {
				b.mu.Lock()
				if len(b.queue) == 0 {
					b.mu.Unlock()
					break
				}
				routed := b.queue[0]
				b.queue[0] = appServerRoutedMessage{}
				b.queue = b.queue[1:]
				b.mu.Unlock()
				if routed.barrier != nil {
					close(routed.barrier)
					continue
				}
				if b.connection == nil || b.connection.registry == nil || b.connection.registry.adapter == nil {
					continue
				}
				b.connection.registry.adapter.dispatchAppServerBindingMessage(b, routed)
			}
		}
	}
}

func (b *appServerThreadBinding) flush(ctx context.Context) error {
	if b == nil {
		return nil
	}
	barrier := make(chan struct{})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		return ErrSessionDisconnected
	default:
	}
	b.enqueue(appServerRoutedMessage{barrier: barrier})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.done:
		return ErrSessionDisconnected
	case <-barrier:
		return nil
	}
}

func (b *appServerThreadBinding) closeMailbox() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.done)
	}
	b.mu.Unlock()
}

func (c *appServerConnection) closeBinding(binding *appServerThreadBinding) error {
	if binding == nil {
		return nil
	}
	binding.closeMailbox()
	err := cleanupPreparedLaunch(binding.threadCleanup)
	c.mu.Lock()
	if err != nil {
		if c.retiredThreadCleanups == nil {
			c.retiredThreadCleanups = make(map[*appServerThreadBinding]struct{})
		}
		c.retiredThreadCleanups[binding] = struct{}{}
	} else {
		delete(c.retiredThreadCleanups, binding)
	}
	c.mu.Unlock()
	return err
}

func (b *appServerThreadBinding) snapshot() (Session, acpUsageState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.runtimeSession, b.replayedUsage, b.replayedKnown
}

func (b *appServerThreadBinding) captureReplayUsage(message acpMessage) {
	if b == nil || message.Method != appServerNotifyTokenUsage || len(message.Params) == 0 {
		return
	}
	params := map[string]any{}
	if json.Unmarshal(message.Params, &params) != nil {
		return
	}
	usage, ok := appServerTokenUsageState(params)
	if !ok {
		return
	}
	b.mu.Lock()
	b.replayedUsage = mergeACPUsageState(b.replayedUsage, usage)
	b.replayedKnown = true
	b.mu.Unlock()
}

func appServerMessageThreadID(message acpMessage) string {
	if len(message.Params) == 0 {
		return ""
	}
	var params map[string]any
	if json.Unmarshal(message.Params, &params) != nil {
		return ""
	}
	if threadID := strings.TrimSpace(asString(params["threadId"])); threadID != "" {
		return threadID
	}
	if thread, ok := params["thread"].(map[string]any); ok {
		return strings.TrimSpace(asString(thread["id"]))
	}
	return ""
}

func (a *CodexAppServerAdapter) dispatchAppServerBindingMessage(
	binding *appServerThreadBinding,
	routed appServerRoutedMessage,
) {
	if a == nil || binding == nil || binding.connection == nil {
		return
	}
	connection := binding.connection
	if binding.generation != connection.generation {
		return
	}
	binding.captureReplayUsage(routed.message)
	if a.appServerReplacementReadHook != nil {
		a.appServerReplacementReadHook()
	}
	replacement := binding.replacementOf.Load()
	if replacement != nil && routed.message.Method == appServerNotifyTokenUsage {
		// Resume replay belongs to the provisional Binding. Applying it to the
		// still-published old Session creates a race and can overwrite the replay
		// snapshot during the atomic swap.
		return
	}
	currentBinding, currentConnection, _ := a.sessionBindingSnapshot(binding.agentSessionID)
	if currentBinding == nil ||
		(currentBinding != binding && replacement != currentBinding) ||
		currentConnection != connection {
		return
	}
	if a.appServerDispatchAcceptedHook != nil {
		a.appServerDispatchAcceptedHook(binding, routed.message)
	}
	session, _, _ := binding.snapshot()
	if binding.startupTrace != nil {
		binding.startupTrace.LogMessage(
			routed.message.Method,
			len(routed.message.ID) > 0,
			len(routed.message.Params),
		)
	}
	endInputUnit := a.inputUnits.begin(routed.ctx, binding.agentSessionID)
	defer endInputUnit()
	_, _, currentThreadID := a.sessionBindingSnapshot(binding.agentSessionID)
	session.ProviderSessionID = firstNonEmpty(currentThreadID, session.ProviderSessionID)
	turnID := ""
	var normalizer *acpTurnNormalizer
	var turnEmit func([]activityshared.Event)
	var turnEmitCommands CommandSnapshotSink
	if activeTurn := a.sessionActiveTurn(binding.agentSessionID); activeTurn != nil {
		session = activeTurn.session
		turnID = activeTurn.turnID
		normalizer = activeTurn.normalizer
		turnEmit = activeTurn.emit
		turnEmitCommands = activeTurn.emitCommands
	}
	events, err := a.handleAppServerMessage(
		routed.ctx,
		connection.client,
		session,
		turnID,
		routed.message,
		normalizer,
		turnEmit,
		turnEmitCommands,
	)
	events = a.inputUnits.stamp(binding.agentSessionID, events)
	turnEvents, detachedChildEvents := appServerEventsForActiveRootTurn(
		binding.agentSessionID,
		turnID,
		events,
	)
	if turnEmit != nil {
		turnEmit(turnEvents)
	}
	if len(detachedChildEvents) > 0 {
		a.emitSessionEvents(
			binding.agentSessionID,
			a.stampTurnLifecycleSnapshots(binding.agentSessionID, detachedChildEvents),
		)
	}
	if err != nil && len(routed.message.ID) > 0 && routed.message.Method != "" {
		_ = connection.client.raw.Respond(context.Background(), routed.message.ID, nil, &acpError{
			Code: -32603, Message: err.Error(),
		})
	}
}

func (a *CodexAppServerAdapter) handleAppServerConnectionDeath(
	connection *appServerConnection,
	bindings []*appServerThreadBinding,
) {
	if a == nil || connection == nil {
		return
	}
	for _, binding := range bindings {
		if binding == nil || binding.generation != connection.generation {
			continue
		}
		a.rejectPendingRequests(binding.agentSessionID, ErrSessionDisconnected)
	}
}
