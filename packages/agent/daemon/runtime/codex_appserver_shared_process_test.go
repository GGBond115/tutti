package agentruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
	"github.com/tutti-os/tutti/packages/agent/daemon/runtime/codexproto"
)

const sharedAppServerHelperEnv = "TUTTI_TEST_SHARED_APP_SERVER_HELPER"
const sharedAppServerWedgedInterruptEnv = "TUTTI_TEST_SHARED_APP_SERVER_WEDGED_INTERRUPT"
const sharedAppServerAutoCompleteEnv = "TUTTI_TEST_SHARED_APP_SERVER_AUTO_COMPLETE"

type countingProcessTransport struct {
	inner    ProcessTransport
	mu       sync.Mutex
	starts   int
	sent     chan sharedObservedRequest
	requests []sharedObservedRequest
	specs    []ProcessSpec
}

type trackingKeyTransport struct{ tracked bool }

func (trackingKeyTransport) Start(context.Context, ProcessSpec) (ProcessConnection, error) {
	return nil, errors.New("not used")
}

func (t trackingKeyTransport) TracksProviderInputUnits() bool { return t.tracked }

type sharedObservedRequest struct {
	method   string
	threadID string
}

type observedProcessConnection struct {
	ProcessConnection
	sent   chan sharedObservedRequest
	record func(sharedObservedRequest)
}

func (c observedProcessConnection) Send(raw []byte) error {
	var request sharedHelperRequest
	if json.Unmarshal(raw, &request) == nil && request.Method != "" && c.sent != nil {
		observed := sharedObservedRequest{method: request.Method, threadID: asString(request.Params["threadId"])}
		if c.record != nil {
			c.record(observed)
		}
		select {
		case c.sent <- observed:
		default:
		}
	}
	return c.ProcessConnection.Send(raw)
}

func (t *countingProcessTransport) Start(ctx context.Context, spec ProcessSpec) (ProcessConnection, error) {
	t.mu.Lock()
	t.starts++
	t.specs = append(t.specs, spec)
	t.mu.Unlock()
	connection, err := t.inner.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	return observedProcessConnection{ProcessConnection: connection, sent: t.sent, record: t.record}, nil
}

func (t *countingProcessTransport) record(request sharedObservedRequest) {
	t.mu.Lock()
	t.requests = append(t.requests, request)
	t.mu.Unlock()
}

func (t *countingProcessTransport) methodCount(method string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := 0
	for _, request := range t.requests {
		if request.method == method {
			count++
		}
	}
	return count
}

func (t *countingProcessTransport) startCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.starts
}

func (t *countingProcessTransport) firstSpec() ProcessSpec {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.specs[0]
}

