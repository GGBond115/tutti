package agenthost

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var errWorkspaceRuntimeDisconnectReentrant = errors.New("workspace runtime disconnect requested by an admitted operation")

type workspaceRuntimeAdmission struct {
	mu     sync.Mutex
	states map[string]*workspaceRuntimeAdmissionState
}

type workspaceRuntimeAdmissionState struct {
	changed       chan struct{}
	operations    int
	disconnecting bool
	refs          int
	deferred      []func(context.Context)
}

type workspaceRuntimeAdmissionContext struct {
	gate        *workspaceRuntimeAdmission
	workspaceID string
	exclusive   bool
}

type workspaceRuntimeAdmissionContextKey struct{}
type workspaceRuntimeDeferredDisconnectContextKey struct{}

func newWorkspaceRuntimeAdmission() *workspaceRuntimeAdmission {
	return &workspaceRuntimeAdmission{states: make(map[string]*workspaceRuntimeAdmissionState)}
}

func (g *workspaceRuntimeAdmission) enterOperation(
	ctx context.Context,
	workspaceID string,
) (context.Context, func(), error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if g == nil || workspaceID == "" {
		return ctx, func() {}, ErrInvalidArgument
	}
	if admission, ok := ctx.Value(workspaceRuntimeAdmissionContextKey{}).(workspaceRuntimeAdmissionContext); ok &&
		admission.gate == g && admission.workspaceID == workspaceID {
		return ctx, func() {}, nil
	}
	for {
		g.mu.Lock()
		state := g.stateLocked(workspaceID)
		if !state.disconnecting {
			state.operations++
			g.mu.Unlock()
			operationCtx := context.WithValue(ctx, workspaceRuntimeAdmissionContextKey{}, workspaceRuntimeAdmissionContext{
				gate: g, workspaceID: workspaceID,
			})
			var once sync.Once
			return operationCtx, func() {
				once.Do(func() { g.leaveOperation(workspaceID, state) })
			}, nil
		}
		changed := state.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.releaseReference(workspaceID, state)
			return ctx, func() {}, ctx.Err()
		case <-changed:
			g.releaseReference(workspaceID, state)
		}
	}
}

func (g *workspaceRuntimeAdmission) beginDisconnect(
	ctx context.Context,
	workspaceID string,
) (context.Context, func(), error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if g == nil || workspaceID == "" {
		return ctx, func() {}, ErrInvalidArgument
	}
	if admission, ok := ctx.Value(workspaceRuntimeAdmissionContextKey{}).(workspaceRuntimeAdmissionContext); ok &&
		admission.gate == g && admission.workspaceID == workspaceID {
		if admission.exclusive {
			return ctx, func() {}, nil
		}
		return ctx, func() {}, errWorkspaceRuntimeDisconnectReentrant
	}
	for {
		g.mu.Lock()
		state := g.stateLocked(workspaceID)
		if !state.disconnecting {
			state.disconnecting = true
			g.notifyLocked(state)
			g.mu.Unlock()
			if err := g.waitForOperations(ctx, workspaceID, state); err != nil {
				g.endDisconnect(workspaceID, state)
				return ctx, func() {}, err
			}
			disconnectCtx := context.WithValue(ctx, workspaceRuntimeAdmissionContextKey{}, workspaceRuntimeAdmissionContext{
				gate: g, workspaceID: workspaceID, exclusive: true,
			})
			var once sync.Once
			return disconnectCtx, func() {
				once.Do(func() { g.endDisconnect(workspaceID, state) })
			}, nil
		}
		changed := state.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.releaseReference(workspaceID, state)
			return ctx, func() {}, ctx.Err()
		case <-changed:
			g.releaseReference(workspaceID, state)
		}
	}
}

func (g *workspaceRuntimeAdmission) waitForOperations(ctx context.Context, workspaceID string, state *workspaceRuntimeAdmissionState) error {
	for {
		g.mu.Lock()
		if state.operations == 0 {
			g.mu.Unlock()
			return nil
		}
		changed := state.changed
		state.refs++
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.releaseReference(workspaceID, state)
			return ctx.Err()
		case <-changed:
			g.releaseReference(workspaceID, state)
		}
	}
}

