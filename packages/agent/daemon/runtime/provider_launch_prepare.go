package agentruntime

import (
	"context"
	"log/slog"
	"sync"
)

type providerLaunchRuntimeContextKey struct{}

func withProviderLaunchRuntimeContext(ctx context.Context, runtimeContext map[string]any) context.Context {
	if len(runtimeContext) == 0 {
		return ctx
	}
	return context.WithValue(ctx, providerLaunchRuntimeContextKey{}, clonePayload(runtimeContext))
}

func providerLaunchRuntimeContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	runtimeContext, _ := ctx.Value(providerLaunchRuntimeContextKey{}).(map[string]any)
	return clonePayload(runtimeContext)
}

type ProviderLaunchPrepareInput struct {
	Provider    string
	Session     Session
	Command     []string
	Env         []string
	CWD         string
	DirectStart bool
	// SkipSkills is used by model-only probes that do not start a live Agent
	// Session and therefore do not need provider Skill materialization.
	SkipSkills bool
}

type ProviderLaunchPrepareResult struct {
	Command []string
	Env     []string
	CWD     string
	Cleanup func(context.Context) error
	// AppServer is consumed only by the Codex-compatible app-server adapter.
	// Generic ProcessTransport and ACP adapters never receive Thread overlay
	// material. Codex-compatible app-server launches must provide it explicitly.
	AppServer *AppServerLaunchPreparation
}

// AppServerLaunchPreparation separates a shared process lease from one
// provider Thread lease. A non-nil value is an explicit compatibility proof:
// ProcessProfile fields never fall back to the flat Session launch fields.
type AppServerLaunchPreparation struct {
	ProcessProfile AppServerProcessProfile
	ThreadOverlay  AppServerThreadOverlay
	ProcessCleanup func(context.Context) error
	ThreadCleanup  func(context.Context) error
}

type AppServerProcessProfile struct {
	ExecutionHostID      string
	RuntimeGeneration    string
	TransportScopeID     string
	ProcessProfileDigest string
	Command              []string
	Env                  []string
	CWD                  string
}

type AppServerThreadOverlay struct {
	Env                      []string
	MCPServers               []MCPServerBinding
	ModelProviderCredentials []AppServerModelProviderCredential
	BaseInstructions         string
	DeveloperInstructions    string
}

// AppServerModelProviderCredential delivers one provider token through the
// provider Thread config. It must never be copied into the shared process env
// or represented by a model-provider env_key.
type AppServerModelProviderCredential struct {
	ModelProviderID string
	BearerToken     string
}

type AppServerRuntimePreparation struct {
	ExecutionHostID          string
	RuntimeGeneration        string
	TransportScopeID         string
	ProcessProfileDigest     string
	ProcessCWD               string
	ProcessEnv               []string
	ThreadEnv                []string
	ModelProviderCredentials []AppServerModelProviderCredential
	BaseInstructions         string
	DeveloperInstructions    string
}

func cloneAppServerRuntimePreparation(input *AppServerRuntimePreparation) *AppServerRuntimePreparation {
	if input == nil {
		return nil
	}
	value := *input
	value.ProcessEnv = append([]string(nil), input.ProcessEnv...)
	value.ThreadEnv = append([]string(nil), input.ThreadEnv...)
	value.ModelProviderCredentials = cloneAppServerModelProviderCredentials(input.ModelProviderCredentials)
	return &value
}

func cloneAppServerModelProviderCredentials(input []AppServerModelProviderCredential) []AppServerModelProviderCredential {
	return append([]AppServerModelProviderCredential(nil), input...)
}

type ProviderLaunchPreparer func(context.Context, ProviderLaunchPrepareInput) (ProviderLaunchPrepareResult, error)

type ProviderLaunchPreparerAdapter interface {
	SetProviderLaunchPreparer(ProviderLaunchPreparer)
}

func setProviderLaunchPreparer(adapters []Adapter, preparer ProviderLaunchPreparer) {
	ApplyProviderLaunchPreparer(adapters, preparer)
}

func ApplyProviderLaunchPreparer(adapters []Adapter, preparer ProviderLaunchPreparer) {
	if preparer == nil {
		return
	}
	for _, adapter := range adapters {
		if setter, ok := adapter.(ProviderLaunchPreparerAdapter); ok {
			setter.SetProviderLaunchPreparer(preparer)
		}
	}
}