func TestAppServerConnectionKeySeparatesIncompatibleProfiles(t *testing.T) {
	t.Parallel()
	base := appServerPreparedLaunch{
		spec: ProcessSpec{
			Provider: "codex", AgentSessionID: "session-a", RootAgentSessionID: "root-a", RoomID: "room-a",
			ExecutableIdentity: &ExecutableIdentity{SHA256: "executable-a", SizeBytes: 1},
		},
		profile: &AppServerProcessProfile{
			ExecutionHostID: "host-a", RuntimeGeneration: "runtime-a",
			TransportScopeID: "mount-a", ProcessProfileDigest: "profile-a",
		},
	}
	baseKey, err := appServerConnectionKeyForLaunch(nil, base)
	if err != nil {
		t.Fatalf("base key: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*appServerPreparedLaunch)
	}{
		{"provider", func(value *appServerPreparedLaunch) { value.spec.Provider = "tutti-agent" }},
		{"execution host", func(value *appServerPreparedLaunch) { value.profile.ExecutionHostID = "host-b" }},
		{"runtime generation", func(value *appServerPreparedLaunch) { value.profile.RuntimeGeneration = "runtime-b" }},
		{"transport scope", func(value *appServerPreparedLaunch) { value.profile.TransportScopeID = "mount-b" }},
		{"process profile", func(value *appServerPreparedLaunch) { value.profile.ProcessProfileDigest = "profile-b" }},
		{"launch command", func(value *appServerPreparedLaunch) { value.spec.Command = []string{"other", "app-server"} }},
		{"launch env", func(value *appServerPreparedLaunch) { value.spec.Env = []string{"PROFILE=value"} }},
		{"launch cwd", func(value *appServerPreparedLaunch) { value.spec.CWD = t.TempDir() }},
		{"executable identity", func(value *appServerPreparedLaunch) {
			value.spec.ExecutableIdentity = &ExecutableIdentity{SHA256: "executable-b", SizeBytes: 1}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			profile := *base.profile
			value.profile = &profile
			test.mutate(&value)
			got, err := appServerConnectionKeyForLaunch(nil, value)
			if err != nil {
				t.Fatalf("key: %v", err)
			}
			if got == baseKey {
				t.Fatalf("key = %#v, want separation from %#v", got, baseKey)
			}
		})
	}
	trackedA, err := appServerConnectionKeyForLaunch(trackingKeyTransport{tracked: true}, base)
	if err != nil {
		t.Fatalf("tracked A key: %v", err)
	}
	captureB := base
	captureB.spec.RootAgentSessionID = "root-b"
	trackedB, err := appServerConnectionKeyForLaunch(trackingKeyTransport{tracked: true}, captureB)
	if err != nil {
		t.Fatalf("tracked B key: %v", err)
	}
	if trackedA == trackedB || trackedA.CaptureScope != "root-a" || trackedB.CaptureScope != "root-b" {
		t.Fatalf("capture keys did not separate root recordings: A=%#v B=%#v", trackedA, trackedB)
	}
}

func TestAppServerConnectionKeysRejectMissingProcessProfile(t *testing.T) {
	launch := appServerPreparedLaunch{spec: ProcessSpec{Provider: ProviderCodex}}
	if _, err := appServerConnectionKeyForLaunch(nil, launch); err == nil ||
		!strings.Contains(err.Error(), "explicit process profile") {
		t.Fatalf("launch key error = %v, want explicit process profile error", err)
	}
	if _, err := keyForUnregisteredConnection(launch); err == nil ||
		!strings.Contains(err.Error(), "explicit process profile") {
		t.Fatalf("unregistered key error = %v, want explicit process profile error", err)
	}
}

func TestAppServerProcessProfileDigestUsesPlatformEffectiveEnvironment(t *testing.T) {
	command := []string{"codex", "app-server"}
	if first, second :=
		appServerProcessProfileDigestForPlatform(command, []string{"A=1", "A=2"}, "/cwd", "linux"),
		appServerProcessProfileDigestForPlatform(command, []string{"A=2", "A=1"}, "/cwd", "linux"); first == second {
		t.Fatal("POSIX duplicate environment order with different last-wins values produced one digest")
	}
	if first, second :=
		appServerProcessProfileDigestForPlatform(command, []string{"A=0", "A=2"}, "/cwd", "linux"),
		appServerProcessProfileDigestForPlatform(command, []string{"A=2"}, "/cwd", "linux"); first != second {
		t.Fatal("equivalent POSIX last-wins environments produced different digests")
	}
	if upper, mixed :=
		appServerProcessProfileDigestForPlatform(command, []string{"PATH=one"}, `C:\\cwd`, "windows"),
		appServerProcessProfileDigestForPlatform(command, []string{"Path=one"}, `C:\\cwd`, "windows"); upper != mixed {
		t.Fatal("Windows case-insensitive environment keys produced different digests")
	}
	if upper, mixed :=
		appServerProcessProfileDigestForPlatform(command, []string{"PATH=one"}, "/cwd", "linux"),
		appServerProcessProfileDigestForPlatform(command, []string{"Path=one"}, "/cwd", "linux"); upper == mixed {
		t.Fatal("POSIX case-sensitive environment keys produced one digest")
	}
}

func TestAppServerPreparationKeepsProcessAndProtocolCWDSeparate(t *testing.T) {
	processCWD := "/runtime/process"
	protocolCWD := "/workspace/project"
	adapter := NewCodexAppServerAdapter(
		trackingKeyTransport{},
	)
	session := testAppServerSession()
	session.CWD = protocolCWD
	session.AppServer = testAppServerRuntimePreparation(processCWD)
	launch, err := adapter.prepareInitializedClientLaunch(context.Background(), session)
	if err != nil {
		t.Fatalf("prepareInitializedClientLaunch: %v", err)
	}
	if launch.spec.CWD != processCWD {
		t.Fatalf("process cwd = %q, want %q", launch.spec.CWD, processCWD)
	}
	if launch.spec.ProtocolCWD != protocolCWD {
		t.Fatalf("protocol cwd = %q, want %q", launch.spec.ProtocolCWD, protocolCWD)
	}
}

func TestAppServerProcessProfileFailsClosedWithoutCompatibilityIdentity(t *testing.T) {
	base := AppServerProcessProfile{
		ExecutionHostID: "host-a", RuntimeGeneration: "runtime-a", TransportScopeID: "scope-a",
		ProcessProfileDigest: "profile-a", Command: []string{"codex", "app-server"}, CWD: t.TempDir(),
	}
	tests := []struct {
		name   string
		mutate func(*AppServerProcessProfile)
	}{
		{"execution host", func(value *AppServerProcessProfile) { value.ExecutionHostID = "" }},
		{"runtime generation", func(value *AppServerProcessProfile) { value.RuntimeGeneration = "" }},
		{"transport scope", func(value *AppServerProcessProfile) { value.TransportScopeID = "" }},
		{"profile digest", func(value *AppServerProcessProfile) { value.ProcessProfileDigest = "" }},
		{"command", func(value *AppServerProcessProfile) { value.Command = nil }},
		{"cwd", func(value *AppServerProcessProfile) { value.CWD = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			if err := validateAppServerProcessProfile(value); err == nil {
				t.Fatal("profile without compatibility identity unexpectedly validated")
			}
		})
	}
}

func TestTuttiAgentSharedPreparationKeepsSkillRootHandoffOutOfProviderEnvironments(t *testing.T) {
	adapter := NewTuttiAgentAppServerAdapterWithHostMetadata(trackingKeyTransport{}, LegacyHostMetadata())
	adapter.SetProviderLaunchPreparer(func(context.Context, ProviderLaunchPrepareInput) (ProviderLaunchPrepareResult, error) {
		return ProviderLaunchPrepareResult{AppServer: &AppServerLaunchPreparation{
			ProcessProfile: AppServerProcessProfile{
				ExecutionHostID: "host-a", RuntimeGeneration: "runtime-a", TransportScopeID: "scope-a",
				ProcessProfileDigest: "profile-a", Command: []string{"tutti-agent", "app-server"},
				Env: []string{
					tuttiAgentExtraSkillRootsEnv + `=["/internal/extra"]`,
					tuttiAgentStableSystemSkillsEnv + "=/internal/system", "PROCESS_OK=1",
				}, CWD: t.TempDir(),
			},
			ThreadOverlay: AppServerThreadOverlay{Env: []string{
				tuttiAgentExtraSkillRootsEnv + `=["/internal/extra"]`,
				tuttiAgentStableSystemSkillsEnv + "=/internal/system", "THREAD_OK=1",
			}},
		}}, nil
	})
	launch, err := adapter.prepareInitializedClientLaunch(t.Context(), Session{
		Provider: ProviderTuttiAgent, AgentSessionID: "session-a", CWD: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("prepareInitializedClientLaunch() error = %v", err)
	}
	for name, values := range map[string][]string{"process": launch.spec.Env, "thread": launch.overlay.Env} {
		for _, entry := range values {
			key := strings.SplitN(entry, "=", 2)[0]
			if key == tuttiAgentExtraSkillRootsEnv || key == tuttiAgentStableSystemSkillsEnv {
				t.Fatalf("%s environment leaked internal handoff %q", name, key)
			}
		}
	}
	if !environmentContainsExact(launch.spec.Env, "PROCESS_OK=1") ||
		!environmentContainsExact(launch.overlay.Env, "THREAD_OK=1") {
		t.Fatalf("preparation removed ordinary environment: process=%#v thread=%#v", launch.spec.Env, launch.overlay.Env)
	}
}

func TestAppServerThreadOverlaySerializesModelProviderBearerTokenForStartAndResume(t *testing.T) {
	overlay := AppServerThreadOverlay{ModelProviderCredentials: []AppServerModelProviderCredential{
		{ModelProviderID: "tutti_model_plan", BearerToken: "session-token"},
	}}
	base := appServerThreadStartParams(Session{CWD: t.TempDir()}, t.TempDir())
	config, _ := base["config"].(map[string]any)
	if config == nil {
		config = make(map[string]any)
		base["config"] = config
	}
	config["model_providers"] = map[string]any{
		"tutti_model_plan": map[string]any{"env_key": "TUTTI_MODEL_PLAN_API_KEY", "wire_api": "responses"},
	}
	applyAppServerThreadOverlay(base, overlay)
	assertSerialized := func(t *testing.T, raw []byte) {
		t.Helper()
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		serializedConfig := payloadObject(payload["config"])
		providers := payloadObject(serializedConfig["model_providers"])
		provider := payloadObject(providers["tutti_model_plan"])
		if provider["experimental_bearer_token"] != "session-token" || provider["env_key"] != nil || provider["wire_api"] != "responses" {
			t.Fatalf("serialized provider credential = %#v", provider)
		}
	}
	startParams, err := codexProtoParams[codexproto.ThreadStartParams](base)
	if err != nil {
		t.Fatal(err)
	}
	startRaw, _ := json.Marshal(startParams)
	assertSerialized(t, startRaw)
	base["threadId"] = "thread-a"
	resumeParams, err := codexProtoParams[codexproto.ThreadResumeParams](base)
	if err != nil {
		t.Fatal(err)
	}
	resumeRaw, _ := json.Marshal(resumeParams)
	assertSerialized(t, resumeRaw)
}

func environmentContainsExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAppServerBindingMailboxOverloadExplicitlyRetiresConnection(t *testing.T) {
	physical := newBarrierModelListConnection()
	client := newCodexAppServerClient(physical)
	connection := &appServerConnection{client: client}
	binding := &appServerThreadBinding{
		connection: connection, wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	for index := 0; index < appServerBindingQueueCapacity-appServerBindingCriticalReserve; index++ {
		binding.enqueue(appServerRoutedMessage{message: acpMessage{Method: appServerNotifyAgentMessageDelta}})
	}
	binding.enqueue(appServerRoutedMessage{message: acpMessage{Method: appServerNotifyAgentMessageDelta}})
	binding.mu.Lock()
	queued, dropped, overloaded := len(binding.queue), binding.droppedMessages, binding.overloaded
	binding.mu.Unlock()
	if queued != appServerBindingQueueCapacity-appServerBindingCriticalReserve {
		t.Fatalf("mailbox length = %d, want bounded normal capacity", queued)
	}
	if dropped != 1 || !overloaded {
		t.Fatalf("mailbox overload = dropped %d, overloaded %v", dropped, overloaded)
	}
	select {
	case <-physical.done:
	case <-time.After(time.Second):
		t.Fatal("mailbox overload silently dropped transcript data without retiring the connection")
	}
}

func TestAppServerUnknownThreadBufferBoundsCountBytesAndExpiry(t *testing.T) {
	now := time.Unix(1_000, 0)
	connection := &appServerConnection{
		unknownByThread: make(map[string][]appServerRoutedMessage), clock: func() time.Time { return now },
	}
	for index := 0; index < appServerUnknownThreadLimit+1; index++ {
		threadID := fmt.Sprintf("thread-%d", index)
		_ = connection.route(t.Context(), acpMessage{
			Method: appServerNotifyAgentMessageDelta,
			Params: mustJSONRawMessage(t, map[string]any{"threadId": threadID, "delta": "x"}),
		})
	}
	telemetry := connection.unknownThreadTelemetry()
	if telemetry.BufferedCount != appServerUnknownThreadLimit || telemetry.BufferedBytes <= 0 || telemetry.DroppedCapacity != 1 {
		t.Fatalf("count-bound telemetry = %#v", telemetry)
	}

	byteBounded := &appServerConnection{
		unknownByThread: make(map[string][]appServerRoutedMessage), clock: func() time.Time { return now },
	}
	_ = byteBounded.route(t.Context(), acpMessage{
		Method: appServerNotifyAgentMessageDelta,
		Params: mustJSONRawMessage(t, map[string]any{
			"threadId": "thread-large", "delta": strings.Repeat("x", appServerUnknownThreadByteLimit),
		}),
	})
	if telemetry := byteBounded.unknownThreadTelemetry(); telemetry.BufferedCount != 0 || telemetry.DroppedCapacity != 1 {
		t.Fatalf("byte-bound telemetry = %#v", telemetry)
	}

	expiring := &appServerConnection{
		unknownByThread: make(map[string][]appServerRoutedMessage), clock: func() time.Time { return now },
	}
	_ = expiring.route(t.Context(), acpMessage{
		Method: appServerNotifyAgentMessageDelta,
		Params: json.RawMessage(`{"threadId":"thread-expiring","delta":"early"}`),
	})
	now = now.Add(appServerUnknownThreadTTL + time.Nanosecond)
	if telemetry := expiring.unknownThreadTelemetry(); telemetry.BufferedCount != 0 || telemetry.BufferedBytes != 0 || telemetry.DroppedExpired != 1 {
		t.Fatalf("expiry telemetry = %#v", telemetry)
	}
}

func TestAppServerRequestSchedulerPriorityReserveAndBackpressure(t *testing.T) {
	scheduler := newAppServerRequestScheduler()
	scheduler.mu.Lock()
	for index := 0; index < appServerRequestQueueCapacity-appServerRequestCriticalReserve; index++ {
		scheduler.queues[appServerRequestBackground] = append(
			scheduler.queues[appServerRequestBackground], &appServerScheduledRequest{},
		)
	}
	scheduler.inFlight = appServerRequestInFlightLimit
	scheduler.mu.Unlock()
	_, err := scheduler.do(t.Context(), appServerMethodModelList+"/uncached", nil, func(context.Context) ([]byte, error) {
		return nil, nil
	})
	var overload *AppServerBackpressureError
	if !errors.Is(err, ErrAppServerBackpressure) || !errors.As(err, &overload) || overload.Priority != "background" {
		t.Fatalf("background overload = %T %v", err, err)
	}
	scheduler.mu.Lock()
	if err := scheduler.admissionErrorLocked(appServerRequestCritical); err != nil {
		scheduler.mu.Unlock()
		t.Fatalf("critical request did not receive reserved capacity: %v", err)
	}
	for index := 0; index < appServerRequestCriticalReserve; index++ {
		scheduler.queues[appServerRequestCritical] = append(
			scheduler.queues[appServerRequestCritical], &appServerScheduledRequest{},
		)
	}
	criticalErr := scheduler.admissionErrorLocked(appServerRequestCritical)
	scheduler.mu.Unlock()
	if !errors.Is(criticalErr, ErrAppServerBackpressure) {
		t.Fatalf("full critical queue error = %v", criticalErr)
	}
	if scheduler.snapshot().Rejected != 1 {
		t.Fatalf("scheduler rejection telemetry = %#v", scheduler.snapshot())
	}
}

func TestAppServerRequestSchedulerRunsPriorityFIFOAndSerializesThreadMutations(t *testing.T) {
	scheduler := newAppServerRequestScheduler()
	started := make(chan string, 3)
	release := make(chan struct{}, 3)
	newRequest := func(name string, priority appServerRequestPriority) *appServerScheduledRequest {
		return &appServerScheduledRequest{
			ctx: t.Context(), priority: priority, enqueuedAt: time.Now(), done: make(chan appServerScheduledResult, 1),
			run: func(context.Context) ([]byte, error) {
				started <- name
				<-release
				return nil, nil
			},
		}
	}
	scheduler.mu.Lock()
	scheduler.inFlight = appServerRequestInFlightLimit - 1
	scheduler.queues[appServerRequestBackground] = append(scheduler.queues[appServerRequestBackground], newRequest("background", appServerRequestBackground))
	scheduler.queues[appServerRequestInteractive] = append(scheduler.queues[appServerRequestInteractive], newRequest("interactive", appServerRequestInteractive))
	scheduler.queues[appServerRequestCritical] = append(scheduler.queues[appServerRequestCritical], newRequest("critical", appServerRequestCritical))
	scheduler.dispatchLocked()
	scheduler.mu.Unlock()
	for _, want := range []string{"critical", "interactive", "background"} {
		if got := <-started; got != want {
			t.Fatalf("scheduler start order = %q, want %q", got, want)
		}
		release <- struct{}{}
	}

	mutationScheduler := newAppServerRequestScheduler()
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	results := make(chan error, 2)
	params := map[string]any{"threadId": "thread-a", "goal": map[string]any{"text": "same"}}
	go func() {
		_, err := mutationScheduler.do(t.Context(), appServerMethodThreadGoalSet, params, func(context.Context) ([]byte, error) {
			close(firstStarted)
			<-releaseFirst
			return nil, nil
		})
		results <- err
	}()
	<-firstStarted
	secondAdmitted := make(chan struct{})
	mutationScheduler.mu.Lock()
	mutationScheduler.admissionHook = func(method string, coalesced bool) {
		if method == appServerMethodThreadGoalSet && !coalesced {
			close(secondAdmitted)
		}
	}
	mutationScheduler.mu.Unlock()
	go func() {
		_, err := mutationScheduler.do(t.Context(), appServerMethodThreadGoalSet, params, func(context.Context) ([]byte, error) {
			close(secondStarted)
			return nil, nil
		})
		results <- err
	}()
	<-secondAdmitted
	mutationScheduler.mu.Lock()
	queuedMutation := len(mutationScheduler.queues[appServerRequestInteractive])
	mutationScheduler.mu.Unlock()
	if queuedMutation != 1 {
		t.Fatalf("same-thread mutation queue = %d, want 1", queuedMutation)
	}
	close(releaseFirst)
	<-secondStarted
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	if mutationScheduler.snapshot().Coalesced != 0 {
		t.Fatal("mutation requests were incorrectly coalesced")
	}
}

func TestAppServerRequestSchedulerCoalescesOnlySafeModelReads(t *testing.T) {
	scheduler := newAppServerRequestScheduler()
	started := make(chan struct{})
	release := make(chan struct{})
	results := make(chan appServerScheduledResult, 2)
	runs := 0
	var runsMu sync.Mutex
	call := func() {
		raw, err := scheduler.do(t.Context(), appServerMethodModelList, map[string]any{}, func(context.Context) ([]byte, error) {
			runsMu.Lock()
			runs++
			runsMu.Unlock()
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return []byte(`{"data":[{"id":"model-a"}]}`), nil
		})
		results <- appServerScheduledResult{raw: raw, err: err}
	}
	go call()
	<-started
	coalesced := make(chan struct{})
	scheduler.mu.Lock()
	scheduler.admissionHook = func(method string, shared bool) {
		if method == appServerMethodModelList && shared {
			close(coalesced)
		}
	}
	scheduler.mu.Unlock()
	go call()
	<-coalesced
	close(release)
	for range 2 {
		result := <-results
		if result.err != nil || !strings.Contains(string(result.raw), "model-a") {
			t.Fatalf("coalesced result = %q, %v", result.raw, result.err)
		}
	}
	runsMu.Lock()
	defer runsMu.Unlock()
	if runs != 1 {
		t.Fatalf("model/list executions = %d, want 1", runs)
	}
}

func TestAppServerRouterDoesNotLetSlowBindingBlockAnotherThread(t *testing.T) {
	connection := &appServerConnection{
		generation:        1,
		bindingsBySession: make(map[string]*appServerThreadBinding),
		ownerByThread:     map[string]string{"thread-a": "session-a", "thread-b": "session-b"},
		unknownByThread:   make(map[string][]appServerRoutedMessage),
	}
	slow := &appServerThreadBinding{
		connection: connection, agentSessionID: "session-a", generation: 1,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	fast := &appServerThreadBinding{
		connection: connection, agentSessionID: "session-b", generation: 1,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	connection.bindingsBySession["session-a"] = slow
	connection.bindingsBySession["session-b"] = fast
	for index := 0; index < 10_000; index++ {
		slow.enqueue(appServerRoutedMessage{message: acpMessage{Method: appServerNotifyAgentMessageDelta}})
	}
	routed := make(chan struct{})
	go func() {
		_ = connection.route(t.Context(), acpMessage{
			Method: appServerNotifyAgentMessageDelta,
			Params: json.RawMessage(`{"threadId":"thread-b","delta":"ok"}`),
		})
		close(routed)
	}()
	select {
	case <-routed:
	case <-time.After(time.Second):
		t.Fatal("thread-b routing blocked behind thread-a backlog")
	}
	fast.mu.Lock()
	queued := len(fast.queue)
	fast.mu.Unlock()
	if queued != 1 {
		t.Fatalf("thread-b queue length = %d, want 1", queued)
	}
}

func TestAppServerRouterDropsStaleConnectionGeneration(t *testing.T) {
	adapter := NewCodexAppServerAdapter(nil)
	accepted := 0
	adapter.appServerDispatchAcceptedHook = func(*appServerThreadBinding, acpMessage) { accepted++ }
	oldConnection := &appServerConnection{generation: 1}
	oldBinding := &appServerThreadBinding{
		connection: oldConnection, agentSessionID: "session-a", generation: 1,
		runtimeSession: Session{AgentSessionID: "session-a", Provider: ProviderCodex, ProviderSessionID: "thread-a"},
	}
	newConnection := &appServerConnection{generation: 2}
	newBinding := &appServerThreadBinding{
		connection: newConnection, agentSessionID: "session-a", generation: 2,
	}
	newTurn := &codexAppServerActiveTurn{
		turnID: "canonical-new", providerTurnID: "turn-new", phase: codexAppServerTurnPhaseRunning,
		terminal: make(chan codexAppServerTurnTerminal, 1), terminated: make(chan struct{}),
	}
	adapter.sessions["session-a"] = &codexAppServerSession{
		connection: newConnection, binding: newBinding, threadID: "thread-a",
		activeTurn: newTurn, activeTurnID: "turn-new", activeTurnStartConfirmed: true,
	}

	// Model a notification from generation N that the old mailbox already
	// dequeued. Install generation N+1 before allowing that terminal to enter
	// the reducer. The adapter's current-binding fence, not an artificial
	// binding/connection generation mismatch, must reject it.
	taken := make(chan struct{})
	release := make(chan struct{})
	dispatched := make(chan struct{})
	go func() {
		close(taken)
		<-release
		adapter.dispatchAppServerBindingMessage(oldBinding, appServerRoutedMessage{
			ctx: t.Context(),
			message: acpMessage{Method: appServerNotifyTurnCompleted, Params: json.RawMessage(
				`{"threadId":"thread-a","turn":{"id":"turn-new","status":"completed"}}`,
			)},
		})
		adapter.dispatchAppServerBindingMessage(oldBinding, appServerRoutedMessage{
			ctx: t.Context(),
			message: acpMessage{Method: appServerNotifyAccountUpdated, Params: json.RawMessage(
				`{"threadId":"thread-a","authMode":"stale-generation"}`,
			)},
		})
		close(dispatched)
	}()
	<-taken
	close(release)
	<-dispatched
	if accepted != 0 {
		t.Fatalf("generation N delivered %d messages past the N+1 fence", accepted)
	}

	select {
	case terminal := <-newTurn.terminal:
		t.Fatalf("generation N terminal settled generation N+1: %#v", terminal)
	default:
	}
	if current := adapter.getSession("session-a"); current == nil || current.binding != newBinding || current.activeTurn != newTurn {
		t.Fatal("generation N terminal mutated the current generation binding")
	} else if asString(current.account["authMode"]) != "" {
		t.Fatalf("generation N account update mutated generation N+1: %#v", current.account)
	}
}

type retryCloseProcessConnection struct {
	mu       sync.Mutex
	calls    int
	finished chan struct{}
	once     sync.Once
}

func (*retryCloseProcessConnection) Send([]byte) error { return nil }

func (c *retryCloseProcessConnection) Recv() (ProcessFrame, error) {
	<-c.finished
	return ProcessFrame{}, io.EOF
}

func (c *retryCloseProcessConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return errors.New("close failed once")
	}
	c.once.Do(func() { close(c.finished) })
	return nil
}

func TestAppServerRegistryRetriesRetiredCloseFailure(t *testing.T) {
	physical := &retryCloseProcessConnection{finished: make(chan struct{})}
	cleanupCalls := 0
	client := newCodexAppServerClient(wrapProviderLaunchCleanup(physical, func(context.Context) error {
		cleanupCalls++
		return nil
	}))
	connection := &appServerConnection{generation: 7, client: client}
	registry := newAppServerConnectionRegistry(nil)
	connection.registry = registry
	if err := client.Close(); err == nil {
		t.Fatal("first close unexpectedly succeeded")
	}
	if cleanupCalls != 0 {
		t.Fatalf("process cleanup calls after failed close = %d, want 0", cleanupCalls)
	}
	registry.retired[connection.generation] = connection
	attempted, err := registry.retryOneRetired()
	if !attempted || err != nil {
		t.Fatalf("retry = %v, %v; want successful attempt", attempted, err)
	}
	registry.mu.Lock()
	_, retained := registry.retired[connection.generation]
	registry.mu.Unlock()
	if retained {
		t.Fatal("successfully closed generation remained retired")
	}
	if cleanupCalls != 1 {
		t.Fatalf("process cleanup calls after successful retry = %d, want 1", cleanupCalls)
	}
}

func TestAppServerRegistryRetainsFailedProcessCleanupUntilRetrySucceeds(t *testing.T) {
	physical := newBarrierModelListConnection()
	cleanupCalls := 0
	cleanup := providerLaunchCleanup(ProcessSpec{Provider: ProviderCodex}, func(context.Context) error {
		cleanupCalls++
		if cleanupCalls == 1 {
			return errors.New("process cleanup failed once")
		}
		return nil
	})
	client := newCodexAppServerClient(wrapProviderLaunchCleanup(physical, cleanup))
	connection := &appServerConnection{generation: 8, client: client}
	registry := newAppServerConnectionRegistry(nil)
	connection.registry = registry
	registry.retired[connection.generation] = connection

	if err := client.Close(); err == nil {
		t.Fatal("first close error = nil, want process cleanup failure")
	}
	registry.completeRetired(connection, errors.New("process cleanup failed once"))
	if attempted, err := registry.retryOneRetired(); !attempted || err != nil {
		t.Fatalf("retry = %v, %v; want successful process cleanup retry", attempted, err)
	}
	if cleanupCalls != 2 {
		t.Fatalf("process cleanup calls = %d, want failed attempt plus retry", cleanupCalls)
	}
	registry.mu.Lock()
	_, retained := registry.retired[connection.generation]
	registry.mu.Unlock()
	if retained {
		t.Fatal("successful process cleanup retry retained physical ownership")
	}
}

func TestAppServerRegistryRetainsFailedThreadCleanupUntilRetrySucceeds(t *testing.T) {
	cleanupCalls := 0
	cleanup := providerLaunchCleanup(ProcessSpec{Provider: ProviderCodex}, func(context.Context) error {
		cleanupCalls++
		if cleanupCalls == 1 {
			return errors.New("thread cleanup failed once")
		}
		return nil
	})
	connection := &appServerConnection{
		generation:            9,
		key:                   appServerConnectionKey{Provider: ProviderCodex},
		bindingsBySession:     map[string]*appServerThreadBinding{},
		ownerByThread:         map[string]string{"thread-a": "session-a"},
		retiredThreadCleanups: make(map[*appServerThreadBinding]struct{}),
	}
	binding := &appServerThreadBinding{
		connection: connection, agentSessionID: "session-a", providerThreadID: "thread-a",
		threadCleanup: cleanup, wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	connection.bindingsBySession[binding.agentSessionID] = binding
	adapter := NewCodexAppServerAdapter(nil)
	connection.registry = adapter.connections
	adapter.connections.byKey[connection.key] = connection
	adapter.sessions[binding.agentSessionID] = &codexAppServerSession{connection: connection, binding: binding}
	if err := adapter.Close(t.Context(), Session{AgentSessionID: binding.agentSessionID}); err == nil {
		t.Fatal("Close error = nil, want thread cleanup failure")
	}
	if adapter.getSession(binding.agentSessionID) != nil {
		t.Fatal("failed thread cleanup kept a detached shared Session published")
	}
	result := adapter.CleanupLiveSessionResources(t.Context(), 1)
	if result.Attempted != 1 || result.Cleaned != 1 || result.Failed != 0 {
		t.Fatalf("cleanup retry = %#v, want successful thread cleanup retry", result)
	}
	if cleanupCalls != 2 {
		t.Fatalf("thread cleanup calls = %d, want failed attempt plus retry", cleanupCalls)
	}
	connection.mu.Lock()
	retained := len(connection.retiredThreadCleanups)
	connection.mu.Unlock()
	if retained != 0 {
		t.Fatalf("retired thread cleanup owners = %d, want 0", retained)
	}
}

type eofProcessConnection struct {
	eof         chan struct{}
	closeCalled chan struct{}
	closeOnce   sync.Once
}

func (*eofProcessConnection) Send([]byte) error { return nil }

func (c *eofProcessConnection) Recv() (ProcessFrame, error) {
	<-c.eof
	return ProcessFrame{}, io.EOF
}

func (c *eofProcessConnection) Close() error {
	c.closeOnce.Do(func() { close(c.closeCalled) })
	return nil
}

func TestAppServerWatchDoneRetainsFailedActiveBindingCleanupUntilRetry(t *testing.T) {
	physical := &eofProcessConnection{eof: make(chan struct{}), closeCalled: make(chan struct{})}
	client := newCodexAppServerClient(physical)
	registry := newAppServerConnectionRegistry(nil)
	connection := &appServerConnection{
		registry: registry, generation: 10, client: client,
		key:               appServerConnectionKey{Provider: ProviderCodex},
		bindingsBySession: map[string]*appServerThreadBinding{}, ownerByThread: map[string]string{},
		replacementByThread: map[string]*appServerThreadBinding{}, unknownByThread: map[string][]appServerRoutedMessage{},
		retiredThreadCleanups: make(map[*appServerThreadBinding]struct{}),
	}
	cleanupCalls := 0
	binding := &appServerThreadBinding{
		connection: connection, agentSessionID: "session-eof", providerThreadID: "thread-eof",
		wake: make(chan struct{}, 1), done: make(chan struct{}),
		threadCleanup: func(context.Context) error {
			cleanupCalls++
			if cleanupCalls == 1 {
				return errors.New("injected EOF thread cleanup failure")
			}
			return nil
		},
	}
	connection.bindingsBySession[binding.agentSessionID] = binding
	connection.ownerByThread[binding.providerThreadID] = binding.agentSessionID
	registry.byKey[connection.key] = connection
	registry.byClient[client] = connection
	watchReturned := make(chan struct{})
	go func() {
		connection.watchDone()
		close(watchReturned)
	}()
	close(physical.eof)
	select {
	case <-watchReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("watchDone did not return after EOF")
	}
	registry.mu.Lock()
	_, retained := registry.retired[connection.generation]
	registry.mu.Unlock()
	if !retained {
		t.Fatal("EOF dropped registry ownership after active Binding cleanup failed")
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls after EOF = %d, want 1 failed attempt", cleanupCalls)
	}
	if attempted, err := registry.retryOneRetired(); !attempted || err != nil {
		t.Fatalf("thread cleanup retry = %v, %v; want success", attempted, err)
	}
	registry.mu.Lock()
	_, retained = registry.retired[connection.generation]
	registry.mu.Unlock()
	if !retained {
		t.Fatal("successful Thread cleanup retry removed physical connection ownership before close retry")
	}
	if attempted, err := registry.retryOneRetired(); !attempted || err != nil {
		t.Fatalf("physical cleanup retry = %v, %v; want success", attempted, err)
	}
	registry.mu.Lock()
	_, retained = registry.retired[connection.generation]
	registry.mu.Unlock()
	if retained {
		t.Fatal("successful Thread and physical cleanup retained connection ownership")
	}
	if cleanupCalls != 2 {
		t.Fatalf("cleanup calls = %d, want failed attempt plus retry", cleanupCalls)
	}
}

type forceCloseFinalizerConnection struct {
	eof            chan struct{}
	cleanupEntered <-chan struct{}
	closeOnce      sync.Once
}

func (*forceCloseFinalizerConnection) Send([]byte) error { return nil }

func (c *forceCloseFinalizerConnection) Recv() (ProcessFrame, error) {
	<-c.eof
	return ProcessFrame{}, io.EOF
}

func (c *forceCloseFinalizerConnection) Close() error {
	c.closeOnce.Do(func() { close(c.eof) })
	<-c.cleanupEntered
	return nil
}

func TestAppServerForceCloseCannotFinalizeBeforeFailedBindingCleanup(t *testing.T) {
	cleanupEntered := make(chan struct{})
	allowCleanup := make(chan struct{})
	physical := &forceCloseFinalizerConnection{eof: make(chan struct{}), cleanupEntered: cleanupEntered}
	client := newCodexAppServerClient(physical)
	registry := newAppServerConnectionRegistry(nil)
	connection := &appServerConnection{
		registry: registry, generation: 12, client: client,
		key:               appServerConnectionKey{Provider: ProviderCodex},
		bindingsBySession: map[string]*appServerThreadBinding{}, ownerByThread: map[string]string{},
		replacementByThread: map[string]*appServerThreadBinding{}, unknownByThread: map[string][]appServerRoutedMessage{},
		retiredThreadCleanups: make(map[*appServerThreadBinding]struct{}),
	}
	binding := &appServerThreadBinding{
		connection: connection, agentSessionID: "session-force", providerThreadID: "thread-force",
		wake: make(chan struct{}, 1), done: make(chan struct{}),
		threadCleanup: func(context.Context) error {
			close(cleanupEntered)
			<-allowCleanup
			return errors.New("injected force-close cleanup failure")
		},
	}
	connection.bindingsBySession[binding.agentSessionID] = binding
	connection.ownerByThread[binding.providerThreadID] = binding.agentSessionID
	registry.byKey[connection.key] = connection
	registry.byClient[client] = connection
	watchReturned := make(chan struct{})
	go func() {
		connection.watchDone()
		close(watchReturned)
	}()
	forceReturned := make(chan error, 1)
	go func() { forceReturned <- connection.forceClose() }()
	if err := <-forceReturned; err != nil {
		t.Fatalf("forceClose(): %v", err)
	}
	registry.mu.Lock()
	_, retained := registry.retired[connection.generation]
	registry.mu.Unlock()
	if !retained {
		t.Fatal("forceClose finalized registry ownership while Binding cleanup was in flight")
	}
	close(allowCleanup)
	select {
	case <-watchReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("watchDone did not return after failed Binding cleanup")
	}
	registry.mu.Lock()
	_, retained = registry.retired[connection.generation]
	registry.mu.Unlock()
	if !retained {
		t.Fatal("force-close Binding cleanup failure lost retry ownership")
	}
}

func TestPublishBindingReplacementIsAtomicWithConcurrentDispatch(t *testing.T) {
	adapter := NewCodexAppServerAdapter(nil)
	connection := &appServerConnection{
		generation:        11,
		bindingsBySession: map[string]*appServerThreadBinding{}, ownerByThread: map[string]string{},
		replacementByThread: map[string]*appServerThreadBinding{}, unknownByThread: map[string][]appServerRoutedMessage{},
		retiredThreadCleanups: make(map[*appServerThreadBinding]struct{}),
	}
	oldBinding := &appServerThreadBinding{
		connection: connection, agentSessionID: "session-replacement", generation: 11,
		providerThreadID: "thread-replacement", wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	newBinding := &appServerThreadBinding{
		connection: connection, agentSessionID: oldBinding.agentSessionID, generation: 11,
		expectedThreadID: oldBinding.providerThreadID, providerThreadID: oldBinding.providerThreadID,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	newBinding.replacementOf.Store(oldBinding)
	connection.bindingsBySession[newBinding.agentSessionID] = newBinding
	connection.replacementByThread[newBinding.providerThreadID] = newBinding
	adapter.sessions[newBinding.agentSessionID] = &codexAppServerSession{connection: connection, binding: oldBinding}

	readReached := make(chan struct{})
	publishReached := make(chan struct{})
	allowAccess := make(chan struct{})
	adapter.appServerReplacementReadHook = func() {
		close(readReached)
		<-allowAccess
	}
	connection.replacementPublishHook = func() {
		close(publishReached)
		<-allowAccess
	}
	dispatchReturned := make(chan struct{})
	go func() {
		adapter.dispatchAppServerBindingMessage(newBinding, appServerRoutedMessage{
			ctx: t.Context(), message: acpMessage{Method: appServerNotifyTokenUsage},
		})
		close(dispatchReturned)
	}()
	publishReturned := make(chan struct{})
	go func() {
		connection.publishBindingReplacement(newBinding)
		close(publishReturned)
	}()
	<-readReached
	<-publishReached
	close(allowAccess)
	select {
	case <-dispatchReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent replacement dispatch did not settle")
	}
	select {
	case <-publishReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent replacement publication did not settle")
	}
	if newBinding.replacementOf.Load() != nil {
		t.Fatal("published Binding retained provisional replacement owner")
	}
}

type barrierModelListConnection struct {
	mu       sync.Mutex
	requests int
	recv     chan ProcessFrame
	observed chan struct{}
	release  chan struct{}
	done     chan struct{}
	close    sync.Once
}

func newBarrierModelListConnection() *barrierModelListConnection {
	return &barrierModelListConnection{
		recv: make(chan ProcessFrame, 1), observed: make(chan struct{}, 1),
		release: make(chan struct{}), done: make(chan struct{}),
	}
}

func (c *barrierModelListConnection) Send(raw []byte) error {
	var request sharedHelperRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	if request.Method != appServerMethodModelList {
		return nil
	}
	c.mu.Lock()
	c.requests++
	c.mu.Unlock()
	select {
	case c.observed <- struct{}{}:
	default:
	}
	go func() {
		select {
		case <-c.release:
			response, _ := json.Marshal(map[string]any{
				"id": request.ID, "result": map[string]any{"data": []any{map[string]any{"id": "model-a"}}},
			})
			select {
			case c.recv <- ProcessFrame{Stdout: append(response, '\n')}:
			case <-c.done:
			}
		case <-c.done:
		}
	}()
	return nil
}

func (c *barrierModelListConnection) Recv() (ProcessFrame, error) {
	select {
	case frame := <-c.recv:
		return frame, nil
	case <-c.done:
		return ProcessFrame{}, io.EOF
	}
}

func (c *barrierModelListConnection) Close() error {
	c.close.Do(func() { close(c.done) })
	return nil
}

func (c *barrierModelListConnection) requestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

func TestAppServerConnectionCoalescesConcurrentModelList(t *testing.T) {
	physical := newBarrierModelListConnection()
	client := newCodexAppServerClient(physical)
	connection := &appServerConnection{client: client}
	start := make(chan struct{})
	entered := make(chan struct{}, 2)
	results := make(chan []map[string]any, 2)
	for range 2 {
		go func() {
			entered <- struct{}{}
			<-start
			results <- connection.modelList(t.Context(), nil)
		}()
	}
	<-entered
	<-entered
	close(start)
	select {
	case <-physical.observed:
	case <-time.After(time.Second):
		t.Fatal("model/list request was not sent")
	}
	close(physical.release)
	for range 2 {
		select {
		case models := <-results:
			if len(models) != 1 || asString(models[0]["id"]) != "model-a" {
				t.Fatalf("coalesced models = %#v, want model-a", models)
			}
		case <-time.After(time.Second):
			t.Fatal("coalesced model/list caller did not finish")
		}
	}
	if got := physical.requestCount(); got != 1 {
		t.Fatalf("physical model/list requests = %d, want 1", got)
	}
	_ = client.Close()
}

func TestConcurrentSessionsShareOneProcessAndRouteInterleavedMessages(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	transport := &countingProcessTransport{inner: NewLocalProcessTransport()}
	adapter := NewCodexAppServerAdapter(transport)
	processCWD := t.TempDir()
	adapter.SetProviderLaunchPreparer(func(_ context.Context, input ProviderLaunchPrepareInput) (ProviderLaunchPrepareResult, error) {
		return ProviderLaunchPrepareResult{AppServer: &AppServerLaunchPreparation{
			ProcessProfile: AppServerProcessProfile{
				ExecutionHostID: "local-test", RuntimeGeneration: "generation-1",
				TransportScopeID: "native-process", ProcessProfileDigest: "shared-test-profile",
				Command: []string{executable, "-test.run=^TestSharedAppServerHelperProcess$", "--"},
				Env: []string{
					sharedAppServerHelperEnv + "=1", sharedAppServerAutoCompleteEnv + "=1",
				}, CWD: processCWD,
			},
			ThreadOverlay: AppServerThreadOverlay{
				Env:        append([]string(nil), input.Session.Env...),
				MCPServers: []MCPServerBinding{{Name: "session-tool", URL: "http://127.0.0.1/tool"}},
				ModelProviderCredentials: []AppServerModelProviderCredential{{
					ModelProviderID: "tutti_model_plan", BearerToken: "token-" + input.Session.AgentSessionID,
				}},
				DeveloperInstructions: "instructions for " + input.Session.AgentSessionID,
			},
		}}, nil
	})

	sessions := []Session{
		{RoomID: "room-a", AgentSessionID: "session-a", Provider: ProviderCodex, CWD: processCWD, Env: []string{"TUTTI_AGENT_SESSION_ID=session-a"}},
		{RoomID: "room-b", AgentSessionID: "session-b", Provider: ProviderCodex, CWD: processCWD, Env: []string{"TUTTI_AGENT_SESSION_ID=session-b"}},
	}
	errorsBySession := make(chan error, len(sessions))
	var wait sync.WaitGroup
	for _, session := range sessions {
		session := session
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, startErr := adapter.Start(t.Context(), session)
			errorsBySession <- startErr
		}()
	}
	wait.Wait()
	close(errorsBySession)
	for startErr := range errorsBySession {
		if startErr != nil {
			t.Fatalf("Start() error = %v", startErr)
		}
	}
	if got := transport.startCount(); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
	for _, entry := range transport.firstSpec().Env {
		key := strings.SplitN(entry, "=", 2)[0]
		switch key {
		case "TUTTI_AGENT_SESSION_ID", "TUTTI_WORKSPACE_ID", "TUTTI_ROOM_ID", "CONNECTOR_PROOF", "TUTTI_MODEL_PLAN_API_KEY":
			t.Fatalf("session-scoped env %q leaked into shared process", key)
		}
	}

	seenThreads := make(map[string]struct{}, len(sessions))
	seenUsage := make(map[int64]struct{}, len(sessions))
	for _, session := range sessions {
		appSession := adapter.getSession(session.AgentSessionID)
		if appSession == nil || appSession.connection == nil || appSession.binding == nil {
			t.Fatalf("session %s has no shared binding", session.AgentSessionID)
		}
		if appSession.connection != adapter.getSession(sessions[0].AgentSessionID).connection {
			t.Fatalf("session %s did not reuse the physical connection", session.AgentSessionID)
		}
		wantThread := "thread-a"
		wantUsage := int64(101)
		if session.AgentSessionID == "session-b" {
			wantThread = "thread-b"
			wantUsage = 202
		}
		if appSession.threadID != wantThread {
			t.Fatalf("session %s thread = %q, want %q", session.AgentSessionID, appSession.threadID, wantThread)
		}
		seenThreads[appSession.threadID] = struct{}{}
		if !appSession.usage.contextKnown {
			t.Fatalf("session %s lost early token usage", session.AgentSessionID)
		}
		seenUsage[appSession.usage.contextUsedTokens] = struct{}{}
		if appSession.usage.contextUsedTokens != wantUsage {
			t.Fatalf("session %s usage = %d, want %d", session.AgentSessionID, appSession.usage.contextUsedTokens, wantUsage)
		}
	}
	if len(seenThreads) != 2 || len(seenUsage) != 2 {
		t.Fatalf("thread/usage routing collapsed: threads=%#v usage=%#v", seenThreads, seenUsage)
	}

	type execResult struct {
		events []activityshared.Event
		err    error
	}
	execResults := make(chan execResult, len(sessions))
	approvals := make(chan activityshared.Event, len(sessions))
	for index, session := range sessions {
		index, session := index, session
		go func() {
			events, execErr := adapter.Exec(
				t.Context(), session, textPrompt("interleave"), "",
				fmt.Sprintf("canonical-interleave-%d", index+1), func(next []activityshared.Event) {
					for _, event := range next {
						if event.Type == activityshared.EventInteractionRequested {
							approvals <- event
						}
					}
				}, nil,
			)
			execResults <- execResult{events: events, err: execErr}
		}()
	}
	sessionByID := map[string]Session{"session-a": sessions[0], "session-b": sessions[1]}
	for range sessions {
		select {
		case approval := <-approvals:
			if approval.Payload.Interaction == nil {
				t.Fatalf("approval event has no interaction: %#v", approval)
			}
			if _, err := adapter.SubmitInteractive(t.Context(), sessionByID[approval.AgentSessionID], SubmitInteractiveInput{
				TurnID: approval.Payload.Interaction.TurnID, RequestID: approval.Payload.Interaction.RequestID, OptionID: "deny",
			}); err != nil {
				t.Fatalf("SubmitInteractive(%s) error = %v", approval.AgentSessionID, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("interleaved approval did not reach its owning Session")
		}
	}
	contents := make(map[string]string, len(sessions))
	messageOrder := make(map[string][]string, len(sessions))
	for range sessions {
		select {
		case result := <-execResults:
			if result.err != nil {
				t.Fatalf("interleaved Exec() error = %v", result.err)
			}
			for _, event := range result.events {
				if event.Type == activityshared.EventMessageAppended && event.Payload.Role == activityshared.MessageRoleAssistant {
					messageOrder[event.AgentSessionID] = append(messageOrder[event.AgentSessionID], event.Payload.Content)
					if asString(event.Payload.Metadata["streamState"]) == messageStreamStateCompleted {
						contents[event.AgentSessionID] = event.Payload.Content
					}
				}
			}
			if len(eventsOfType(result.events, activityshared.EventRootProviderTurnCompleted)) != 1 {
				t.Fatalf("interleaved terminal events = %#v", result.events)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("interleaved turn did not complete after server-request responses")
		}
	}
	if contents["session-a"] != "A-1A-2" || contents["session-b"] != "B-1B-2" {
		t.Fatalf("interleaved assistant routing = %#v, want exact A/B streams", contents)
	}
	if got := strings.Join(messageOrder["session-a"], "|"); got != "A-1|A-1A-2|A-1A-2" {
		t.Fatalf("session-a message order = %q", got)
	}
	if got := strings.Join(messageOrder["session-b"], "|"); got != "B-1|B-1B-2|B-1B-2" {
		t.Fatalf("session-b message order = %q", got)
	}

	if err := adapter.Close(t.Context(), sessions[0]); err != nil {
		t.Fatalf("Close(session-a) error = %v", err)
	}
	if !adapter.HasLiveSession(sessions[1]) {
		t.Fatal("closing session-a terminated session-b shared connection")
	}
	if got := transport.startCount(); got != 1 {
		t.Fatalf("process starts after detach = %d, want 1", got)
	}
	if err := adapter.Close(t.Context(), sessions[1]); err != nil {
		t.Fatalf("Close(session-b) error = %v", err)
	}
	cleanup := adapter.CleanupLiveSessionResources(t.Context(), 1)
	if cleanup.Attempted != 1 || cleanup.Cleaned != 1 {
		t.Fatalf("idle connection cleanup = %#v, want one clean close", cleanup)
	}
}

func TestDetachAndGracefulCancelAreThreadScoped(t *testing.T) {
	adapter, transport, sessions := startTwoSharedHelperSessions(t, false)
	turnStarted := make(chan string, len(sessions))
	adapter.appServerDispatchAcceptedHook = func(binding *appServerThreadBinding, message acpMessage) {
		if message.Method == appServerNotifyTurnStarted {
			turnStarted <- binding.agentSessionID
		}
	}
	execDone := []chan error{make(chan error, 1), make(chan error, 1)}
	for index, session := range sessions {
		index, session := index, session
		go func() {
			_, err := adapter.Exec(t.Context(), session, textPrompt("hold"), "", fmt.Sprintf("canonical-turn-%d", index+1), nil, nil)
			execDone[index] <- err
		}()
	}
	awaitSharedRequests(t, transport.sent, appServerMethodTurnStart, 2)
	seenTurnStarts := make(map[string]struct{}, len(sessions))
	for range sessions {
		select {
		case agentSessionID := <-turnStarted:
			seenTurnStarts[agentSessionID] = struct{}{}
		case <-time.After(5 * time.Second):
			t.Fatal("both shared turn/start notifications were not accepted")
		}
	}
	for _, session := range sessions {
		if _, ok := seenTurnStarts[session.AgentSessionID]; !ok {
			t.Fatalf("turn/start notification for %s was not accepted", session.AgentSessionID)
		}
	}
	if _, err := adapter.Cancel(t.Context(), sessions[0], "test graceful cancel"); err != nil {
		t.Fatalf("Cancel(session-a) error = %v", err)
	}
	request := awaitSharedRequest(t, transport.sent, appServerMethodTurnInterrupt)
	if request.threadID != sessions[0].ProviderSessionID {
		t.Fatalf("interrupt thread = %q, want %q", request.threadID, sessions[0].ProviderSessionID)
	}
	select {
	case err := <-execDone[0]:
		if err != nil {
			t.Fatalf("Exec(session-a) error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session-a turn did not settle after graceful interrupt")
	}
	if !adapter.HasLiveSession(sessions[1]) || adapter.sessionActiveTurnID(sessions[1].AgentSessionID) == "" {
		t.Fatal("session-a graceful cancel affected session-b")
	}
	if err := adapter.Close(t.Context(), sessions[0]); err != nil {
		t.Fatalf("Close(session-a) error = %v", err)
	}
	unsubscribe := awaitSharedRequest(t, transport.sent, appServerMethodThreadUnsubscribe)
	if unsubscribe.threadID != sessions[0].ProviderSessionID {
		t.Fatalf("unsubscribe thread = %q, want %q", unsubscribe.threadID, sessions[0].ProviderSessionID)
	}
	if !adapter.HasLiveSession(sessions[1]) {
		t.Fatal("session-a detach closed session-b connection")
	}
	if _, err := adapter.Cancel(t.Context(), sessions[1], "test cleanup cancel"); err != nil {
		t.Fatalf("Cancel(session-b) error = %v", err)
	}
	awaitSharedRequest(t, transport.sent, appServerMethodTurnInterrupt)
	select {
	case <-execDone[1]:
	case <-time.After(5 * time.Second):
		t.Fatal("session-b cleanup turn did not settle")
	}
	_ = adapter.Close(t.Context(), sessions[1])
	adapter.CleanupLiveSessionResources(t.Context(), 1)
}

func TestSharedProfileResumeUsesExactThreadAndAtomicallyReplacesBinding(t *testing.T) {
	adapter, transport, sessions := startTwoSharedHelperSessions(t, false)
	session := sessions[0]
	original := adapter.getSession(session.AgentSessionID)
	originalBinding := original.binding
	turnStartsBefore := transport.methodCount(appServerMethodTurnStart)

	failing := session
	failing.Env = append(append([]string(nil), session.Env...), "FAIL_RESUME=1")
	failing.Settings = &SessionSettings{Model: "fail-resume"}
	if err := adapter.Resume(t.Context(), failing); err == nil {
		t.Fatal("failing shared resume unexpectedly succeeded")
	}
	afterFailure := adapter.getSession(session.AgentSessionID)
	if afterFailure != original || afterFailure.binding != originalBinding || !adapter.HasLiveSession(session) {
		t.Fatal("failed shared resume replaced or disconnected the previous binding")
	}
	original.lastCanonicalTurnID = "root-turn-window"
	adapter.appServerReplacementCommittedHook = func(binding *appServerThreadBinding) {
		adapter.dispatchAppServerBindingMessage(binding, appServerRoutedMessage{
			ctx: t.Context(),
			message: acpMessage{Method: appServerNotifyTokenUsage, Params: mustJSONRawMessage(t, map[string]any{
				"threadId": session.ProviderSessionID,
				"tokenUsage": map[string]any{
					"modelContextWindow": 1000,
					"last":               map[string]any{"inputTokens": 404, "totalTokens": 404},
					"total":              map[string]any{"totalTokens": 404},
				},
			})},
		})
		_, _ = adapter.rememberAppServerChildThreads(
			session, session.ProviderSessionID, session.AgentSessionID, "root-turn-window",
			session.AgentSessionID, "root-turn-window", map[string]any{
				"type": "collabAgentToolCall", "id": "spawn-window", "tool": "spawnAgent",
				"receiverThreadIds": []any{"child-thread-window"},
			},
		)
	}

	if err := adapter.Resume(t.Context(), session); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	resumed := adapter.getSession(session.AgentSessionID)
	if resumed.binding == originalBinding {
		t.Fatal("successful shared resume did not atomically replace the binding")
	}
	if resumed.threadID != session.ProviderSessionID {
		t.Fatalf("resumed thread = %q, want %q", resumed.threadID, session.ProviderSessionID)
	}
	if resumed.usage.contextUsedTokens != 404 {
		_, bindingUsage, bindingKnown := resumed.binding.snapshot()
		t.Fatalf("resumed usage = %d, binding replay=%#v known=%t; want commit-window notification 404", resumed.usage.contextUsedTokens, bindingUsage, bindingKnown)
	}
	if child, ok := resumed.childThreads["child-thread-window"]; !ok || child == nil {
		t.Fatal("commit-window child spawn was not inherited by the published Session")
	}
	resumed.connection.mu.Lock()
	childOwner := resumed.connection.ownerByThread["child-thread-window"]
	resumed.connection.mu.Unlock()
	if childOwner != session.AgentSessionID {
		t.Fatalf("commit-window child owner = %q, want %q", childOwner, session.AgentSessionID)
	}
	if got := transport.startCount(); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
	if got := transport.methodCount(appServerMethodTurnStart); got != turnStartsBefore {
		t.Fatalf("resume replayed a prompt: turn/start count %d -> %d", turnStartsBefore, got)
	}
	for _, value := range sessions {
		_ = adapter.Close(t.Context(), value)
	}
	adapter.CleanupLiveSessionResources(t.Context(), 1)
}

func TestWedgedInterruptRetiresConnectionForEveryBinding(t *testing.T) {
	adapter, transport, sessions := startTwoSharedHelperSessions(t, true)
	adapter.cancelGraceWindow = 10 * time.Millisecond
	type execResult struct {
		events []activityshared.Event
		err    error
	}
	execDone := []chan execResult{make(chan execResult, 1), make(chan execResult, 1)}
	for index, session := range sessions {
		index, session := index, session
		go func() {
			events, err := adapter.Exec(t.Context(), session, textPrompt("hold"), "", fmt.Sprintf("canonical-turn-%d", index+1), nil, nil)
			execDone[index] <- execResult{events: events, err: err}
		}()
	}
	awaitSharedRequests(t, transport.sent, appServerMethodTurnStart, 2)
	if _, err := adapter.Cancel(t.Context(), sessions[0], "force shared retirement"); err != nil {
		t.Fatalf("Cancel(session-a) error = %v", err)
	}
	awaitSharedRequest(t, transport.sent, appServerMethodTurnInterrupt)
	results := make([]execResult, len(execDone))
	for index := range execDone {
		select {
		case results[index] = <-execDone[index]:
		case <-time.After(5 * time.Second):
			t.Fatalf("session %d turn did not settle after connection retirement", index)
		}
	}
	canceled := eventsOfType(results[0].events, activityshared.EventRootProviderTurnCompleted)
	if len(canceled) != 1 || canceled[0].Payload.TurnOutcome != string(activityshared.TurnOutcomeCanceled) {
		t.Fatalf("triggering session terminal = %#v, err=%v; want canceled", results[0].events, results[0].err)
	}
	if results[1].err == nil && len(eventsOfType(results[1].events, activityshared.EventRootProviderTurnCompleted)) == 0 {
		t.Fatalf("collateral session had no explicit terminal/error: %#v", results[1])
	}
	for _, session := range sessions {
		if adapter.HasLiveSession(session) {
			t.Fatalf("session %s retained dead shared generation", session.AgentSessionID)
		}
		if current := adapter.getSession(session.AgentSessionID); current != nil && len(current.pendingRequests) != 0 {
			t.Fatalf("session %s retained pending interactions after retirement", session.AgentSessionID)
		}
	}
	if got := transport.startCount(); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
}

func startTwoSharedHelperSessions(
	t *testing.T,
	wedgedInterrupt bool,
) (*CodexAppServerAdapter, *countingProcessTransport, []Session) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	transport := &countingProcessTransport{inner: NewLocalProcessTransport(), sent: make(chan sharedObservedRequest, 64)}
	adapter := NewCodexAppServerAdapter(transport)
	processCWD := t.TempDir()
	adapter.SetProviderLaunchPreparer(func(_ context.Context, input ProviderLaunchPrepareInput) (ProviderLaunchPrepareResult, error) {
		processEnv := []string{sharedAppServerHelperEnv + "=1"}
		if wedgedInterrupt {
			processEnv = append(processEnv, sharedAppServerWedgedInterruptEnv+"=1")
		}
		return ProviderLaunchPrepareResult{AppServer: &AppServerLaunchPreparation{
			ProcessProfile: AppServerProcessProfile{
				ExecutionHostID: "local-test", RuntimeGeneration: "generation-1",
				TransportScopeID: "native-process", ProcessProfileDigest: fmt.Sprintf("shared-test-%t", wedgedInterrupt),
				Command: []string{executable, "-test.run=^TestSharedAppServerHelperProcess$", "--"},
				Env:     processEnv, CWD: processCWD,
			},
			ThreadOverlay: AppServerThreadOverlay{
				Env:        append([]string(nil), input.Session.Env...),
				MCPServers: []MCPServerBinding{{Name: "session-tool", URL: "http://127.0.0.1/tool"}},
				ModelProviderCredentials: []AppServerModelProviderCredential{{
					ModelProviderID: "tutti_model_plan", BearerToken: "token-" + input.Session.AgentSessionID,
				}},
				DeveloperInstructions: "instructions for " + input.Session.AgentSessionID,
			},
		}}, nil
	})
	sessions := []Session{
		{RoomID: "room-a", AgentSessionID: "session-a", Provider: ProviderCodex, CWD: processCWD, Env: []string{"TUTTI_AGENT_SESSION_ID=session-a"}},
		{RoomID: "room-b", AgentSessionID: "session-b", Provider: ProviderCodex, CWD: processCWD, Env: []string{"TUTTI_AGENT_SESSION_ID=session-b"}},
	}
	startErrors := make(chan error, 2)
	for _, value := range sessions {
		value := value
		go func() {
			_, startErr := adapter.Start(t.Context(), value)
			startErrors <- startErr
		}()
	}
	for range sessions {
		if startErr := <-startErrors; startErr != nil {
			t.Fatalf("Start() error = %v", startErr)
		}
	}
	for index := range sessions {
		sessions[index].ProviderSessionID = adapter.getSession(sessions[index].AgentSessionID).threadID
	}
	return adapter, transport, sessions
}

func awaitSharedRequests(t *testing.T, sent <-chan sharedObservedRequest, method string, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		awaitSharedRequest(t, sent, method)
	}
}

func awaitSharedRequest(t *testing.T, sent <-chan sharedObservedRequest, method string) sharedObservedRequest {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case request := <-sent:
			if request.method == method {
				return request
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", method)
		}
	}
}

func TestSharedAppServerHelperProcess(_ *testing.T) {
	if os.Getenv(sharedAppServerHelperEnv) != "1" {
		return
	}
	if err := runSharedAppServerHelper(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

type sharedHelperRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params map[string]any  `json:"params"`
}

func runSharedAppServerHelper() error {
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	pendingStarts := make([]sharedHelperRequest, 0, 2)
	turnsByThread := make(map[string]string)
	serverResponses := make(map[string]bool)
	write := func(value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(raw, '\n')); err != nil {
			return err
		}
		return writer.Flush()
	}
	respond := func(request sharedHelperRequest, result any) error {
		return write(map[string]any{"id": request.ID, "result": result})
	}
	for scanner.Scan() {
		var request sharedHelperRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return err
		}
		if request.Method == "" && os.Getenv(sharedAppServerAutoCompleteEnv) == "1" {
			responseID := strings.Trim(string(request.ID), `"`)
			if responseID == "route-request-a" || responseID == "route-request-b" {
				serverResponses[responseID] = true
				if len(serverResponses) == 2 {
					sequence := []map[string]any{
						{"method": appServerNotifyAgentMessageDelta, "params": map[string]any{"threadId": "thread-a", "delta": "A-1"}},
						{"method": appServerNotifyAgentMessageDelta, "params": map[string]any{"threadId": "thread-b", "delta": "B-1"}},
						{"method": appServerNotifyAgentMessageDelta, "params": map[string]any{"threadId": "thread-a", "delta": "A-2"}},
						{"method": appServerNotifyTurnCompleted, "params": map[string]any{"threadId": "thread-a", "turn": map[string]any{"id": turnsByThread["thread-a"], "status": "completed", "items": []any{}}}},
						{"method": appServerNotifyAgentMessageDelta, "params": map[string]any{"threadId": "thread-b", "delta": "B-2"}},
						{"method": appServerNotifyTurnCompleted, "params": map[string]any{"threadId": "thread-b", "turn": map[string]any{"id": turnsByThread["thread-b"], "status": "completed", "items": []any{}}}},
					}
					for _, notification := range sequence {
						if err := write(notification); err != nil {
							return err
						}
					}
				}
				continue
			}
		}
		switch request.Method {
		case appServerMethodInitialized:
			continue
		case appServerMethodInitialize:
			if err := respond(request, map[string]any{"userAgent": "codex/test-shared", "platformOs": "test", "platformFamily": "test"}); err != nil {
				return err
			}
		case appServerMethodAccountRead:
			if err := respond(request, map[string]any{"account": map[string]any{"type": "chatgpt", "email": "test@example.com"}, "requiresOpenaiAuth": false}); err != nil {
				return err
			}
		case appServerMethodCollaborationModeList:
			if err := respond(request, map[string]any{"data": []any{}}); err != nil {
				return err
			}
		case appServerMethodModelList:
			if err := respond(request, map[string]any{"data": []any{}}); err != nil {
				return err
			}
		case appServerMethodRateLimitsRead:
			if err := respond(request, map[string]any{"rateLimits": map[string]any{}}); err != nil {
				return err
			}
		case appServerMethodThreadStart:
			sessionID := asString(helperThreadEnv(request.Params)["TUTTI_AGENT_SESSION_ID"])
			config, _ := request.Params["config"].(map[string]any)
			mcpServers, _ := config["mcp_servers"].(map[string]any)
			if sessionID == "" || mcpServers["session-tool"] == nil ||
				!strings.Contains(asString(request.Params["developerInstructions"]), sessionID) ||
				helperModelProviderToken(request.Params) != "token-"+sessionID {
				return errors.New("thread overlay was not delivered through thread/start")
			}
			pendingStarts = append(pendingStarts, request)
			if len(pendingStarts) < 2 {
				continue
			}
			for index := len(pendingStarts) - 1; index >= 0; index-- {
				sessionID := asString(helperThreadEnv(pendingStarts[index].Params)["TUTTI_AGENT_SESSION_ID"])
				threadID := "thread-a"
				used := int64(101)
				if sessionID == "session-b" {
					threadID = "thread-b"
					used = 202
				}
				if err := write(map[string]any{"method": appServerNotifyThreadStarted, "params": map[string]any{"thread": map[string]any{"id": threadID}}}); err != nil {
					return err
				}
				if err := write(map[string]any{"method": appServerNotifyTokenUsage, "params": map[string]any{
					"threadId":   threadID,
					"tokenUsage": map[string]any{"modelContextWindow": 1000, "last": map[string]any{"inputTokens": used, "totalTokens": used}, "total": map[string]any{"totalTokens": used}},
				}}); err != nil {
					return err
				}
				if err := respond(pendingStarts[index], map[string]any{"thread": map[string]any{"id": threadID}, "cwd": "/workspace"}); err != nil {
					return err
				}
			}
			pendingStarts = pendingStarts[:0]
		case appServerMethodThreadResume:
			threadID := asString(request.Params["threadId"])
			resumeSessionID := "session-a"
			if threadID == "thread-b" {
				resumeSessionID = "session-b"
			}
			if helperModelProviderToken(request.Params) != "token-"+resumeSessionID {
				return errors.New("thread model-provider credential was not delivered through thread/resume")
			}
			if strings.Contains(fmt.Sprint(request.Params), "FAIL_RESUME") || asString(request.Params["model"]) == "fail-resume" {
				if err := write(map[string]any{"id": request.ID, "error": map[string]any{"code": -32000, "message": "resume rejected by helper"}}); err != nil {
					return err
				}
				continue
			}
			if err := write(map[string]any{"method": appServerNotifyTokenUsage, "params": map[string]any{
				"threadId":   threadID,
				"tokenUsage": map[string]any{"modelContextWindow": 1000, "last": map[string]any{"inputTokens": 303, "totalTokens": 303}, "total": map[string]any{"totalTokens": 303}},
			}}); err != nil {
				return err
			}
			if err := respond(request, map[string]any{"thread": map[string]any{"id": threadID}, "cwd": "/workspace"}); err != nil {
				return err
			}
		case appServerMethodThreadUnsubscribe:
			if err := respond(request, map[string]any{"status": "unsubscribed"}); err != nil {
				return err
			}
		case appServerMethodTurnStart:
			threadID := asString(request.Params["threadId"])
			turnID := "turn-" + threadID
			turnsByThread[threadID] = turnID
			turn := map[string]any{"id": turnID, "status": "inProgress", "items": []any{}}
			if err := respond(request, map[string]any{"turn": turn}); err != nil {
				return err
			}
			if err := write(map[string]any{"method": appServerNotifyTurnStarted, "params": map[string]any{"threadId": threadID, "turn": turn}}); err != nil {
				return err
			}
			if os.Getenv(sharedAppServerAutoCompleteEnv) == "1" && len(turnsByThread) == 2 {
				for _, thread := range []string{"thread-a", "thread-b"} {
					requestID := "route-request-a"
					if thread == "thread-b" {
						requestID = "route-request-b"
					}
					if err := write(map[string]any{
						"id": requestID, "method": appServerMethodCommandApproval,
						"params": map[string]any{
							"threadId": thread, "turnId": turnsByThread[thread], "itemId": "item-" + thread,
							"command": "echo " + thread, "cwd": "/workspace", "reason": "route proof",
						},
					}); err != nil {
						return err
					}
				}
			}
		case appServerMethodTurnInterrupt:
			if os.Getenv(sharedAppServerWedgedInterruptEnv) == "1" {
				continue
			}
			threadID := asString(request.Params["threadId"])
			turnID := turnsByThread[threadID]
			if err := respond(request, map[string]any{"turnId": turnID}); err != nil {
				return err
			}
			if err := write(map[string]any{"method": appServerNotifyTurnCompleted, "params": map[string]any{
				"threadId": threadID,
				"turn":     map[string]any{"id": turnID, "status": "interrupted", "items": []any{}},
			}}); err != nil {
				return err
			}
		default:
			if len(request.ID) > 0 {
				if err := respond(request, map[string]any{}); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}

func helperThreadEnv(params map[string]any) map[string]any {
	config, _ := params["config"].(map[string]any)
	policy, _ := config["shell_environment_policy"].(map[string]any)
	values, _ := policy["set"].(map[string]any)
	return values
}

func helperModelProviderToken(params map[string]any) string {
	config, _ := params["config"].(map[string]any)
	providers, _ := config["model_providers"].(map[string]any)
	provider, _ := providers["tutti_model_plan"].(map[string]any)
	if provider["env_key"] != nil {
		return ""
	}
	return asString(provider["experimental_bearer_token"])
}