func (g *workspaceRuntimeAdmission) stateLocked(workspaceID string) *workspaceRuntimeAdmissionState {
	state := g.states[workspaceID]
	if state == nil {
		state = &workspaceRuntimeAdmissionState{changed: make(chan struct{})}
		g.states[workspaceID] = state
	}
	state.refs++
	return state
}

func (g *workspaceRuntimeAdmission) leaveOperation(workspaceID string, state *workspaceRuntimeAdmissionState) {
	g.mu.Lock()
	state.operations--
	g.notifyLocked(state)
	var deferred []func(context.Context)
	if state.operations == 0 && len(state.deferred) > 0 {
		deferred = append(deferred, state.deferred...)
		state.deferred = nil
	}
	if len(deferred) == 0 {
		g.releaseReferenceLocked(workspaceID, state)
	}
	g.mu.Unlock()
	if len(deferred) == 0 {
		return
	}
	disconnectCtx := context.WithValue(context.Background(), workspaceRuntimeAdmissionContextKey{}, workspaceRuntimeAdmissionContext{
		gate: g, workspaceID: workspaceID, exclusive: true,
	})
	for _, disconnect := range deferred {
		disconnect(disconnectCtx)
	}
	g.endDisconnect(workspaceID, state)
}

func (g *workspaceRuntimeAdmission) deferDisconnect(
	workspaceID string,
	disconnect func(context.Context),
) {
	g.mu.Lock()
	state := g.states[workspaceID]
	if state == nil || state.operations == 0 {
		g.mu.Unlock()
		return
	}
	state.disconnecting = true
	state.deferred = append(state.deferred, disconnect)
	g.notifyLocked(state)
	g.mu.Unlock()
}

func (g *workspaceRuntimeAdmission) endDisconnect(workspaceID string, state *workspaceRuntimeAdmissionState) {
	g.mu.Lock()
	state.disconnecting = false
	g.notifyLocked(state)
	g.releaseReferenceLocked(workspaceID, state)
	g.mu.Unlock()
}

func (g *workspaceRuntimeAdmission) releaseReference(workspaceID string, state *workspaceRuntimeAdmissionState) {
	g.mu.Lock()
	g.releaseReferenceLocked(workspaceID, state)
	g.mu.Unlock()
}

func (g *workspaceRuntimeAdmission) releaseReferenceLocked(workspaceID string, state *workspaceRuntimeAdmissionState) {
	state.refs--
	if state.refs == 0 && state.operations == 0 && !state.disconnecting && g.states[workspaceID] == state {
		delete(g.states, workspaceID)
	}
}

func (*workspaceRuntimeAdmission) notifyLocked(state *workspaceRuntimeAdmissionState) {
	close(state.changed)
	state.changed = make(chan struct{})
}

func (h *Host) withWorkspaceRuntimeOperation(
	ctx context.Context,
	workspaceID string,
	fn func(context.Context) error,
) error {
	if h == nil || h.workspaceRuntimeAdmission == nil || fn == nil {
		return ErrInvalidArgument
	}
	operationCtx, release, err := h.workspaceRuntimeAdmission.enterOperation(ctx, workspaceID)
	if err != nil {
		return err
	}
	defer release()
	return fn(operationCtx)
}

// BeginWorkspaceRuntimeDisconnect prevents new runtime mutations in one
// Workspace and waits for mutations already admitted there. The returned
// context must be used for DisconnectWorkspaceRuntime while release is held.
func (h *Host) BeginWorkspaceRuntimeDisconnect(
	ctx context.Context,
	workspaceID string,
) (context.Context, func(), error) {
	if h == nil || h.workspaceRuntimeAdmission == nil {
		return ctx, func() {}, ErrInvalidArgument
	}
	disconnectCtx, release, err := h.workspaceRuntimeAdmission.beginDisconnect(ctx, workspaceID)
	if errors.Is(err, errWorkspaceRuntimeDisconnectReentrant) {
		// A transport may request attach cleanup while its Host operation already
		// owns admission. Let physical cleanup continue on the caller's existing
		// attachAxis; DisconnectWorkspaceRuntime will defer only its semantic sweep
		// until this operation leaves.
		return context.WithValue(ctx, workspaceRuntimeDeferredDisconnectContextKey{}, true), func() {}, nil
	}
	return disconnectCtx, release, err
}