func prepareProviderLaunch(
	ctx context.Context,
	preparer ProviderLaunchPreparer,
	session Session,
	spec ProcessSpec,
) (ProcessSpec, func(context.Context) error, error) {
	spec.Command = append([]string(nil), spec.Command...)
	spec.Env = append([]string(nil), spec.Env...)
	spec.ExecutableIdentity = cloneExecutableIdentity(spec.ExecutableIdentity)
	if preparer == nil {
		return spec, nil, nil
	}
	launchSession := cloneProviderLaunchSession(session)
	if runtimeContext := providerLaunchRuntimeContext(ctx); runtimeContext != nil {
		launchSession.RuntimeContext = runtimeContext
	}
	result, err := preparer(ctx, ProviderLaunchPrepareInput{
		Provider:    spec.Provider,
		Session:     launchSession,
		Command:     append([]string(nil), spec.Command...),
		Env:         append([]string(nil), spec.Env...),
		CWD:         spec.CWD,
		DirectStart: spec.DirectStart,
	})
	if err != nil {
		return ProcessSpec{}, nil, err
	}
	spec.Command = append([]string(nil), result.Command...)
	spec.Env = append([]string(nil), result.Env...)
	spec.CWD = result.CWD
	return spec, providerLaunchCleanup(spec, result.Cleanup), nil
}

func cloneExecutableIdentity(value *ExecutableIdentity) *ExecutableIdentity {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneProviderLaunchSession(session Session) Session {
	session.Env = append([]string(nil), session.Env...)
	session.RuntimeContext = clonePayload(session.RuntimeContext)
	session.ProviderTargetRef = clonePayload(session.ProviderTargetRef)
	if session.Settings != nil {
		session.Settings = cloneSessionSettings(*session.Settings)
	}
	return session
}

func providerLaunchCleanup(spec ProcessSpec, cleanup func(context.Context) error) func(context.Context) error {
	if cleanup == nil {
		return nil
	}
	var mu sync.Mutex
	completed := false
	return func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if completed {
			return nil
		}
		if ctx == nil {
			ctx = context.Background()
		}
		if err := cleanup(ctx); err != nil {
			slog.Warn("agent session provider launch cleanup failed",
				"event", "agent_session.provider_launch.cleanup_failed",
				"provider", spec.Provider,
				"room_id", spec.RoomID,
				"agent_session_id", spec.AgentSessionID,
				"error", err.Error(),
			)
			return err
		}
		completed = true
		return nil
	}
}

func cleanupPreparedLaunch(cleanup func(context.Context) error) error {
	if cleanup != nil {
		return cleanup(context.Background())
	}
	return nil
}

func wrapProviderLaunchCleanup(conn ProcessConnection, cleanup func(context.Context) error) ProcessConnection {
	if conn == nil || cleanup == nil {
		return conn
	}
	wrapped := &providerLaunchCleanupConnection{
		ProcessConnection: conn,
		cleanup:           cleanup,
	}
	if graceful, ok := conn.(GracefulProcessConnection); ok {
		return &providerLaunchCleanupGracefulConnection{
			providerLaunchCleanupConnection: wrapped,
			graceful:                        graceful,
		}
	}
	return wrapped
}

type providerLaunchCleanupConnection struct {
	ProcessConnection
	cleanup func(context.Context) error
}

func (c *providerLaunchCleanupConnection) ProcessCassetteCaptureOrigin() ProcessCassetteCaptureOrigin {
	checkpoint, ok := c.ProcessConnection.(ProcessCassetteCheckpointConnection)
	if !ok {
		return ""
	}
	return checkpoint.ProcessCassetteCaptureOrigin()
}

func (c *providerLaunchCleanupConnection) RecvContext(ctx context.Context) (ProcessFrame, error) {
	if contextual, ok := c.ProcessConnection.(ContextProcessConnection); ok {
		return contextual.RecvContext(ctx)
	}
	return c.Recv()
}

func (c *providerLaunchCleanupConnection) Close() error {
	if c == nil {
		return nil
	}
	err := c.ProcessConnection.Close()
	if err != nil {
		return err
	}
	return c.cleanup(context.Background())
}

type providerLaunchCleanupGracefulConnection struct {
	*providerLaunchCleanupConnection
	graceful GracefulProcessConnection
}

func (c *providerLaunchCleanupGracefulConnection) CloseInput() error {
	return c.graceful.CloseInput()
}

func (c *providerLaunchCleanupGracefulConnection) Terminate() error {
	return c.graceful.Terminate()
}

func (c *providerLaunchCleanupGracefulConnection) Kill() error {
	return c.graceful.Kill()
}
